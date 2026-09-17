//go:build android

package mobile

import (
	mobiledriver "fyne.io/fyne/v2/driver/mobile"
	"fyne.io/fyne/v2/internal/driver/mobile/app"
)

// The Android driver implements mobiledriver.MediaSessionDriver through these
// methods; the interface assertion in consuming apps fails on other
// platforms, so no stubs are needed there.

// SetMediaActionHandler registers the callback for transport and audio-focus
// events from the media playback service. The handler runs on a service
// thread and must not block; dispatch UI work through fyne.Do.
func (d *driver) SetMediaActionHandler(fn func(action mobiledriver.MediaAction, arg int64)) {
	app.SetMediaActionHandler(func(action int, arg int64) {
		fn(mobiledriver.MediaAction(action), arg)
	})
}

// MediaSessionUpdate pushes track metadata and playback state to the Android
// media session, starting the foreground service on first use.
func (d *driver) MediaSessionUpdate(meta *mobiledriver.MediaMetadata, state mobiledriver.MediaState) {
	if meta == nil {
		meta = &mobiledriver.MediaMetadata{}
	}
	if err := app.MediaSessionUpdate(meta.Title, meta.Artist, meta.Album, meta.ArtworkPNG,
		state.Playing, state.PositionMS, state.DurationMS); err != nil {
		// A missing service declaration is reported by the Java side; log here
		// for visibility.
		println("fyne: MediaSessionUpdate failed:", err.Error())
	}
}

// MediaSessionStop tears down the media session and foreground service.
func (d *driver) MediaSessionStop() {
	_ = app.MediaSessionStop()
}
