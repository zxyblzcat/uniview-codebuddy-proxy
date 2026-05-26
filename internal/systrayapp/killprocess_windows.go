//go:build windows

package systrayapp

import (
	"fmt"
	"log"
	"os/exec"
	"strings"
	"time"
)

// killProcessOnPort kills the process occupying the given port using netstat + taskkill.
func killProcessOnPort(port int) error {
	// Use netstat to find the PID listening on the port
	cmd := exec.Command("netstat", "-ano")
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("netstat failed: %w", err)
	}

	target := fmt.Sprintf(":%d ", port)
	var pids []string
	seen := make(map[string]bool)

	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "LISTENING") {
			continue
		}
		// Lines look like:  TCP   0.0.0.0:1026   0.0.0.0:0   LISTENING   12345
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		// Check if the local address field contains our port
		if !strings.Contains(fields[1], target) {
			continue
		}
		pid := fields[4]
		if !seen[pid] {
			seen[pid] = true
			pids = append(pids, pid)
		}
	}

	if len(pids) == 0 {
		return nil
	}

	for _, pid := range pids {
		log.Printf("Killing process %s on port %d...", pid, port)
		killCmd := exec.Command("taskkill", "/F", "/PID", pid)
		if out, err := killCmd.CombinedOutput(); err != nil {
			log.Printf("taskkill /PID %s failed: %v, output: %s", pid, err, strings.TrimSpace(string(out)))
		}
	}

	// Wait for the process to release the port (poll up to 2s)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		check := exec.Command("netstat", "-ano")
		if out, err := check.Output(); err == nil {
			stillListening := false
			for _, line := range strings.Split(string(out), "\n") {
				if strings.Contains(line, "LISTENING") && strings.Contains(line, target) {
					stillListening = true
					break
				}
			}
			if !stillListening {
				return nil
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}
