package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

var (
	Port         int
	APIPassword  string
	CKApiKey     string
	BaseURL      string
	Domain       string
	AuthStateURL string
	AuthTokenURL string
	ChatURL      string
	ConfigURL    string
)

func init() {
	// 加载 .env 文件（忽略不存在的错误，允许纯环境变量运行）
	_ = godotenv.Load()

	Port, _ = strconv.Atoi(getEnv("PORT", "1026"))
	APIPassword = getEnv("API_PASSWORD", "")
	CKApiKey = getEnv("CODEBUDDY_API_KEY", "")

	BaseURL = "https://unvcoding.copilot.qq.com"
	Domain = "unvcoding.copilot.qq.com"
	AuthStateURL = BaseURL + "/v2/plugin/auth/state"
	AuthTokenURL = BaseURL + "/v2/plugin/auth/token"
	ChatURL = BaseURL + "/v2/chat/completions"
	ConfigURL = BaseURL + "/v2/config"
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
