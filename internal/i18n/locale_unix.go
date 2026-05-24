//go:build !windows

package i18n

import (
	"os"
	"strings"

	"golang.org/x/text/language"
)

func detectSystemLocale() language.Tag {
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
