package config

import (
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

var (
	Port             int
	APIPassword      string
	CKApiKey         string
	BaseURL          string
	Domain           string
	AuthStateURL     string
	AuthTokenURL     string
	TokenRefreshURL  string
	ChatURL          string
	ConfigURL        string
	CacheEnabled     bool
	CacheTTL         int
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
	CacheEnabled = getEnv("CACHE_ENABLED", "") == "true"
	ttl := getEnv("CACHE_TTL", "300")
	cacheTTL, err := strconv.Atoi(ttl)
	if err != nil || cacheTTL <= 0 {
		cacheTTL = 300
	}
	CacheTTL = cacheTTL
}

// ListenAddr 返回服务监听地址
func ListenAddr() string {
	return fmt.Sprintf("0.0.0.0:%d", Port)
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
