package auth

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"codebuddy-proxy/internal/config"
)

// TokenData 表示持久化的 token 数据
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

func credentialsFile() string {
	return filepath.Join(config.CredsDir, "token.json")
}

// LoadToken 从磁盘加载 token，过期返回 nil
func LoadToken() *TokenData {
	path := credentialsFile()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var td TokenData
	if err := json.Unmarshal(data, &td); err != nil {
		return nil
	}
	bearer := td.BearerToken
	if bearer == "" {
		bearer = td.AccessToken
	}
	if bearer == "" {
		return nil
	}
	if td.ExpiresAt > 0 && time.Now().Unix() > td.ExpiresAt {
		log.Println("Token expired")
		return nil
	}
	return &td
}

// SaveToken 将 token 持久化到磁盘
func SaveToken(td *TokenData) error {
	if err := os.MkdirAll(config.CredsDir, 0700); err != nil {
		return fmt.Errorf("create creds dir: %w", err)
	}
	data, err := json.MarshalIndent(td, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal token: %w", err)
	}
	return os.WriteFile(credentialsFile(), data, 0600)
}

// GetBearerToken 返回当前 bearer token，无 token 返回空字符串
func GetBearerToken() string {
	td := LoadToken()
	if td == nil {
		return ""
	}
	if td.BearerToken != "" {
		return td.BearerToken
	}
	return td.AccessToken
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
	// 按 . 分割，最多 3 段
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
