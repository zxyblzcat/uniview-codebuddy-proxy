package auth

import (
	"encoding/base64"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
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
func saveTokenToFile(td *TokenData) {
	p := tokenFilePath()
	if p == "" {
		log.Println("Warning: cannot determine token file path, skipping persist")
		return
	}

	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0700); err != nil {
		log.Printf("Warning: failed to create token dir %s: %v", dir, err)
		return
	}

	data, err := json.MarshalIndent(td, "", "  ")
	if err != nil {
		log.Printf("Warning: failed to marshal token: %v", err)
		return
	}

	if err := os.WriteFile(p, data, 0600); err != nil {
		log.Printf("Warning: failed to write token file %s: %v", p, err)
	}
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

	// 文件中的 token 已过期则删除文件
	if td.ExpiresAt > 0 && time.Now().Unix() > td.ExpiresAt {
		log.Printf("Token file expired, removing %s", p)
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			log.Printf("Warning: failed to remove expired token file %s: %v", p, err)
		}
		return nil
	}

	return &td
}

// LoadToken 从内存或文件加载 token，过期时清除缓存和文件并触发自动登录
//
// 使用写锁路径避免 RLock→RUnlock→Lock 升级中的竞态条件：
// 在 RUnlock 和 Lock 之间，另一个 goroutine 可能已保存新 token，
// 旧代码会丢失该 token 导致瞬时 401。
func LoadToken() *TokenData {
	tokenMu.Lock()

	if cachedToken != nil {
		bearer := cachedToken.BearerToken
		if bearer == "" {
			bearer = cachedToken.AccessToken
		}
		if bearer != "" {
			if cachedToken.ExpiresAt > 0 && time.Now().Unix() > cachedToken.ExpiresAt {
				log.Println("Token expired, clearing cache and triggering auto-login")
				filePath := tokenFilePath()
				cachedToken = nil
				tokenMu.Unlock()
				// 锁外执行 I/O 和触发重登录
				if filePath != "" {
					if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
						log.Printf("Warning: failed to remove expired token file %s: %v", filePath, err)
					}
				}
				go triggerAutoRelogin()
				return nil
			}
			result := cachedToken
			tokenMu.Unlock()
			return result
		}
	}
	tokenMu.Unlock()

	// 内存缓存为空，尝试从文件加载
	td := loadTokenFromFile()
	if td == nil {
		return nil
	}

	tokenMu.Lock()
	if cachedToken == nil {
		cachedToken = td
	}
	result := cachedToken
	tokenMu.Unlock()

	return result
}

// triggerAutoRelogin 触发后台自动重新登录
func triggerAutoRelogin() {
	reloginMu.Lock()
	if reloginInProgress {
		reloginMu.Unlock()
		return
	}
	reloginInProgress = true
	// 创建新的 reloginDone channel 并保存到局部变量
	// 必须在锁内创建并赋值，在锁外用局部变量关闭，
	// 避免 Unlock→close 之间新 relogin 覆盖 reloginDone 导致等待者丢失信号
	ch := make(chan struct{})
	reloginDone = ch
	reloginMu.Unlock()

	defer func() {
		reloginMu.Lock()
		reloginInProgress = false
		reloginMu.Unlock()
		close(ch)
	}()

	// 尝试自动重新登录
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

// SaveToken 将 token 缓存到内存并持久化到文件
func SaveToken(td *TokenData) error {
	tokenMu.Lock()
	cachedToken = td
	tokenMu.Unlock()
	// 在锁外持久化和通知，避免死锁
	saveTokenToFile(td)
	return nil
}

// GetBearerToken 返回当前 bearer token，如果正在重登录则等待最多 3 分钟
func GetBearerToken() string {
	td := LoadToken()
	if td != nil {
		if td.BearerToken != "" {
			return td.BearerToken
		}
		return td.AccessToken
	}

	// Token 为空，检查是否有重登录正在进行
	reloginMu.Lock()
	inProgress := reloginInProgress
	ch := reloginDone
	reloginMu.Unlock()

	if !inProgress {
		// 重登录可能刚完成，再试一次
		td = LoadToken()
		if td != nil {
			if td.BearerToken != "" {
				return td.BearerToken
			}
			return td.AccessToken
		}
		return ""
	}

	// 等待重登录完成，最多 3 分钟
	if ch != nil {
		select {
		case <-ch:
			// 重登录完成，重新加载 token
			td = LoadToken()
			if td != nil {
				if td.BearerToken != "" {
					return td.BearerToken
				}
				return td.AccessToken
			}
		case <-time.After(3 * time.Minute):
			log.Println("Timed out waiting for token reload")
		}
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
	// 优先级: email > preferred_username > sub
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
	// 补齐 base64url padding
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	}
	return base64.URLEncoding.DecodeString(s)
}
