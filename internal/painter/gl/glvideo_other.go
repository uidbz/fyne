//go:build !((!gles && !arm && !arm64 && !android && !ios && !mobile && !test_web_driver && !wasm) || (darwin && !mobile && !ios && !wasm && !test_web_driver)) && !((gles || arm || arm64) && !android && !ios && !mobile && !darwin && !wasm && !test_web_driver)

package gl

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
)

// drawGLVideo has real implementations for desktop OpenGL (glvideo_desktop.go)
// and OpenGL ES (glvideo_gles.go). On all other targets - mobile, web/wasm - the
// GLVideo object draws nothing.
func (p *painter) drawGLVideo(v *canvas.GLVideo, pos fyne.Position, frame fyne.Size) {}

func (p *painter) freeVideoTarget(v *canvas.GLVideo) {}
