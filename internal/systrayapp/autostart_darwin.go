//go:build darwin

package systrayapp

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const serviceID = "com.codebuddy.proxy.helper"

// IsAutoStartEnabled returns true if SMLoginItem is registered.
func IsAutoStartEnabled() bool {
	out, err := exec.Command("launchctl", "list", serviceID).CombinedOutput()
	if err != nil {
		return false
	}
	return len(out) > 0
}

// SetAutoStart enables or disables the SMLoginItem login item.
func SetAutoStart(enabled bool) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot determine executable path: %w", err)
	}

	exePath, _ = filepath.EvalSymlinks(exePath)

	appPath := findAppBundle(exePath)
	if appPath == "" {
		return fmt.Errorf("not running from a .app bundle; cannot set login item")
	}

	script := fmt.Sprintf(`tell application "System Events" to %s login item %q`, func() string {
		if enabled {
			return "add"
		}
		return "delete"
	}(), filepath.Base(appPath))

	cmd := exec.Command("osascript", "-e", script)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("osascript failed: %w, output: %s", err, string(out))
	}
	return nil
}

// findAppBundle walks up from exePath to find a .app bundle root.
func findAppBundle(exePath string) string {
	dir := filepath.Dir(exePath)
	for i := 0; i < 10; i++ {
		if filepath.Ext(dir) == ".app" {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}
