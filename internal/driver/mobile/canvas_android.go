//go:build android

package mobile

import "fyne.io/fyne/v2/internal/driver/mobile/app"

func setSystemBarsImpl(visible bool) {
	app.SetSystemBarsVisible(visible)
}
