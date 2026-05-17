package main

import (
	"fmt"
	"log"

	"codebuddy-proxy/internal/auth"
	"codebuddy-proxy/internal/config"
	"codebuddy-proxy/internal/proxy"

	"github.com/gin-gonic/gin"
)

func main() {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	// 注册路由
	auth.RegisterRoutes(r)
	proxy.RegisterRoutes(r)

	// 启动信息
	addr := config.ListenAddr()
	fmt.Println()
	fmt.Println("==================================================")
	fmt.Println("  CodeBuddy CN -> OpenAI API Proxy v3 (Go)")
	fmt.Printf("  URL: http://localhost:%d\n", config.Port)
	fmt.Printf("  Auth: http://localhost:%d/auth/start\n", config.Port)
	fmt.Println("==================================================")
	fmt.Println()

	if auth.LoadToken() != nil {
		log.Println("Token loaded from disk")
	} else {
		log.Println("No token. Visit /auth/start to login via OAuth2")
	}

	if err := r.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
