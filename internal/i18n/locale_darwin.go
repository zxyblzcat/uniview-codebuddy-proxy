//go:build darwin

package i18n

import (
	"os"
	"os/exec"
	"strings"

	"golang.org/x/text/language"
)

func detectSystemLocale() language.Tag {
	// GUI apps on macOS do not inherit shell env vars (LANG, LC_ALL, etc.).
	// Read the system locale via the defaults command which accesses
	// ~/Library/Preferences/.GlobalPreferences.plist directly.
	if out, err := exec.Command("defaults", "read", "-g", "AppleLocale").Output(); err == nil {
		loc := strings.TrimSpace(string(out))
		loc = strings.ReplaceAll(loc, "_", "-")
		if tag, err := language.Parse(loc); err == nil {
			return tag
		}
	}

	// Fallback: POSIX env vars (works when launched from terminal)
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if val := os.Getenv(key); val != "" {
			loc := strings.Split(val, ".")[0]
			loc = strings.ReplaceAll(loc, "_", "-")
			if tag, err := language.Parse(loc); err == nil {
				return tag
			}
		}
	}

	return language.English
}
