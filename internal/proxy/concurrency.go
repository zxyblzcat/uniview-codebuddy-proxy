package proxy

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync/atomic"

	"uniview-codebuddy-proxy/internal/config"

	"github.com/gin-gonic/gin"
	"golang.org/x/sync/semaphore"
)

// concurrencySem 控制同时处理的代理请求数
// 使用加权信号量而非固定 worker pool，因为 Go goroutine 已是轻量级 worker，
// 信号量只需控制并发上限，无需管理 worker 生命周期
var concurrencySem = semaphore.NewWeighted(int64(config.MaxConcurrentReqsAtomic()))

// activeRequestCount 始终追踪当前活跃请求数（不仅 debug）
var activeRequestCount atomic.Int64

// ActiveRequestCount 返回当前活跃请求数
func ActiveRequestCount() int64 {
	return activeRequestCount.Load()
}

// ConcurrencyMiddleware 返回一个 gin 中间件，限制同时处理的代理请求数
// 当并发数达到上限时返回 429 而非排队等待（避免请求超时）
func ConcurrencyMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		maxConcurrent := int64(config.MaxConcurrentReqsAtomic())
		if maxConcurrent <= 0 {
			// 不限制并发
			activeRequestCount.Add(1)
			defer activeRequestCount.Add(-1)
			c.Next()
			return
		}

		// 尝试非阻塞获取信号量
		if !concurrencySem.TryAcquire(1) {
			// 信号量不可用（并发已满），返回 429
			active := activeRequestCount.Load()
			log.Printf("concurrency: rejecting request, active=%d limit=%d", active, maxConcurrent)
			writeConcurrencyLimitedResponse(c)
			c.Abort()
			return
		}

		activeRequestCount.Add(1)
		defer func() {
			activeRequestCount.Add(-1)
			concurrencySem.Release(1)
		}()

		c.Next()
	}
}

// writeConcurrencyLimitedResponse 返回并发限制错误，根据请求路径自动选择 OpenAI 或 Anthropic 格式
func writeConcurrencyLimitedResponse(c *gin.Context) {
	path := c.Request.URL.Path
	isAnthropic := strings.Contains(path, "/v1/messages") || strings.Contains(path, "/v1/responses")

	if isAnthropic {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"type":  "error",
			"error": gin.H{"type": "rate_limit_error", "message": fmt.Sprintf("concurrency limit reached (max %d concurrent requests). Please retry later.", config.MaxConcurrentReqsAtomic())},
		})
	} else {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error": gin.H{
				"message": fmt.Sprintf("concurrency limit reached (max %d concurrent requests). Please retry later.", config.MaxConcurrentReqsAtomic()),
				"type":    "rate_limit_error",
				"code":    "concurrency_limit",
			},
		})
	}
}

// UpdateConcurrencyLimit 动态调整并发上限
// 因为 semaphore 不支持动态调整大小，需要创建新的信号量
// 注意：正在进行的请求仍使用旧信号量，新请求使用新信号量，
// 短暂过渡期内实际并发可能略超新上限，但会自然收敛
func UpdateConcurrencyLimit(newLimit int) {
	oldLimit := config.MaxConcurrentReqsAtomic()
	if newLimit == oldLimit {
		return
	}
	config.SetMaxConcurrentReqs(newLimit)
	if newLimit > 0 {
		concurrencySem = semaphore.NewWeighted(int64(newLimit))
	}
	log.Printf("concurrency: limit updated from %d to %d", oldLimit, newLimit)
}
