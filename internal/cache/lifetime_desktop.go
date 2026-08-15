//go:build !android && !ios && !mobile && !wasm && !test_web_driver

package cache

import "time"

// defaultValidDuration is the default cache lifetime for desktop builds.
// imgview fork change: raised from 1 minute to 10 minutes so off-screen
// gallery tile textures survive longer; scrolling back up no longer
// re-uploads every thumbnail after a minute. Trade-off: more VRAM.
// Override at runtime with the FYNE_CACHE environment variable.
const defaultValidDuration = 10 * time.Minute
