package auth

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"uniview-codebuddy-proxy/internal/config"

	"github.com/gin-gonic/gin"
)

// upstreamClient 用于非流式认证请求（带超时）
var upstreamClient = &http.Client{
	Timeout: 30 * time.Second,
}

// RegisterRoutes 注册 /auth/* 路由组
func RegisterRoutes(r *gin.Engine) {
	auth := r.Group("/auth")
	{
		if config.APIPassword != "" {
			auth.Use(authPasswordMiddleware())
		}
		auth.GET("/start", handleAuthStart)
		auth.GET("/poll", handleAuthPoll)
		auth.POST("/manual", handleAuthManual)
		auth.GET("/status", handleAuthStatus)
	}
}

// authPasswordMiddleware 验证 /auth/* 路由的 API_PASSWORD
func authPasswordMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if config.APIPassword == "" {
			c.Next()
			return
		}
		authHeader := c.GetHeader("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			token := strings.TrimPrefix(authHeader, "Bearer ")
			if subtle.ConstantTimeCompare([]byte(token), []byte(config.APIPassword)) == 1 {
				c.Next()
				return
			}
		}
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		c.Abort()
	}
}

// handleAuthStart 发起 OAuth2 Device Flow
func handleAuthStart(c *gin.Context) {
	authURL, authState, err := FetchAuthURL()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "auth_start_failed", "detail": err.Error()})
		return
	}

	// 自动打开浏览器让用户登录
	OpenBrowser(authURL)
	c.JSON(http.StatusOK, gin.H{
		"success":    true,
		"auth_state": authState,
		"auth_url":   authURL,
		"message":    "Please open auth_url in browser and login",
		"poll_url":   fmt.Sprintf("http://localhost:%d/auth/poll?auth_state=%s", config.Port, authState),
	})
}

// PollResult 表示轮询结果
type PollResult struct {
	Status    string // "pending", "success", "error"
	Message   string
	UserID    string
	ExpiresAt int64
	Detail    map[string]interface{}
}

// PollToken 向上游轮询 token 状态，返回轮询结果
func PollToken(authState string) *PollResult {
	url := fmt.Sprintf("%s?state=%s", config.AuthTokenURL, authState)
	headers := authPollHeaders()

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return &PollResult{Status: "error", Message: err.Error()}
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := upstreamClient.Do(req)
	if err != nil {
		return &PollResult{Status: "error", Message: err.Error()}
	}
	defer resp.Body.Close()

	var data map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return &PollResult{Status: "error", Message: "invalid response"}
	}

	// 11217 = 等待用户登录
	code, _ := data["code"].(float64)
	if code == 11217 {
		return &PollResult{Status: "pending", Message: "Waiting for login..."}
	}

	if code == 0 {
		d, _ := data["data"].(map[string]interface{})
		if d != nil {
			accessToken, _ := d["accessToken"].(string)
			if accessToken != "" {
				expiresIn := getInt(d, "expiresIn")
				if expiresIn == 0 {
					expiresIn = 3600
				}
				now := time.Now().Unix()
				userID := ExtractUserIDFromJWT(accessToken)
				if userID == "" {
					userID, _ = d["domain"].(string)
				}

				td := &TokenData{
					BearerToken:  accessToken,
					AccessToken:  accessToken,
					RefreshToken: getString(d, "refreshToken"),
					TokenType:    getString(d, "tokenType"),
					ExpiresIn:    expiresIn,
					Domain:       getString(d, "domain"),
					SessionState: getString(d, "sessionState"),
					CreatedAt:    now,
					ExpiresAt:    now + int64(expiresIn),
					UserID:       userID,
				}

				if err := SaveToken(td); err != nil {
					log.Printf("save token error: %v", err)
				}

				return &PollResult{
					Status:    "success",
					Message:   "Login success! Token saved.",
					UserID:    td.UserID,
					ExpiresAt: td.ExpiresAt,
				}
			}
		}
	}

	return &PollResult{Status: "error", Message: "auth_poll_failed", Detail: data}
}

// handleAuthPoll 轮询 token 状态
func handleAuthPoll(c *gin.Context) {
	authState := c.Query("auth_state")
	if authState == "" {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "auth_state is required"})
		return
	}

	result := PollToken(authState)
	switch result.Status {
	case "pending":
		c.JSON(http.StatusOK, gin.H{
			"status":  "pending",
			"message": result.Message,
			"code":    11217,
		})
	case "success":
		c.JSON(http.StatusOK, gin.H{
			"status":     "success",
			"message":    result.Message,
			"user_id":    result.UserID,
			"expires_at": result.ExpiresAt,
		})
	default:
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "detail": result.Detail})
	}
}

// handleAuthManual 手动设置 Bearer Token
func handleAuthManual(c *gin.Context) {
	var body struct {
		BearerToken string `json:"bearer_token"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	if body.BearerToken == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bearer_token is required"})
		return
	}

	now := time.Now().Unix()
	userID := ExtractUserIDFromJWT(body.BearerToken)

	td := &TokenData{
		BearerToken: body.BearerToken,
		AccessToken: body.BearerToken,
		CreatedAt:   now,
		ExpiresAt:   now + 86400,
		UserID:      userID,
	}

	// 从 JWT 解析实际过期时间
	exp := extractJWTExp(body.BearerToken)
	if exp > 0 {
		td.ExpiresAt = exp
	}

	if err := SaveToken(td); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "Token saved",
		"user_id": td.UserID,
	})
}

// handleAuthStatus 返回当前 token 状态
func handleAuthStatus(c *gin.Context) {
	td := LoadToken()
	if td == nil {
		c.JSON(http.StatusOK, gin.H{
			"has_token": false,
			"message":   "No token. Visit /auth/start",
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"has_token":  true,
		"user_id":    td.UserID,
		"expires_at": td.ExpiresAt,
		"created_at": td.CreatedAt,
	})
}

// ─── 请求头构建 ─────────────────────────────────────

func authStartHeaders() map[string]string {
	return map[string]string{
		"Host":                 config.Domain,
		"Accept":               "application/json, text/plain, */*",
		"Content-Type":         "application/json",
		"Cache-Control":        "no-cache",
		"Pragma":               "no-cache",
		"Connection":           "close",
		"X-Requested-With":     "XMLHttpRequest",
		"X-Domain":             config.Domain,
		"X-No-Authorization":   "true",
		"X-No-User-Id":         "true",
		"X-No-Enterprise-Id":   "true",
		"X-No-Department-Info": "true",
		"User-Agent":           "CLI/1.0.8 CodeBuddy/1.0.8",
		"X-Product":            "SaaS",
		"X-Request-ID":         generateRequestID(),
	}
}

func authPollHeaders() map[string]string {
	rid := generateRequestID()
	span := generateSpanID()
	return map[string]string{
		"Host":                 config.Domain,
		"Accept":               "application/json, text/plain, */*",
		"Cache-Control":        "no-cache",
		"Pragma":               "no-cache",
		"Connection":           "close",
		"X-Requested-With":     "XMLHttpRequest",
		"X-Request-ID":         rid,
		"b3":                   fmt.Sprintf("%s-%s-1-", rid, span),
		"X-B3-TraceId":         rid,
		"X-B3-ParentSpanId":    "",
		"X-B3-SpanId":          span,
		"X-B3-Sampled":         "1",
		"X-No-Authorization":   "true",
		"X-No-User-Id":         "true",
		"X-No-Enterprise-Id":   "true",
		"X-No-Department-Info": "true",
		"X-Domain":             config.Domain,
		"User-Agent":           "CLI/1.0.8 CodeBuddy/1.0.8",
		"X-Product":            "SaaS",
	}
}

// BuildUpstreamHeaders 构建发送到上游的请求头
func BuildUpstreamHeaders(model string) map[string]string {
	return map[string]string{
		"Accept":            "text/event-stream",
		"Content-Type":      "application/json",
		"X-Requested-With":  "XMLHttpRequest",
		"X-B3-ParentSpanId": "",
		"X-B3-Sampled":      "1",
		"X-Agent-Intent":    "CodeCompletion",
		"X-Env-ID":          "production",
		"X-Domain":          config.Domain,
		"X-Product":         "SaaS",
		"X-User-Id":         GetUserID(),
		"X-Machine-Id":      generateRequestID(),
		"X-Request-ID":      generateRequestID(),
		"User-Agent":        "CLI/1.0.8 CodeBuddy/1.0.8",
	}
}

// ─── 工具函数 ───────────────────────────────────────

func generateNonce() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func generateRequestID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand.Read failed: %v", err))
	}
	return hex.EncodeToString(b)
}

func generateSpanID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand.Read failed: %v", err))
	}
	return hex.EncodeToString(b)
}

func getString(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}

func getInt(m map[string]interface{}, key string) int {
	v, _ := m[key].(float64)
	return int(v)
}

func extractJWTExp(token string) int64 {
	parts := splitJWT(token)
	if len(parts) < 2 {
		return 0
	}
	payload, err := base64urlDecode(parts[1])
	if err != nil {
		return 0
	}
	var claims map[string]interface{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return 0
	}
	if exp, ok := claims["exp"].(float64); ok {
		return int64(exp)
	}
	return 0
}

func truncateJSON(data interface{}, maxLen int) string {
	s := fmt.Sprintf("%v", data)
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}

// OpenBrowser 在有 GUI 时打开浏览器，无 GUI 时在终端显示 URL
func OpenBrowser(url string) {
	if url == "" {
		return
	}
	if !isGUIAvailable() {
		printAuthURL(url)
		return
	}
	var cmd *exec.Cmd
	switch {
	case commandExists("open"):
		cmd = exec.Command("open", url)
	case commandExists("xdg-open"):
		cmd = exec.Command("xdg-open", url)
	case commandExists("rundll32"):
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		printAuthURL(url)
		return
	}
	if err := cmd.Start(); err != nil {
		log.Printf("Failed to open browser: %v", err)
		printAuthURL(url)
	}
}

// isGUIAvailable 检测当前环境是否有 GUI 可用
func isGUIAvailable() bool {
	if runtime.GOOS != "linux" {
		return true
	}
	return os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != ""
}

// printAuthURL 在终端输出登录 URL
func printAuthURL(url string) {
	fmt.Println()
	fmt.Println("============================================")
	fmt.Println("  请在浏览器中打开以下链接完成登录：")
	fmt.Println()
	fmt.Printf("  %s\n", url)
	fmt.Println("============================================")
	fmt.Println()
}

// commandExists 检查系统命令是否存在
func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// FetchAuthURL 调用上游 Device Flow 获取 CodeBuddy 登录 URL 和 state
func FetchAuthURL() (authURL string, authState string, err error) {
	nonce, nerr := generateNonce()
	if nerr != nil {
		return "", "", nerr
	}

	url := fmt.Sprintf("%s?platform=CLI&nonce=%s", config.AuthStateURL, nonce)
	headers := authStartHeaders()

	body := map[string]string{"nonce": nonce}
	bodyJSON, _ := json.Marshal(body)

	req, rerr := http.NewRequest("POST", url, bytes.NewReader(bodyJSON))
	if rerr != nil {
		return "", "", rerr
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, rerr := upstreamClient.Do(req)
	if rerr != nil {
		return "", "", rerr
	}
	defer resp.Body.Close()

	var data map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", "", fmt.Errorf("invalid response")
	}

	log.Printf("auth/state response: %v", truncateJSON(data, 200))

	code, _ := data["code"].(float64)
	if resp.StatusCode == 200 && code == 0 {
		d, _ := data["data"].(map[string]interface{})
		if d != nil {
			state, _ := d["state"].(string)
			aURL, _ := d["authUrl"].(string)
			if state != "" && aURL != "" {
				return aURL, state, nil
			}
		}
	}

	return "", "", fmt.Errorf("auth_start_failed: %v", truncateJSON(data, 200))
}
