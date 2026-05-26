//go:build windows

package systrayapp

import (
	"os"
	"os/signal"
)

// setupSignalNotify registers os.Interrupt for graceful shutdown on Windows.
// SIGTERM is not supported on Windows; os.Interrupt handles Ctrl+C and console close events.
func setupSignalNotify(ch chan os.Signal) {
	signal.Notify(ch, os.Interrupt)
}
