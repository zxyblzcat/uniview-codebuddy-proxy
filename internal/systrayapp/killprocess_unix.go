//go:build darwin || linux || freebsd || openbsd || netbsd

package systrayapp

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// killProcessOnPort kills the process occupying the given port using lsof.
func killProcessOnPort(port int) error {
	cmd := exec.Command("lsof", "-iTCP", "-sTCP:LISTEN", "-ti", fmt.Sprintf(":%d", port))
	out, err := cmd.Output()
	if err != nil {
		// lsof exits non-zero when nothing is listening — that's fine
		return nil
	}

	pidStr := strings.TrimSpace(string(out))
	if pidStr == "" {
		return nil
	}

	for _, pidStr := range strings.Split(pidStr, "\n") {
		pid, err := strconv.Atoi(strings.TrimSpace(pidStr))
		if err != nil {
			continue
		}

		proc, err := os.FindProcess(pid)
		if err != nil {
			continue
		}

		log.Printf("Killing process %d on port %d...", pid, port)
		if err := proc.Signal(syscall.SIGTERM); err != nil {
			return fmt.Errorf("failed to kill PID %d: %w", pid, err)
		}
	}

	// Wait for the process to release the port (poll up to 2s)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		check := exec.Command("lsof", "-iTCP", "-sTCP:LISTEN", "-ti", fmt.Sprintf(":%d", port))
		if out, _ := check.Output(); len(strings.TrimSpace(string(out))) == 0 {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}
