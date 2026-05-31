//go:build !gui

package systrayapp

// Stub implementations for headless mode — no systray dependency.

func setIconNormal() {}
func setIconGray()   {}
func setIconError()  {}
func setTrayTitle(_ string) {}
