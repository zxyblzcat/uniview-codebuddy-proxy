package auth

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const cooldownDuration = 30 * time.Second

// TokenEntry 是 pool 中的一个 token 条目
type TokenEntry struct {
	Token      *TokenData
	Cooldown   time.Time // 429 后的冷却截止时间
	Unavailable bool      // 401 后标记不可用
}

// TokenPool 管理多个 token 的轮换和健康状态
type TokenPool struct {
	mu      sync.Mutex
	entries []*TokenEntry
	index   int // 轮询索引
}

var (
	globalPool     *TokenPool
	globalPoolOnce sync.Once
)

// GetPool 返回全局 TokenPool 单例
func GetPool() *TokenPool {
	globalPoolOnce.Do(func() {
		globalPool = &TokenPool{}
		globalPool.loadFromDisk()
	})
	return globalPool
}

// loadFromDisk 从 tokens/ 目录和旧 token.json 加载所有凭证
func (p *TokenPool) loadFromDisk() {
	p.mu.Lock()
	defer p.mu.Unlock()

	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	baseDir := filepath.Join(home, ".codebuddy-proxy")

	// 先从 tokens/ 目录加载
	tokensDir := filepath.Join(baseDir, "tokens")
	entries, err := os.ReadDir(tokensDir)
	if err == nil {
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
				continue
			}
			data, err := os.ReadFile(filepath.Join(tokensDir, e.Name()))
			if err != nil {
				continue
			}
			var td TokenData
			if err := json.Unmarshal(data, &td); err != nil {
				continue
			}
			if td.ExpiresAt > 0 && time.Now().Unix() > td.ExpiresAt+5 {
				continue // 跳过过期的
			}
			p.entries = append(p.entries, &TokenEntry{Token: &td})
		}
	}

	// 兼容旧的单 token.json
	legacyPath := filepath.Join(baseDir, "token.json")
	if len(p.entries) == 0 {
		data, err := os.ReadFile(legacyPath)
		if err == nil {
			var td TokenData
			if err := json.Unmarshal(data, &td); err == nil {
				if td.ExpiresAt <= 0 || time.Now().Unix() <= td.ExpiresAt+5 {
					p.entries = append(p.entries, &TokenEntry{Token: &td})
				}
			}
		}
	}

	log.Printf("TokenPool loaded %d token(s)", len(p.entries))
}

// NextToken 返回下一个可用的 token（健康度感知轮换）
func (p *TokenPool) NextToken() *TokenData {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.entries) == 0 {
		return nil
	}

	now := time.Now()
	n := len(p.entries)

	// 从当前索引开始尝试，跳过不可用和冷却中的
	for i := 0; i < n; i++ {
		idx := (p.index + i) % n
		e := p.entries[idx]
		if e.Unavailable {
			continue
		}
		if now.Before(e.Cooldown) {
			continue
		}
		// 检查过期
		if e.Token.ExpiresAt > 0 && now.Unix() > e.Token.ExpiresAt+5 {
			continue
		}
		bearer := e.Token.BearerToken
		if bearer == "" {
			bearer = e.Token.AccessToken
		}
		if bearer == "" {
			continue
		}
		// 可用：推进索引到下一个
		p.index = (idx + 1) % n
		return e.Token
	}

	// 所有 token 都不可用或冷却中，尝试找冷却期最短即将结束的
	var best *TokenEntry
	for _, e := range p.entries {
		if e.Unavailable {
			continue
		}
		if best == nil || e.Cooldown.Before(best.Cooldown) {
			best = e
		}
	}
	if best != nil && best.Token != nil {
		bearer := best.Token.BearerToken
		if bearer == "" {
			bearer = best.Token.AccessToken
		}
		if bearer != "" {
			return best.Token
		}
	}

	return nil
}

// MarkCooldown 标记某个 token 进入冷却期（429 后调用）
func (p *TokenPool) MarkCooldown(userID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, e := range p.entries {
		if e.Token.UserID == userID {
			e.Cooldown = time.Now().Add(cooldownDuration)
			log.Printf("Token for %s cooled down for %v", userID, cooldownDuration)
		}
	}
}

// MarkUnavailable 标记某个 token 不可用（401 后调用）
func (p *TokenPool) MarkUnavailable(userID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, e := range p.entries {
		if e.Token.UserID == userID {
			e.Unavailable = true
			log.Printf("Token for %s marked unavailable", userID)
		}
	}
}

// AddToken 添加一个新 token 到 pool
func (p *TokenPool) AddToken(td *TokenData) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// 避免重复
	for _, e := range p.entries {
		if e.Token.UserID == td.UserID {
			e.Token = td
			e.Unavailable = false
			e.Cooldown = time.Time{}
			return saveTokenEntryToDisk(td)
		}
	}

	p.entries = append(p.entries, &TokenEntry{Token: td})
	return saveTokenEntryToDisk(td)
}

// RemoveToken 从 pool 中删除一个 token
func (p *TokenPool) RemoveToken(userID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	newEntries := make([]*TokenEntry, 0, len(p.entries))
	for _, e := range p.entries {
		if e.Token.UserID == userID {
			continue
		}
		newEntries = append(newEntries, e)
	}
	p.entries = newEntries

	return removeTokenEntryFromDisk(userID)
}

// GetAllTokens 返回所有 token 的信息（用于 API 展示）
func (p *TokenPool) GetAllTokens() []TokenInfo {
	p.mu.Lock()
	defer p.mu.Unlock()

	result := make([]TokenInfo, 0, len(p.entries))
	now := time.Now()
	for _, e := range p.entries {
		info := TokenInfo{
			UserID:      e.Token.UserID,
			ExpiresAt:   e.Token.ExpiresAt,
			CreatedAt:   e.Token.CreatedAt,
			Unavailable: e.Unavailable,
			InCooldown:  now.Before(e.Cooldown),
			CooldownUntil: e.Cooldown,
		}
		if e.Token.ExpiresAt > 0 && now.Unix() > e.Token.ExpiresAt+5 {
			info.Expired = true
		}
		result = append(result, info)
	}
	return result
}

// TokenInfo 用于 API 展示的 token 信息
type TokenInfo struct {
	UserID        string    `json:"user_id"`
	ExpiresAt     int64     `json:"expires_at"`
	CreatedAt     int64     `json:"created_at"`
	Expired       bool      `json:"expired"`
	Unavailable   bool      `json:"unavailable"`
	InCooldown    bool      `json:"in_cooldown"`
	CooldownUntil time.Time `json:"cooldown_until,omitempty"`
}

// tokenEntryFilename 生成 token 文件名
func tokenEntryFilename(td *TokenData) string {
	h := sha256.Sum256([]byte(td.UserID))
	return fmt.Sprintf("%s_%x.json", sanitizeFilename(td.UserID), h[:4])
}

func sanitizeFilename(s string) string {
	result := make([]byte, 0, len(s))
	for i := 0; i < len(s) && len(result) < 32; i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '-' {
			result = append(result, c)
		} else if c >= 'A' && c <= 'Z' {
			result = append(result, c+32) // to lowercase
		} else if c == '@' || c == '.' {
			result = append(result, '_')
		}
	}
	if len(result) == 0 {
		return "token"
	}
	return string(result)
}

// saveTokenEntryToDisk 将 token 保存到 tokens/ 目录
func saveTokenEntryToDisk(td *TokenData) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot determine home dir: %w", err)
	}
	tokensDir := filepath.Join(home, ".codebuddy-proxy", "tokens")
	if err := os.MkdirAll(tokensDir, 0700); err != nil {
		return fmt.Errorf("cannot create tokens dir: %w", err)
	}

	filename := tokenEntryFilename(td)
	path := filepath.Join(tokensDir, filename)

	data, err := json.MarshalIndent(td, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot marshal token: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("cannot write token file: %w", err)
	}
	return nil
}

// removeTokenEntryFromDisk 删除 tokens/ 目录中的 token 文件
func removeTokenEntryFromDisk(userID string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	tokensDir := filepath.Join(home, ".codebuddy-proxy", "tokens")
	entries, err := os.ReadDir(tokensDir)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := filepath.Join(tokensDir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var td TokenData
		if err := json.Unmarshal(data, &td); err != nil {
			continue
		}
		if td.UserID == userID {
			os.Remove(path)
			return nil
		}
	}
	return nil
}
