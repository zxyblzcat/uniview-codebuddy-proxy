package web

import (
	"fmt"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"uniview-codebuddy-proxy/internal/auth"
	"uniview-codebuddy-proxy/internal/cache"
	"uniview-codebuddy-proxy/internal/config"

	"github.com/gin-gonic/gin"
)

var (
	totalRequests atomic.Int64
	successCount  atomic.Int64
	errorCount    atomic.Int64
	modelsUsed    syncMap
	startTime     = time.Now()
)

type syncMap struct {
	mu   sync.Mutex
	data map[string]int64
}

func (m *syncMap) Incr(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.data == nil {
		m.data = make(map[string]int64)
	}
	m.data[key]++
}

func (m *syncMap) Get() map[string]int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make(map[string]int64, len(m.data))
	for k, v := range m.data {
		result[k] = v
	}
	return result
}

// RecordRequest 记录请求统计
func RecordRequest(model string, success bool) {
	totalRequests.Add(1)
	if success {
		successCount.Add(1)
	} else {
		errorCount.Add(1)
	}
	if model != "" {
		modelsUsed.Incr(model)
	}
}

// RegisterAPIRoutes 注册 /api/* 后端 API 路由
func RegisterAPIRoutes(r *gin.Engine) {
	api := r.Group("/api")
	if config.APIPassword != "" {
		api.Use(auth.APIPasswordMiddleware())
	}
	api.GET("/config", handleGetConfig)
	api.PUT("/config", handlePutConfig)
	api.GET("/stats", handleGetStats)
	api.GET("/logs/stream", handleLogStream)
}

func handleGetConfig(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"port":             config.Port,
		"api_password_set": config.APIPassword != "",
		"cache_enabled":    config.CacheEnabled && cache.GlobalCache.IsEnabled(),
		"cache_ttl":        config.CacheTTL,
		"base_url":         config.BaseURL,
	})
}

func handlePutConfig(c *gin.Context) {
	var body map[string]interface{}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	// 更新缓存设置（热重载）
	if v, ok := body["cache_enabled"].(bool); ok {
		config.CacheEnabled = v
		cache.GlobalCache.SetEnabled(v)
	}
	if v, ok := body["cache_ttl"].(float64); ok {
		ttl := int(v)
		if ttl <= 0 {
			ttl = 300
		}
		config.CacheTTL = ttl
		cache.GlobalCache.SetTTL(time.Duration(ttl) * time.Second)
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func handleGetStats(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"total_requests": totalRequests.Load(),
		"success_count":  successCount.Load(),
		"error_count":    errorCount.Load(),
		"models_used":    modelsUsed.Get(),
		"uptime_seconds": int64(time.Since(startTime).Seconds()),
	})
}

func handleLogStream(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming not supported"})
		return
	}

	// 保持连接 5 分钟
	timeout := time.After(5 * time.Minute)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// 发送心跳
			fmt.Fprintf(c.Writer, "event: ping\ndata: {}\n\n")
			flusher.Flush()
		case <-c.Request.Context().Done():
			return
		case <-timeout:
			return
		}
	}
}

// embedFS 已在 embed.go 中通过 go:embed 定义

// SetupAdminUI 设置 /admin/ 路由提供前端 SPA
func SetupAdminUI(r *gin.Engine) {
	distFS := DistFS

	r.GET("/admin", func(c *gin.Context) {
		data, err := distFS.ReadFile("dist/index.html")
		if err != nil {
			c.String(http.StatusNotFound, "admin UI not found")
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", data)
	})

	r.GET("/admin/*filepath", func(c *gin.Context) {
		fp := c.Param("filepath")
		if fp == "/" || fp == "" {
			fp = "/index.html"
		}
		data, err := distFS.ReadFile("dist" + fp)
		if err != nil {
			// SPA fallback: 返回 index.html
			data, err = distFS.ReadFile("dist/index.html")
			if err != nil {
				c.String(http.StatusNotFound, "not found")
				return
			}
			c.Data(http.StatusOK, "text/html; charset=utf-8", data)
			return
		}

		contentType := "application/octet-stream"
		switch {
		case len(fp) > 5 && fp[len(fp)-5:] == ".html":
			contentType = "text/html; charset=utf-8"
		case len(fp) > 4 && fp[len(fp)-4:] == ".css":
			contentType = "text/css; charset=utf-8"
		case len(fp) > 3 && fp[len(fp)-3:] == ".js":
			contentType = "application/javascript; charset=utf-8"
		case len(fp) > 4 && fp[len(fp)-4:] == ".svg":
			contentType = "image/svg+xml"
		case len(fp) > 4 && fp[len(fp)-4:] == ".png":
			contentType = "image/png"
		case len(fp) > 4 && fp[len(fp)-4:] == ".ico":
			contentType = "image/x-icon"
		}

		log.Printf("Serving admin file: dist%s (%s)", fp, contentType)
		c.Data(http.StatusOK, contentType, data)
	})
}
