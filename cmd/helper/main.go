package main

import (
	"log"
	"os"
)

// Minimal helper binary for macOS SMLoginItem.
// Only handles --login-item to launch the main .app bundle.
func main() {
	if len(os.Args) > 1 && os.Args[1] == "--login-item" {
		launchMainApp()
		return
	}
	log.Println("Helper launched without --login-item flag, exiting")
}
