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
	debugEnabled         atomic.Bool
	claudeInject         atomic.Bool
	maxRetries           atomic.Int32
	cbMaxFailures        atomic.Int32
	cbResetTimeoutSecs   atomic.Int32
	cooldownDurationSecs   atomic.Int32
	telemetryEnabled        atomic.Bool
	imageUnderstanding      atomic.Bool
	imageUnderstandingModel atomic.Value // string
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

	// 调试模式配置
	debugEnabled.Store(getEnv("DEBUG", "") == "true")

	// Claude Inject 配置：注入 Claude Code 兼容的请求头
	claudeInject.Store(getEnv("CLAUDE_INJECT", "") == "true")

	// 请求重试配置
	maxRetriesInt, err := strconv.Atoi(getEnv("MAX_RETRIES", "3"))
	if err != nil || maxRetriesInt < 0 {
		maxRetriesInt = 3
	}
	maxRetries.Store(int32(maxRetriesInt))

	// 熔断器配置
	cbMaxFailuresInt, err := strconv.Atoi(getEnv("CB_MAX_FAILURES", "5"))
	if err != nil || cbMaxFailuresInt < 1 {
		cbMaxFailuresInt = 5
	}
	cbMaxFailures.Store(int32(cbMaxFailuresInt))

	cbResetTimeoutInt, err := strconv.Atoi(getEnv("CB_RESET_TIMEOUT_SECS", "30"))
	if err != nil || cbResetTimeoutInt < 1 {
		cbResetTimeoutInt = 30
	}
	cbResetTimeoutSecs.Store(int32(cbResetTimeoutInt))

	// 凭证冷却时长配置
	cooldownDurationInt, err := strconv.Atoi(getEnv("COOLDOWN_DURATION_SECS", "30"))
	if err != nil || cooldownDurationInt < 1 {
		cooldownDurationInt = 30
	}
	cooldownDurationSecs.Store(int32(cooldownDurationInt))

	// 遥测上报开关
	telemetryEnabled.Store(getEnv("TELEMETRY_ENABLED", "true") == "true")

	// 自动图片解析开关（默认开启，用 Vision 模型解析图片内容为文本描述）
	imageUnderstanding.Store(getEnv("IMAGE_UNDERSTANDING", "true") == "true")

	// 自动图片解析模型（默认 glm-4.6v，需为支持 Vision 的模型）
	imageUnderstandingModel.Store(getEnv("IMAGE_UNDERSTANDING_MODEL", "glm-4.6v"))
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

// DebugEnabledAtomic 返回调试模式是否启用。
func DebugEnabledAtomic() bool { return debugEnabled.Load() }

// SetDebugEnabled 设置调试模式开关。
func SetDebugEnabled(v bool) { debugEnabled.Store(v) }

// ClaudeInjectAtomic 返回 Claude Code 兼容头注入是否启用。
func ClaudeInjectAtomic() bool { return claudeInject.Load() }

// SetClaudeInject 设置 Claude Code 兼容头注入开关。
func SetClaudeInject(v bool) { claudeInject.Store(v) }

// MaxRetriesAtomic 返回请求最大重试次数。
func MaxRetriesAtomic() int { return int(maxRetries.Load()) }

// SetMaxRetries 设置请求最大重试次数。
func SetMaxRetries(v int) {
	if v < 0 {
		v = 3
	}
	maxRetries.Store(int32(v))
}

// CBMaxFailuresAtomic 返回熔断器最大连续失败次数。
func CBMaxFailuresAtomic() int { return int(cbMaxFailures.Load()) }

// SetCBMaxFailures 设置熔断器最大连续失败次数。
func SetCBMaxFailures(v int) {
	if v < 1 {
		v = 5
	}
	cbMaxFailures.Store(int32(v))
}

// CBResetTimeoutSecsAtomic 返回熔断器重置超时时间（秒）。
func CBResetTimeoutSecsAtomic() int { return int(cbResetTimeoutSecs.Load()) }

// SetCBResetTimeoutSecs 设置熔断器重置超时时间（秒）。
func SetCBResetTimeoutSecs(v int) {
	if v < 1 {
		v = 30
	}
	cbResetTimeoutSecs.Store(int32(v))
}

// CooldownDurationSecsAtomic 返回凭证冷却时长（秒）。
func CooldownDurationSecsAtomic() int { return int(cooldownDurationSecs.Load()) }

// SetCooldownDurationSecs 设置凭证冷却时长（秒）。
func SetCooldownDurationSecs(v int) {
	if v < 1 {
		v = 30
	}
	cooldownDurationSecs.Store(int32(v))
}

// TelemetryEnabledAtomic 返回遥测上报是否启用。
func TelemetryEnabledAtomic() bool { return telemetryEnabled.Load() }

// SetTelemetryEnabled 设置遥测上报开关。
func SetTelemetryEnabled(v bool) { telemetryEnabled.Store(v) }

// ImageUnderstandingAtomic 返回是否启用自动图片解析（Vision 模型）。
func ImageUnderstandingAtomic() bool { return imageUnderstanding.Load() }

// SetImageUnderstanding 设置自动图片解析开关。
func SetImageUnderstanding(v bool) { imageUnderstanding.Store(v) }

// ImageUnderstandingModelAtomic 返回自动图片解析使用的模型名称。
func ImageUnderstandingModelAtomic() string {
	if v, ok := imageUnderstandingModel.Load().(string); ok {
		return v
	}
	return "glm-4.6v"
}

// SetImageUnderstandingModel 设置自动图片解析模型名称。
func SetImageUnderstandingModel(v string) {
	if v == "" {
		v = "glm-4.6v"
	}
	imageUnderstandingModel.Store(v)
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
