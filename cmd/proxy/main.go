package main

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"uniview-codebuddy-proxy/internal/logbuf"
	"uniview-codebuddy-proxy/internal/systrayapp"
	"uniview-codebuddy-proxy/internal/version"
)

func main() {
	// Handle --login-item helper mode (macOS SMLoginItem)
	if len(os.Args) > 1 && os.Args[1] == "--login-item" {
		launchMainApp()
		return
	}

	// Setup log output: ring buffer + file
	logFilePath := logFilePath()
	mw := logbuf.NewMultiWriter(1000, logFilePath)
	log.SetOutput(mw)

	// Print startup info (goes to ring buffer + file)
	log.Println("==================================================")
	log.Printf("  CodeBuddy CN -> OpenAI API Proxy %s", version.Version)
	log.Printf("  Commit: %s | Built: %s", version.Commit, version.Date)
	log.Println("==================================================")

	app := systrayapp.New(mw)
	app.Run()
	// onExit in app.go handles logWriter.Close()
}

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
		cmd.Start()
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
			parent := filepath.Dir(dir)
			// Walk up further to find the main .app
			for j := 0; j < 5; j++ {
				if filepath.Ext(parent) == ".app" && !strings.Contains(parent, "Helper") {
					return parent
				}
				parent = filepath.Dir(parent)
				if parent == dir {
					break
				}
				dir = parent
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

// logFilePath returns the path for the log file.
func logFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codebuddy-proxy", "proxy.log")
}
