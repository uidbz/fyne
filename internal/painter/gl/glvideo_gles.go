//go:build (gles || arm || arm64) && !android && !ios && !mobile && !darwin && !wasm && !test_web_driver

package gl

import (
	gl "github.com/go-gl/gl/v3.1/gles2"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
)

// drawGLVideo asks the object's renderer to draw the current video frame into a
// dedicated offscreen framebuffer, then composites that framebuffer's colour
// texture into the object's bounds like any other image. This is the OpenGL ES
// variant; it is identical in behaviour to the desktop-core version, using the
// gles2 bindings. Framebuffer objects are core in GLES 2.0, so the same calls
// apply.
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

	// Save the currently bound framebuffer so we can restore it: Fyne renders to
	// the window's default framebuffer, which is not necessarily 0 on every
	// platform.
	var prevFBO int32
	gl.GetIntegerv(gl.FRAMEBUFFER_BINDING, &prevFBO)

	gl.BindFramebuffer(gl.FRAMEBUFFER, target.fbo)
	v.Renderer.RenderInto(target.fbo, target.width, target.height)

	// Restore Fyne's framebuffer and viewport; the renderer will have changed both.
	gl.BindFramebuffer(gl.FRAMEBUFFER, uint32(prevFBO))
	p.ctx.Viewport(0, 0, p.fbWidth, p.fbHeight)

	p.drawTextureRegion(Texture(target.tex), drawPos, drawSize, frame, canvas.ImageFillStretch, 1, 0, 0, 0)
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

	if t == nil {
		t = &videoTarget{}
		var tex, fbo uint32
		gl.GenTextures(1, &tex)
		gl.GenFramebuffers(1, &fbo)
		t.tex = tex
		t.fbo = fbo
		p.videoTargets[v] = t
	}

	t.width = width
	t.height = height

	gl.BindTexture(gl.TEXTURE_2D, t.tex)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
	gl.TexImage2D(gl.TEXTURE_2D, 0, gl.RGBA, int32(width), int32(height), 0, gl.RGBA, gl.UNSIGNED_BYTE, nil)

	var prevFBO int32
	gl.GetIntegerv(gl.FRAMEBUFFER_BINDING, &prevFBO)
	gl.BindFramebuffer(gl.FRAMEBUFFER, t.fbo)
	gl.FramebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, t.tex, 0)
	gl.BindFramebuffer(gl.FRAMEBUFFER, uint32(prevFBO))

	return t
}

// freeVideoTarget releases the FBO and texture held for the given object.
func (p *painter) freeVideoTarget(v *canvas.GLVideo) {
	t := p.videoTargets[v]
	if t == nil {
		return
	}
	gl.DeleteFramebuffers(1, &t.fbo)
	gl.DeleteTextures(1, &t.tex)
	delete(p.videoTargets, v)
}
