//go:build (android || ios || mobile) && (!wasm || !test_web_driver)

package gl

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/internal/driver/mobile/gl"
)

// drawGLVideo is the mobile (OpenGL ES via Fyne's work-queue GL binding) variant
// of the GLVideo painter. It mirrors glvideo_gles.go, but the GL context is
// current only on the driver's GL worker thread, so calls are marshalled through
// the work queue (p.glctx()) and the renderer's own direct GL (libmpv) runs
// inside RunOnGLThread. Behaviour is otherwise identical: render the current
// frame into an offscreen FBO, then composite its colour texture like an image.
func (p *painter) drawGLVideo(v *canvas.GLVideo, pos fyne.Position, frame fyne.Size) {
	if v.Renderer == nil {
		return
	}

	size := v.Size()

	// Letterbox: when the video aspect is known and differs from the object's,
	// shrink the drawn rectangle to preserve the video ratio, centring it.
	drawPos := pos
	drawSize := size
	if aspect := v.Renderer.Aspect(); aspect > 0 && size.Height > 0 {
		objAspect := size.Width / size.Height
		if objAspect > aspect { // object too wide: pillarbox
			drawSize.Width = size.Height * aspect
			drawPos.X = pos.X + (size.Width-drawSize.Width)/2
		} else if objAspect < aspect { // object too tall: letterbox
			drawSize.Height = size.Width / aspect
			drawPos.Y = pos.Y + (size.Height-drawSize.Height)/2
		}
	}

	texW := int(roundToPixel(drawSize.Width*p.pixScale, 1.0))
	texH := int(roundToPixel(drawSize.Height*p.pixScale, 1.0))
	if texW <= 0 || texH <= 0 {
		return
	}

	target := p.ensureVideoTarget(v, texW, texH)
	target.painted = true

	// libmpv resolves GL entry points itself and renders synchronously; it must
	// run on the thread that owns the EGL context. It binds the supplied FBO via
	// MPV_RENDER_PARAM_OPENGL_FBO. RunOnGLThread blocks until the frame is drawn,
	// so the composite below always reads a complete texture.
	fbo, w, h := target.fbo, target.width, target.height
	p.glctx().RunOnGLThread(func() {
		v.Renderer.RenderInto(fbo, w, h)
	})

	// mpv leaves its own FBO bound and the viewport changed; restore the window
	// framebuffer (0 on an EGL window surface) and viewport for scene drawing.
	p.glctx().BindFramebuffer(gl.FramebufferTarget, gl.Framebuffer{})
	p.ctx.Viewport(0, 0, p.fbWidth, p.fbHeight)

	p.drawTextureRegion(Texture{Value: target.tex}, drawPos, drawSize, frame)
}

// ensureVideoTarget returns the FBO/texture for the given object, (re)allocating
// it when missing or when its size no longer matches the requested pixel size.
func (p *painter) ensureVideoTarget(v *canvas.GLVideo, width, height int) *videoTarget {
	if p.videoTargets == nil {
		p.videoTargets = make(map[*canvas.GLVideo]*videoTarget)
	}

	t := p.videoTargets[v]
	if t != nil && t.width == width && t.height == height {
		return t
	}

	glctx := p.glctx()
	if t == nil {
		t = &videoTarget{}
		t.tex = glctx.CreateTexture().Value
		t.fbo = glctx.CreateFramebuffer().Value
		p.videoTargets[v] = t
	}

	t.width = width
	t.height = height

	tex := gl.Texture{Value: t.tex}
	glctx.BindTexture(gl.Texture2D, tex)
	glctx.TexParameteri(gl.Texture2D, gl.TextureMinFilter, gl.Linear)
	glctx.TexParameteri(gl.Texture2D, gl.TextureMagFilter, gl.Linear)
	glctx.TexParameteri(gl.Texture2D, gl.TextureWrapS, gl.ClampToEdge)
	glctx.TexParameteri(gl.Texture2D, gl.TextureWrapT, gl.ClampToEdge)
	glctx.TexImage2D(gl.Texture2D, 0, gl.RGBA, width, height, gl.RGBA, gl.UnsignedByte, nil)

	glctx.BindFramebuffer(gl.FramebufferTarget, gl.Framebuffer{Value: t.fbo})
	glctx.FramebufferTexture2D(gl.FramebufferTarget, gl.ColorAttachment0, gl.Texture2D, tex, 0)
	glctx.BindFramebuffer(gl.FramebufferTarget, gl.Framebuffer{})

	return t
}

// freeVideoTarget releases the FBO and texture held for the given object.
func (p *painter) freeVideoTarget(v *canvas.GLVideo) {
	t := p.videoTargets[v]
	if t == nil {
		return
	}
	glctx := p.glctx()
	glctx.DeleteFramebuffer(gl.Framebuffer{Value: t.fbo})
	glctx.DeleteTexture(gl.Texture{Value: t.tex})
	delete(p.videoTargets, v)
}
