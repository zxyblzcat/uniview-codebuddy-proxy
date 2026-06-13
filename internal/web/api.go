package web

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"uniview-codebuddy-proxy/internal/auth"
	"uniview-codebuddy-proxy/internal/cache"
	"uniview-codebuddy-proxy/internal/circuitbreaker"
	"uniview-codebuddy-proxy/internal/config"
	"uniview-codebuddy-proxy/internal/i18n"
	"uniview-codebuddy-proxy/internal/logbuf"

	"github.com/gin-gonic/gin"
	"golang.org/x/text/language"
)

var (
	logWriter *logbuf.MultiWriter
)

// RegisterAPIRoutes 注册 /api/* 后端 API 路由
func RegisterAPIRoutes(r *gin.Engine, lw *logbuf.MultiWriter) {
	logWriter = lw
	api := r.Group("/api")
	if config.APIPassword != "" {
		api.Use(auth.APIPasswordMiddleware())
	}
	api.GET("/config", handleGetConfig)
	api.PUT("/config", handlePutConfig)
	api.GET("/logs/stream", handleLogStream)
	api.DELETE("/logs", handleClearLogs)
	api.GET("/locale", handleGetLocale)
	api.PUT("/locale", handlePutLocale)
}

func handleGetConfig(c *gin.Context) {
	cbState, cbFailures, cbLastFailure := circuitbreaker.GetBreaker().Stats()
	c.JSON(http.StatusOK, gin.H{
		"port":                   config.Port,
		"api_password_set":       config.APIPassword != "",
		"cache_enabled":          config.CacheEnabledAtomic() && cache.GlobalCache.IsEnabled(),
		"cache_ttl":              config.CacheTTLAtomic(),
		"base_url":               config.BaseURL,
		"locale":                 i18n.GetLocale().String(),
		"debug_enabled":          config.DebugEnabledAtomic(),
		"claude_inject":          config.ClaudeInjectAtomic(),
		"max_retries":            config.MaxRetriesAtomic(),
		"cb_state":               cbState.String(),
		"cb_failures":            cbFailures,
		"cb_last_failure":        cbLastFailure,
		"cb_max_failures":        config.CBMaxFailuresAtomic(),
		"cb_reset_timeout_secs":  config.CBResetTimeoutSecsAtomic(),
		"cooldown_duration_secs": config.CooldownDurationSecsAtomic(),
		"telemetry_enabled":            config.TelemetryEnabledAtomic(),
		"drop_images_when_unsupported": config.DropImagesWhenUnsupportedAtomic(),
		"image_understanding":        config.ImageUnderstandingAtomic(),
		"image_understanding_model":  config.ImageUnderstandingModelAtomic(),
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
		config.SetCacheEnabled(v)
		cache.GlobalCache.SetEnabled(v)
	}
	if v, ok := body["cache_ttl"].(float64); ok {
		ttl := int(v)
		config.SetCacheTTL(ttl)
		cache.GlobalCache.SetTTL(time.Duration(config.CacheTTLAtomic()) * time.Second)
	}
	if v, ok := body["debug_enabled"].(bool); ok {
		config.SetDebugEnabled(v)
	}
	if v, ok := body["claude_inject"].(bool); ok {
		config.SetClaudeInject(v)
	}
	if v, ok := body["max_retries"].(float64); ok {
		config.SetMaxRetries(int(v))
	}
	if v, ok := body["cb_max_failures"].(float64); ok {
		config.SetCBMaxFailures(int(v))
	}
	if v, ok := body["cb_reset_timeout_secs"].(float64); ok {
		config.SetCBResetTimeoutSecs(int(v))
	}
	if v, ok := body["cooldown_duration_secs"].(float64); ok {
		config.SetCooldownDurationSecs(int(v))
	}
	if v, ok := body["telemetry_enabled"].(bool); ok {
		config.SetTelemetryEnabled(v)
	}
 if v, ok := body["drop_images_when_unsupported"].(bool); ok {
		config.SetDropImagesWhenUnsupported(v)
	}
	if v, ok := body["image_understanding"].(bool); ok {
		config.SetImageUnderstanding(v)
	}
	if v, ok := body["image_understanding_model"].(string); ok {
		config.SetImageUnderstandingModel(v)
	}
	// 熔断器重置
	if v, ok := body["cb_reset"].(bool); ok && v {
		circuitbreaker.GetBreaker().Reset()
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func handleClearLogs(c *gin.Context) {
	if logWriter == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "log writer not available"})
		return
	}
	logWriter.Clear()
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func handleLogStream(c *gin.Context) {
	if logWriter == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "log writer not available"})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming not supported"})
		return
	}

	// Send existing log lines as initial backlog.
	for _, line := range logWriter.Lines() {
		fmt.Fprintf(c.Writer, "data: %s\n\n", sseEscape(line))
	}
	flusher.Flush()

	// Subscribe to new log lines.
	ch, unsubscribe := logWriter.Subscribe()
	defer unsubscribe()

	timeout := time.After(5 * time.Minute)
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case line, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(c.Writer, "data: %s\n\n", sseEscape(line))
			flusher.Flush()
		case <-heartbeat.C:
			fmt.Fprintf(c.Writer, "event: ping\ndata: {}\n\n")
			flusher.Flush()
		case <-c.Request.Context().Done():
			return
		case <-timeout:
			return
		}
	}
}

// sseEscape escapes a string for use in an SSE data field.
// Per the SSE spec, only literal \n needs escaping (as two separate data: lines
// would be interpreted as two events). We replace newlines with   to keep
// each log entry as a single SSE data field.
func sseEscape(s string) string {
	return strings.ReplaceAll(s, "\n", " ")
}

func handleGetLocale(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"locale": i18n.GetLocale().String(),
	})
}

func handlePutLocale(c *gin.Context) {
	var body struct {
		Locale string `json:"locale"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Locale == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	tag, err := language.Parse(body.Locale)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid locale"})
		return
	}
	i18n.SetLocale(tag)
	c.JSON(http.StatusOK, gin.H{"status": "ok", "locale": tag.String()})
}

// embedFS 已在 embed.go 中通过 go:embed 定义

// SetupAdminUI 设置 /admin/ 路由提供前端 SPA
func SetupAdminUI(r *gin.Engine) {
	distFS := DistFS

	// 注意：/admin 路由不添加 APIPasswordMiddleware，因为浏览器导航请求无法携带
	// Authorization header。SPA 静态文件本身不包含敏感数据；所有数据访问通过
	// /api/* 路由，这些路由已有自己的认证中间件。
	admin := r.Group("/admin")

	admin.GET("", func(c *gin.Context) {
		data, err := distFS.ReadFile("dist/index.html")
		if err != nil {
			c.String(http.StatusNotFound, "admin UI not found")
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", data)
	})

	admin.GET("/*filepath", func(c *gin.Context) {
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

		c.Data(http.StatusOK, contentType, data)
	})
}

