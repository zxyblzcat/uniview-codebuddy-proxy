package cache

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// CacheEntry 缓存的响应
type CacheEntry struct {
	Data      json.RawMessage `json:"data"`
	CreatedAt time.Time       `json:"created_at"`
}

// Cache 简单的 TTL 内存缓存
type Cache struct {
	mu    sync.RWMutex
	store map[string]*CacheEntry
	ttl   time.Duration
	enabled bool
}

// New 创建新的缓存实例
func New(ttl time.Duration) *Cache {
	return &Cache{
		store:   make(map[string]*CacheEntry),
		ttl:     ttl,
		enabled: false,
	}
}

// SetEnabled 启用或禁用缓存
func (c *Cache) SetEnabled(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.enabled = enabled
	if enabled && c.store == nil {
		c.store = make(map[string]*CacheEntry)
	}
}

// IsEnabled 返回缓存是否启用
func (c *Cache) IsEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.enabled
}

// SetTTL 设置缓存 TTL
func (c *Cache) SetTTL(ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ttl = ttl
}

// Key 构建缓存 key
func Key(model string, messages, tools interface{}, temperature float64) string {
	h := sha256.New()
	h.Write([]byte(model))
	if messages != nil {
		b, _ := json.Marshal(messages)
		h.Write(b)
	}
	if tools != nil {
		b, _ := json.Marshal(tools)
		h.Write(b)
	}
	h.Write([]byte(fmt.Sprintf("%v", temperature)))
	return fmt.Sprintf("%x", h.Sum(nil))
}

// Get 获取缓存条目
func (c *Cache) Get(key string) json.RawMessage {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.enabled {
		return nil
	}

	entry, ok := c.store[key]
	if !ok {
		return nil
	}

	if time.Since(entry.CreatedAt) > c.ttl {
		return nil
	}

	return entry.Data
}

// Set 设置缓存条目
func (c *Cache) Set(key string, data json.RawMessage) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.enabled {
		return
	}

	if c.store == nil {
		c.store = make(map[string]*CacheEntry)
	}

	c.store[key] = &CacheEntry{
		Data:      data,
		CreatedAt: time.Now(),
	}

	// 惰性清理过期条目
	if len(c.store) > 10000 {
		go c.cleanup()
	}
}

// Invalidate 使匹配前缀的缓存失效
func (c *Cache) Invalidate(prefix string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.store {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			delete(c.store, k)
		}
	}
}

// Clear 清空所有缓存
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store = make(map[string]*CacheEntry)
}

// cleanup 删除过期条目
func (c *Cache) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for k, v := range c.store {
		if now.Sub(v.CreatedAt) > c.ttl {
			delete(c.store, k)
		}
	}
}

// GlobalCache 全局缓存实例
var GlobalCache = New(300 * time.Second)
