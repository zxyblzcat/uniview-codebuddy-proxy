package systrayapp

import (
	_ "embed"

	"fyne.io/systray"
)

//go:embed icon_normal.png
var iconNormal []byte

//go:embed icon_gray.png
var iconGray []byte

func setIconNormal() {
	systray.SetIcon(iconNormal)
	systray.SetTitle("")
	systray.SetTooltip("CodeBuddy Proxy — Connected")
}

func setIconError() {
	systray.SetIcon(iconGray)
	systray.SetTitle("")
	systray.SetTooltip("CodeBuddy Proxy — Not Connected")
}
