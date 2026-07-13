package canvas

import (
	"fyne.io/fyne/v2"
)

// Declare conformity with CanvasObject interface
var _ fyne.CanvasObject = (*GLVideo)(nil)

// GLVideoRenderer is implemented by a backend (for example one driving libmpv's
// OpenGL Render API) that can draw a video frame directly into an OpenGL
// framebuffer object. It is called by Fyne's GL painter during a paint pass,
// with the painter's OpenGL context current on the render thread, so the
// implementation may issue GL calls freely.
//
// Since: 2.8
type GLVideoRenderer interface {
	// RenderInto draws the current video frame into the given framebuffer object,
	// which has a colour texture attached and a viewport of width x height pixels.
	// The origin is bottom-left, matching OpenGL and libmpv conventions. The
	// implementation must leave the GL state as it found it where practical; the
	// painter re-binds its own program and buffers afterwards.
	RenderInto(fbo uint32, width, height int)

	// NeedsPaint reports whether the video has produced a new frame since the
	// last RenderInto. The painter uses this to decide whether a repaint is due.
	NeedsPaint() bool

	// Aspect returns the display aspect ratio (width / height) of the current
	// video, or 0 if it is not yet known. When non-zero the painter letterboxes
	// the frame within the object's bounds to preserve this ratio; when 0 the
	// frame fills the whole object.
	Aspect() float32
}

// GLVideo is a canvas object whose contents are produced by a GLVideoRenderer
// drawing directly into an OpenGL framebuffer. It is the integration point for
// embedding a hardware video pipeline (such as libmpv's Render API) inside a
// Fyne canvas, without any cross-process window embedding - which makes it work
// identically on X11, Wayland, macOS and Windows.
//
// The renderer is invoked every paint while the object is visible. A backend
// should call Refresh (or trigger a canvas repaint) when a new frame is ready.
//
// Since: 2.8
type GLVideo struct {
	baseObject

	// Renderer produces frames for this object. It must be set before the object
	// is first painted. It may be nil, in which case the object draws nothing.
	Renderer GLVideoRenderer
}

// NewGLVideo returns a new GLVideo backed by the given renderer.
//
// Since: 2.8
func NewGLVideo(renderer GLVideoRenderer) *GLVideo {
	return &GLVideo{Renderer: renderer}
}

// Hide will set this video object to not be visible.
func (v *GLVideo) Hide() {
	v.baseObject.Hide()

	repaint(v)
}

// Move the video object to a new position, relative to its parent / canvas.
func (v *GLVideo) Move(pos fyne.Position) {
	if v.Position() == pos {
		return
	}

	v.baseObject.Move(pos)

	repaint(v)
}

// Refresh causes this video object to be redrawn with its current frame.
func (v *GLVideo) Refresh() {
	Refresh(v)
}

// Resize updates the size of this video object.
func (v *GLVideo) Resize(size fyne.Size) {
	if size == v.Size() {
		return
	}

	v.baseObject.Resize(size)

	Refresh(v)
}
