package systrayapp

import (
	_ "embed"
	"runtime"

	"fyne.io/systray"
)

var (
	//go:embed icon_normal.png
	iconNormalPNG []byte
	//go:embed icon_gray.png
	iconGrayPNG []byte
	//go:embed icon_error.png
	iconErrorPNG []byte
)

var (
	//go:embed icon_normal.ico
	iconNormalICO []byte
	//go:embed icon_gray.ico
	iconGrayICO []byte
	//go:embed icon_error.ico
	iconErrorICO []byte
)

func iconNormal() []byte {
	if runtime.GOOS == "windows" {
		return iconNormalICO
	}
	return iconNormalPNG
}

func iconGray() []byte {
	if runtime.GOOS == "windows" {
		return iconGrayICO
	}
	return iconGrayPNG
}

func iconError() []byte {
	if runtime.GOOS == "windows" {
		return iconErrorICO
	}
	return iconErrorPNG
}

func setIconNormal() {
	systray.SetIcon(iconNormal())
	systray.SetTitle("")
	systray.SetTooltip("UniviewCodeBuddyProxy")
}

func setIconGray() {
	systray.SetIcon(iconGray())
	systray.SetTitle("")
	systray.SetTooltip("UniviewCodeBuddyProxy")
}

func setIconError() {
	systray.SetIcon(iconError())
	systray.SetTitle("")
	systray.SetTooltip("UniviewCodeBuddyProxy")
}

func setTrayTitle(title string) {
	systray.SetTitle(title)
}
