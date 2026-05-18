package main

import (
	"bufio"
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

	"github.com/gin-gonic/gin"
	"golang.org/x/term"
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
	srv := &http.Server{Addr: addr, Handler: r}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// 等待任意按键或信号退出
	fmt.Println("按任意键关闭代理...")
	waitExit(context.Background())

	// 优雅关闭 HTTP 服务
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer shutdownCancel()
	_ = srv.Shutdown(shutdownCtx)
	log.Println("Proxy stopped.")
	os.Exit(0)
}

// waitExit 监听任意按键和系统信号，触发时关闭程序
func waitExit(ctx context.Context) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	keyCh := make(chan struct{})
	if term.IsTerminal(int(os.Stdin.Fd())) {
		go func() {
			bufio.NewReader(os.Stdin).ReadString('\n')
			keyCh <- struct{}{}
		}()
	} else {
		fmt.Println("(非终端模式，按 Ctrl+C 关闭)")
	}

	select {
	case <-keyCh:
		fmt.Println("\n收到按键，正在关闭代理...")
	case sig := <-sigCh:
		fmt.Printf("\n收到信号 %v，正在关闭代理...\n", sig)
	case <-ctx.Done():
	}
}
