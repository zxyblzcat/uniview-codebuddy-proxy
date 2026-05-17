package main

import (
	"fmt"

	"codebuddy-proxy/internal/config"
)

func main() {
	fmt.Printf("codebuddy-proxy starting on %s\n", config.ListenAddr())
	fmt.Printf("upstream: %s\n", config.ChatURL)
	fmt.Printf("api_password: %q\n", config.APIPassword)
}
