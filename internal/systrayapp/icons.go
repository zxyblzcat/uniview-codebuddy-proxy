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
	//go:embed icon_template.png
	iconTemplatePNG []byte
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
	if runtime.GOOS == "darwin" {
		systray.SetTemplateIcon(iconTemplatePNG, iconNormalPNG)
	} else {
		systray.SetIcon(iconNormal())
	}
	systray.SetTitle("")
	systray.SetTooltip("UniviewCodeBuddyProxy")
}

func setIconGray() {
	if runtime.GOOS == "darwin" {
		systray.SetTemplateIcon(iconTemplatePNG, iconGrayPNG)
	} else {
		systray.SetIcon(iconGray())
	}
	systray.SetTitle("")
	systray.SetTooltip("UniviewCodeBuddyProxy")
}

func setIconError() {
	if runtime.GOOS == "darwin" {
		// Error state: still use template icon for consistent appearance
		systray.SetTemplateIcon(iconTemplatePNG, iconErrorPNG)
	} else {
		systray.SetIcon(iconError())
	}
	systray.SetTitle("")
	systray.SetTooltip("UniviewCodeBuddyProxy")
}

func setTrayTitle(title string) {
	systray.SetTitle(title)
}
