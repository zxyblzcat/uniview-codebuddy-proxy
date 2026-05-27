package config

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"sync/atomic"

	"github.com/joho/godotenv"
)

var (
	Port            int
	APIPassword     string
	CKApiKey        string
	BaseURL         string
	Domain          string
	AuthStateURL    string
	AuthTokenURL    string
	TokenRefreshURL string
	ChatURL         string
	CompletionURL   string
	EmbeddingURL    string
	ConfigURL       string
	ReportURL       string
	ModelReportURL  string
)

var (
	cacheEnabled        atomic.Bool
	cacheTTL             atomic.Int32
	logMaxSizeMB         atomic.Int32
	logCleanupInterval   atomic.Int32
)

func init() {
	// 加载 .env 文件（忽略不存在的错误，允许纯环境变量运行）
	_ = godotenv.Load()

	portStr := getEnv("PORT", "1026")
	var err error
	Port, err = strconv.Atoi(portStr)
	if err != nil {
		log.Printf("Warning: invalid PORT value %q, using default 1026", portStr)
		Port = 1026
	}
	APIPassword = getEnv("API_PASSWORD", "")
	CKApiKey = getEnv("CODEBUDDY_API_KEY", "")

	BaseURL = "https://unvcoding.copilot.qq.com"
	Domain = "unvcoding.copilot.qq.com"
	AuthStateURL = BaseURL + "/v2/plugin/auth/state"
	AuthTokenURL = BaseURL + "/v2/plugin/auth/token"
	TokenRefreshURL = BaseURL + "/v2/plugin/auth/token/refresh"
	ChatURL = BaseURL + "/v2/chat/completions"
	CompletionURL = BaseURL + "/v2/completions"
	EmbeddingURL = BaseURL + "/v2/embeddings"
	ConfigURL = BaseURL + "/v2/config"
	ReportURL = BaseURL + "/v2/report"
	ModelReportURL = BaseURL + "/llm/data/report"

	cacheEnabled.Store(getEnv("CACHE_ENABLED", "") == "true")
	ttl := getEnv("CACHE_TTL", "300")
	cacheTTL, err := strconv.Atoi(ttl)
	if err != nil || cacheTTL <= 0 {
		cacheTTL = 300
	}
	SetCacheTTL(cacheTTL)

	// 日志清理配置
	maxSizeStr := getEnv("LOG_MAX_SIZE_MB", "50")
	maxSize, err := strconv.Atoi(maxSizeStr)
	if err != nil || maxSize <= 0 {
		maxSize = 50
	}
	SetLogMaxSizeMB(maxSize)

	intervalStr := getEnv("LOG_CLEANUP_INTERVAL", "1800")
	interval, err := strconv.Atoi(intervalStr)
	if err != nil || interval <= 0 {
		interval = 1800
	}
	SetLogCleanupInterval(interval)
}

// ListenAddr 返回服务监听地址
func ListenAddr() string {
	return fmt.Sprintf("0.0.0.0:%d", Port)
}

// CacheEnabledAtomic returns the cache enabled flag (thread-safe).
func CacheEnabledAtomic() bool { return cacheEnabled.Load() }

// SetCacheEnabled sets the cache enabled flag (thread-safe).
func SetCacheEnabled(v bool) { cacheEnabled.Store(v) }

// CacheTTLAtomic returns the cache TTL (thread-safe).
func CacheTTLAtomic() int { return int(cacheTTL.Load()) }

// SetCacheTTL sets the cache TTL (thread-safe).
func SetCacheTTL(v int) {
	if v <= 0 {
		v = 300
	}
	cacheTTL.Store(int32(v))
}

// LogMaxSizeMBAtomic 返回日志文件大小上限（MB）。
func LogMaxSizeMBAtomic() int { return int(logMaxSizeMB.Load()) }

// SetLogMaxSizeMB 设置日志文件大小上限（MB）。
func SetLogMaxSizeMB(v int) {
	if v <= 0 {
		v = 50
	}
	logMaxSizeMB.Store(int32(v))
}

// LogCleanupIntervalAtomic 返回后台清理间隔（秒）。
func LogCleanupIntervalAtomic() int { return int(logCleanupInterval.Load()) }

// SetLogCleanupInterval 设置后台清理间隔（秒）。
func SetLogCleanupInterval(v int) {
	if v <= 0 {
		v = 1800
	}
	logCleanupInterval.Store(int32(v))
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
