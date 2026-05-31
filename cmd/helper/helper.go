package main

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// launchMainApp is called in --login-item mode to start the main application.
// It finds the main .app bundle and launches it, then exits.
func launchMainApp() {
	exePath, err := os.Executable()
	if err != nil {
		log.Fatalf("cannot determine executable path: %v", err)
	}
	exePath, _ = filepath.EvalSymlinks(exePath)

	// Find the main .app bundle (the helper is inside Contents/Library/LoginItems/)
	appPath := findMainAppBundle(exePath)
	if appPath == "" {
		// Fallback: just run without --login-item
		log.Println("--login-item: could not find main .app bundle, launching self without flag")
		cmd := exec.Command(exePath)
		if err := cmd.Start(); err != nil {
			log.Fatalf("failed to launch self: %v", err)
		}
		return
	}

	// Open the main .app bundle
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.Command("open", appPath)
	} else {
		cmd = exec.Command(appPath)
	}

	if err := cmd.Start(); err != nil {
		log.Fatalf("failed to launch main app: %v", err)
	}
}

// findMainAppBundle walks up from the helper binary to find the parent .app bundle.
func findMainAppBundle(exePath string) string {
	dir := filepath.Dir(exePath)
	for i := 0; i < 10; i++ {
		if filepath.Ext(dir) == ".app" {
			// This is the helper .app inside LoginItems — keep going up
			cur := filepath.Dir(dir)
			for j := 0; j < 5; j++ {
				if filepath.Ext(cur) == ".app" && !strings.Contains(cur, "Helper") {
					return cur
				}
				parent := filepath.Dir(cur)
				if parent == cur {
					break
				}
				cur = parent
			}
			return ""
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}
