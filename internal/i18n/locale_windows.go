//go:build windows

package i18n

import (
	"syscall"
	"unsafe"

	"golang.org/x/text/language"
)

var kernel32 = syscall.NewLazyDLL("kernel32.dll")
var procGetUserDefaultUILanguage = kernel32.NewProc("GetUserDefaultUILanguage")

func detectSystemLocale() language.Tag {
	langID, _, _ := procGetUserDefaultUILanguage.Call()
	lang := uint16(langID)
	switch {
	case lang == 0x0804:
		return language.SimplifiedChinese
	case lang == 0x0404:
		return language.TraditionalChinese
	default:
		return language.AmericanEnglish
	}
}
