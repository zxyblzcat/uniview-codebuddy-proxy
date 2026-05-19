package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"codebuddy-proxy/internal/auth"
	"codebuddy-proxy/internal/config"
	"codebuddy-proxy/internal/proxy"
	"codebuddy-proxy/internal/version"

	"github.com/gin-gonic/gin"
)

func main() {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(requestLogger(), gin.Recovery(), maxBodySize(10<<20))

	// 注册路由
	auth.RegisterRoutes(r)
	proxy.RegisterRoutes(r)

	// 启动信息
	addr := config.ListenAddr()
	fmt.Println()
	fmt.Println("==================================================")
	fmt.Printf("  CodeBuddy CN -> OpenAI API Proxy %s\n", version.Version)
	fmt.Printf("  Commit: %s | Built: %s\n", version.Commit, version.Date)
	fmt.Printf("  URL: http://localhost:%d\n", config.Port)
	fmt.Printf("  Auth: http://localhost:%d/auth/start\n", config.Port)
	fmt.Println("==================================================")
	fmt.Println()

	if auth.LoadToken() != nil {
		log.Println("Token loaded from cache")
	} else {
		log.Println("No token. Fetching CodeBuddy login URL...")
		authURL, authState, err := auth.FetchAuthURL()
		if err != nil {
			log.Printf("Failed to get auth URL: %v", err)
			log.Printf("Please visit http://localhost:%d/auth/start manually", config.Port)
		} else {
			log.Printf("Auth state: %s", authState)
			auth.OpenBrowser(authURL)
		}

		// 启动后台轮询，等待用户完成登录
		if authState != "" {
			go func() {
				for i := 0; i < 60; i++ {
					result := auth.PollToken(authState)
					if result.Status == "success" {
						log.Printf("Login success! User: %s", result.UserID)
						return
					}
					if result.Status == "error" {
						log.Printf("Auth poll error: %s", result.Message)
						return
					}
					time.Sleep(3 * time.Second)
				}
				log.Println("Auth poll timed out after 3 minutes")
			}()
		}
	}

	// 启动 HTTP 服务
	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// 等待信号退出
	fmt.Println("按 Ctrl+C 关闭代理...")
	waitExit()

	// 优雅关闭 HTTP 服务
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer shutdownCancel()
	_ = srv.Shutdown(shutdownCtx)
	log.Println("代理已停止")
	os.Exit(0)
}

// waitExit 监听系统信号，触发时关闭程序
func waitExit() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigCh
	fmt.Printf("\n收到信号 %v，正在关闭代理...\n", sig)
}

// requestLogger 按状态码分级输出请求日志：4xx/5xx 始终打印，2xx 仅打印慢请求
func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		elapsed := time.Since(start).Truncate(time.Millisecond)
		status := c.Writer.Status()
		path := c.Request.URL.Path
		if q := c.Request.URL.RawQuery; q != "" {
			path += "?" + q
		}
		if status >= 400 || elapsed >= 20*time.Second {
			log.Printf("[GIN] %v | %d | %13v | %15s | %6d | %-7s %s",
				start.Format("2006/01/02 - 15:04:05"), status, elapsed, c.ClientIP(), c.Writer.Size(), c.Request.Method, path)
		}
	}
}

// maxBodySize 限制请求体大小（防止 OOM 攻击）
func maxBodySize(maxBytes int) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, int64(maxBytes))
		c.Next()
	}
}
