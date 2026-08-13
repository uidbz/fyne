//go:build !android

package mobile

func setSystemBarsImpl(visible bool) {
	// No-op on non-Android platforms (iOS doesn't need this as it manages system bars automatically)
}
