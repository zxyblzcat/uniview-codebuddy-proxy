package auth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"uniview-codebuddy-proxy/internal/config"
)

// TokenData 表示缓存的 token 数据
type TokenData struct {
	BearerToken  string `json:"bearer_token"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Domain       string `json:"domain"`
	SessionState string `json:"session_state"`
	CreatedAt    int64  `json:"created_at"`
	ExpiresAt    int64  `json:"expires_at"`
	UserID       string `json:"user_id"`
}

// 内存中的 token 缓存
var (
	cachedToken       *TokenData
	tokenMu           sync.Mutex
	reloginMu         sync.Mutex
	reloginInProgress bool
	reloginDone       chan struct{} // relogin 完成时关闭此 channel
	fileLoaded        bool          // 是否已尝试从文件加载（重置后可再次加载）
)

// tokenFilePath 返回 token 文件路径，优先使用 TOKEN_FILE_PATH 环境变量
func tokenFilePath() string {
	if p := os.Getenv("TOKEN_FILE_PATH"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codebuddy-proxy", "token.json")
}

// saveTokenToFile 将 TokenData 序列化写入文件，权限 0600
func saveTokenToFile(td *TokenData) error {
	p := tokenFilePath()
	if p == "" {
		return fmt.Errorf("cannot determine token file path")
	}

	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create token dir %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(td, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal token: %w", err)
	}

	if err := os.WriteFile(p, data, 0600); err != nil {
		return fmt.Errorf("failed to write token file %s: %w", p, err)
	}
	return nil
}

// loadTokenFromFile 从文件加载 TokenData，文件不存在或已过期返回 nil
func loadTokenFromFile() *TokenData {
	p := tokenFilePath()
	if p == "" {
		log.Println("Warning: cannot determine token file path, skipping load")
		return nil
	}

	data, err := os.ReadFile(p)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("Warning: failed to read token file %s: %v", p, err)
		}
		return nil
	}

	var td TokenData
	if err := json.Unmarshal(data, &td); err != nil {
		log.Printf("Warning: failed to parse token file %s: %v", p, err)
		return nil
	}

	// 文件中的 token 已过期（加 5 秒容错时钟漂移）则删除文件
	if td.ExpiresAt > 0 && time.Now().Unix() > td.ExpiresAt+5 {
		log.Printf("Token file expired, removing %s", p)
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			log.Printf("Warning: failed to remove expired token file %s: %v", p, err)
		}
		return nil
	}

	return &td
}

// LoadToken 从内存或文件加载 token，过期时清除缓存和文件并触发自动登录
func LoadToken() *TokenData {
	tokenMu.Lock()

	if cachedToken != nil {
		bearer := cachedToken.BearerToken
		if bearer == "" {
			bearer = cachedToken.AccessToken
		}
		if bearer != "" {
			if cachedToken.ExpiresAt > 0 && time.Now().Unix() > cachedToken.ExpiresAt+5 {
				log.Println("Token expired, clearing cache and triggering auto-login")
				filePath := tokenFilePath()
				cachedToken = nil
				fileLoaded = false
				if filePath != "" {
					if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
						log.Printf("Warning: failed to remove expired token file %s: %v", filePath, err)
					}
				}
				tokenMu.Unlock()
				go triggerAutoRelogin()
				return nil
			}
			result := cachedToken
			tokenMu.Unlock()
			return result
		}
	}

	// 内存缓存为空，尝试从文件加载（允许重试）
	if !fileLoaded {
		fileLoaded = true
		tokenMu.Unlock()
		td := loadTokenFromFile()
		if td != nil {
			tokenMu.Lock()
			if cachedToken == nil {
				cachedToken = td
			}
			result := cachedToken
			tokenMu.Unlock()
			return result
		}
		// SaveToken 可能在文件加载期间被调用，重新检查缓存
		tokenMu.Lock()
		result := cachedToken
		tokenMu.Unlock()
		return result
	}

	result := cachedToken
	tokenMu.Unlock()
	return result
}

// RefreshToken 尝试使用 refresh_token 刷新 access_token
func RefreshToken(td *TokenData) (*TokenData, error) {
	if td == nil || td.RefreshToken == "" {
		return nil, fmt.Errorf("no refresh token available")
	}

	req, err := http.NewRequest("POST", config.TokenRefreshURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create refresh request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+td.AccessToken)
	req.Header.Set("X-Refresh-Token", td.RefreshToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("refresh returned %d: %s", resp.StatusCode, string(body[:min(len(body), 300)]))
	}

	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("invalid refresh response: %w", err)
	}

	code, _ := data["code"].(float64)
	if code != 0 {
		return nil, fmt.Errorf("refresh failed with code %v", code)
	}

	d, _ := data["data"].(map[string]interface{})
	if d == nil {
		return nil, fmt.Errorf("refresh response missing data field")
	}

	accessToken, _ := d["accessToken"].(string)
	if accessToken == "" {
		return nil, fmt.Errorf("refresh response missing accessToken")
	}

	now := time.Now().Unix()
	expiresIn := getIntFromRefresh(d, "expiresIn")
	if expiresIn == 0 {
		expiresIn = 3600
	}
	userID := ExtractUserIDFromJWT(accessToken)
	if userID == "" {
		userID, _ = d["domain"].(string)
	}

	newTD := &TokenData{
		BearerToken:  accessToken,
		AccessToken:  accessToken,
		RefreshToken: getStringFromRefresh(d, "refreshToken"),
		TokenType:    getStringFromRefresh(d, "tokenType"),
		ExpiresIn:    expiresIn,
		Domain:       getStringFromRefresh(d, "domain"),
		SessionState: getStringFromRefresh(d, "sessionState"),
		CreatedAt:    now,
		ExpiresAt:    now + int64(expiresIn),
		UserID:       userID,
	}

	if newTD.RefreshToken == "" {
		newTD.RefreshToken = td.RefreshToken
	}

	if err := SaveToken(newTD); err != nil {
		log.Printf("save refreshed token error: %v", err)
	}

	return newTD, nil
}

func getIntFromRefresh(m map[string]interface{}, key string) int {
	v, _ := m[key].(float64)
	return int(v)
}

func getStringFromRefresh(m map[string]interface{}, key string) string {
	v, _ := m[key].(string)
	return v
}

// triggerAutoRelogin 触发后台自动重新登录
func triggerAutoRelogin() {
	reloginMu.Lock()
	if reloginInProgress {
		reloginMu.Unlock()
		return
	}
	reloginInProgress = true
	ch := make(chan struct{})
	reloginDone = ch
	reloginMu.Unlock()

	defer func() {
		reloginMu.Lock()
		reloginInProgress = false
		reloginMu.Unlock()
		close(ch)
	}()

	// 先尝试用 refresh_token 刷新
	tokenMu.Lock()
	oldToken := cachedToken
	tokenMu.Unlock()
	if oldToken != nil && oldToken.RefreshToken != "" {
		newTD, err := RefreshToken(oldToken)
		if err != nil {
			log.Printf("Token refresh failed, falling back to device flow: %v", err)
		} else {
			log.Printf("Token refresh success! User: %s", newTD.UserID)
			return
		}
	}

	// 刷新失败，走 Device Flow
	authURL, authState, err := FetchAuthURL()
	if err != nil {
		log.Printf("Auto-relogin failed (FetchAuthURL): %v", err)
		return
	}

	log.Println("Auto-relogin: please complete login in browser...")
	OpenBrowser(authURL)

	// 后台轮询等待登录完成
	for i := 0; i < 60; i++ {
		result := PollToken(authState)
		if result.Status == "success" {
			log.Printf("Auto-relogin success! User: %s", result.UserID)
			return
		}
		if result.Status == "error" {
			log.Printf("Auto-relogin poll error: %s", result.Message)
			return
		}
		time.Sleep(3 * time.Second)
	}
	log.Println("Auto-relogin timed out after 3 minutes")
}

// ClearToken clears the in-memory token cache and deletes the token file (logout).
func ClearToken() {
	tokenMu.Lock()
	cachedToken = nil
	fileLoaded = false
	p := tokenFilePath()
	tokenMu.Unlock()
	if p != "" {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			log.Printf("Warning: failed to remove token file %s: %v", p, err)
		}
	}
	log.Println("Token cleared (logout)")
}

// SaveToken 将 token 缓存到内存并持久化到文件
func SaveToken(td *TokenData) error {
	tokenMu.Lock()
	cachedToken = td
	fileLoaded = true
	tokenMu.Unlock()
	return saveTokenToFile(td)
}

// GetBearerToken 返回当前 bearer token，如果正在重登录则等待最多 3 分钟
func GetBearerToken() string {
	// 先在 tokenMu 内原子地检查缓存和 relogin 状态
	tokenMu.Lock()
	if cachedToken != nil {
		bearer := cachedToken.BearerToken
		if bearer == "" {
			bearer = cachedToken.AccessToken
		}
		if bearer != "" && (cachedToken.ExpiresAt <= 0 || time.Now().Unix() <= cachedToken.ExpiresAt+5) {
			tokenMu.Unlock()
			return bearer
		}
	}

	// 缓存为空或已过期，检查 relogin 状态
	reloginMu.Lock()
	inProgress := reloginInProgress
	ch := reloginDone
	reloginMu.Unlock()
	tokenMu.Unlock()

	if !inProgress {
		// 没有 relogin 进行中，尝试触发一个
		go triggerAutoRelogin()
		// 重新获取 reloginDone channel
		reloginMu.Lock()
		ch = reloginDone
		reloginMu.Unlock()
		if ch == nil {
			return ""
		}
	}

	// 等待重登录完成，最多 3 分钟
	select {
	case <-ch:
		td := LoadToken()
		if td != nil {
			if td.BearerToken != "" {
				return td.BearerToken
			}
			return td.AccessToken
		}
	case <-time.After(3 * time.Minute):
		log.Println("Timed out waiting for token reload")
	}

	return ""
}

// GetUserID 返回当前 user_id
func GetUserID() string {
	td := LoadToken()
	if td == nil {
		return ""
	}
	return td.UserID
}

// ExtractUserIDFromJWT 从 JWT token 中解析 user_id
func ExtractUserIDFromJWT(token string) string {
	parts := splitJWT(token)
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64urlDecode(parts[1])
	if err != nil {
		return ""
	}
	var claims map[string]interface{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}
	for _, key := range []string{"email", "preferred_username", "sub"} {
		if v, ok := claims[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

func splitJWT(token string) []string {
	result := make([]string, 0, 3)
	start := 0
	for i := 0; i < len(token) && len(result) < 2; i++ {
		if token[i] == '.' {
			result = append(result, token[start:i])
			start = i + 1
		}
	}
	result = append(result, token[start:])
	return result
}

func base64urlDecode(s string) ([]byte, error) {
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	}
	return base64.URLEncoding.DecodeString(s)
}
