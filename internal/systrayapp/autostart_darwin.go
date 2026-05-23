//go:build darwin

package systrayapp

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// IsAutoStartEnabled returns true if the .app is in the login items list.
func IsAutoStartEnabled() bool {
	out, err := exec.Command("osascript", "-e",
		`tell application "System Events" to get the path of every login item`).Output()
	if err != nil {
		return false
	}
	exePath, err := os.Executable()
	if err != nil {
		return false
	}
	exePath, _ = filepath.EvalSymlinks(exePath)
	appPath := findAppBundle(exePath)
	if appPath == "" {
		return false
	}
	return strings.Contains(string(out), appPath)
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

	var script string
	if enabled {
		script = fmt.Sprintf(
			`tell application "System Events" to make new login item with properties {path:%q, hidden:false}`,
			appPath,
		)
	} else {
		script = fmt.Sprintf(
			`tell application "System Events" to delete (every login item whose path is %q)`,
			appPath,
		)
	}

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
