//go:build darwin || linux || freebsd || openbsd || netbsd

package systrayapp

import (
	"os"
	"os/signal"
	"syscall"
)

// setupSignalNotify registers SIGINT and SIGTERM for graceful shutdown.
func setupSignalNotify(ch chan os.Signal) {
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
}
