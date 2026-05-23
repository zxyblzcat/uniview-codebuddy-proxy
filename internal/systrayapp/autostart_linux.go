//go:build linux

package systrayapp

// IsAutoStartEnabled returns false on Linux (not implemented).
func IsAutoStartEnabled() bool {
	return false
}

// SetAutoStart is a no-op on Linux.
func SetAutoStart(enabled bool) error {
	return nil
}
