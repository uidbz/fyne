package mobile

// MediaAction identifies a transport or audio event delivered by the
// platform's media session (notification buttons, lock screen, headset
// buttons, audio focus changes, headphone unplug).
type MediaAction int

const (
	// MediaActionPlay requests playback start/resume. arg is unused.
	MediaActionPlay MediaAction = 1
	// MediaActionPause requests pause. arg is unused.
	MediaActionPause MediaAction = 2
	// MediaActionNext requests the next track. arg is unused.
	MediaActionNext MediaAction = 3
	// MediaActionPrevious requests the previous track. arg is unused.
	MediaActionPrevious MediaAction = 4
	// MediaActionStop requests stop. arg is unused.
	MediaActionStop MediaAction = 5
	// MediaActionSeek requests an absolute seek. arg is the target in
	// milliseconds.
	MediaActionSeek MediaAction = 6

	// MediaActionFocusLoss reports a permanent audio focus loss (another
	// app took over). Apps usually pause. arg is unused.
	MediaActionFocusLoss MediaAction = 10
	// MediaActionFocusLossTransient reports a temporary focus loss (call,
	// assistant). Apps usually pause and resume on MediaActionFocusGain.
	// arg is unused.
	MediaActionFocusLossTransient MediaAction = 11
	// MediaActionFocusDuck reports a temporary loss where ducking (lowering
	// volume) instead of pausing is acceptable. arg is unused.
	MediaActionFocusDuck MediaAction = 12
	// MediaActionFocusGain reports focus regained after a transient loss or
	// duck. arg is unused.
	MediaActionFocusGain MediaAction = 13
	// MediaActionBecomingNoisy reports audio routing to the device speaker
	// (headphones unplugged). Apps usually pause. arg is unused.
	MediaActionBecomingNoisy MediaAction = 14
)

// MediaMetadata describes the currently playing track for the media session
// (notification, lock screen, Bluetooth AVRCP).
type MediaMetadata struct {
	Title  string
	Artist string
	Album  string
	// ArtworkPNG is the album art as PNG bytes. A nil slice keeps the
	// previously sent artwork; an empty non-nil slice clears it.
	ArtworkPNG []byte
}

// MediaState is the current playback state pushed to the media session.
type MediaState struct {
	Playing    bool
	PositionMS int64
	DurationMS int64
}

// MediaSessionDriver is implemented by the mobile driver on Android:
// it feeds a system media session and foreground playback service
// (notification with transport controls, lock screen, headset buttons,
// audio focus). Apps discover it with a type assertion:
//
//	if d, ok := fyne.CurrentApp().Driver().(mobile.MediaSessionDriver); ok {
//		d.SetMediaActionHandler(...)
//	}
//
// The assertion fails on other platforms.
type MediaSessionDriver interface {
	// SetMediaActionHandler registers the callback for MediaAction events.
	// The handler runs on a platform service thread and must not block;
	// dispatch UI work through fyne.Do.
	SetMediaActionHandler(func(action MediaAction, arg int64))
	// MediaSessionUpdate pushes the current track and state, starting the
	// foreground service on first use.
	MediaSessionUpdate(meta *MediaMetadata, state MediaState)
	// MediaSessionStop tears down the session and service, removing the
	// playback notification.
	MediaSessionStop()
}
