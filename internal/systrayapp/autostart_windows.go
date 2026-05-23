//go:build windows

package systrayapp

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows/registry"
)

const regKey = `Software\Microsoft\Windows\CurrentVersion\Run`
const regValueName = "UniviewCodeBuddyProxy"

// IsAutoStartEnabled returns true if the registry entry exists.
func IsAutoStartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, regKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()

	_, _, err = k.GetStringValue(regValueName)
	return err == nil
}

// SetAutoStart enables or disables Windows autostart via the registry.
func SetAutoStart(enabled bool) error {
	if enabled {
		exePath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("cannot determine executable path: %w", err)
		}

		k, _, err := registry.CreateKey(registry.CURRENT_USER, regKey, registry.SET_VALUE)
		if err != nil {
			return fmt.Errorf("cannot open registry key: %w", err)
		}
		defer k.Close()

		if err := k.SetStringValue(regValueName, exePath); err != nil {
			return fmt.Errorf("cannot set registry value: %w", err)
		}
		return nil
	}

	k, err := registry.OpenKey(registry.CURRENT_USER, regKey, registry.SET_VALUE)
	if err != nil {
		return nil
	}
	defer k.Close()

	_ = k.DeleteValue(regValueName)
	return nil
}
