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
	ConfigURL       string
)

var (
	cacheEnabled atomic.Bool
	cacheTTL     atomic.Int32
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
	ConfigURL = BaseURL + "/v2/config"

	cacheEnabled.Store(getEnv("CACHE_ENABLED", "") == "true")
	ttl := getEnv("CACHE_TTL", "300")
	cacheTTL, err := strconv.Atoi(ttl)
	if err != nil || cacheTTL <= 0 {
		cacheTTL = 300
	}
	SetCacheTTL(cacheTTL)
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

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
