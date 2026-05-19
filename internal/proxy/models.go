package proxy

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"codebuddy-proxy/internal/auth"
	"codebuddy-proxy/internal/config"
)

// Model 表示 OpenAI 格式的模型条目
type Model struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// 额外模型：/v2/config 不返回但实测可用的模型
var extraModels = []Model{
	{ID: "glm-5.1", Object: "model", Created: 1700000000, OwnedBy: "zhipu"},
	{ID: "glm-5.0", Object: "model", Created: 1700000000, OwnedBy: "zhipu"},
	{ID: "glm-4.7", Object: "model", Created: 1700000000, OwnedBy: "zhipu"},
	{ID: "glm-4.6", Object: "model", Created: 1700000000, OwnedBy: "zhipu"},
	{ID: "minimax-m2.7", Object: "model", Created: 1700000000, OwnedBy: "minimax"},
	{ID: "minimax-m2.5", Object: "model", Created: 1700000000, OwnedBy: "minimax"},
	{ID: "kimi-k2.5", Object: "model", Created: 1700000000, OwnedBy: "moonshot"},
	{ID: "deepseek-r1", Object: "model", Created: 1700000000, OwnedBy: "deepseek"},
	{ID: "deepseek-v3-1-lkeap", Object: "model", Created: 1700000000, OwnedBy: "deepseek"},
	{ID: "hunyuan-2.0-instruct", Object: "model", Created: 1700000000, OwnedBy: "tencent"},
}

var (
	modelsCache   []Model
	modelsExpires int64
	modelsMu      sync.RWMutex
)

const modelsCacheTTL = 300 // 5 分钟缓存

// inferOwnedBy 根据模型名前缀推断 owned_by
func inferOwnedBy(name string) string {
	prefixes := map[string]string{
		"deepseek": "deepseek",
		"hunyuan":  "tencent",
		"glm":      "zhipu",
		"minimax":  "minimax",
		"kimi":     "moonshot",
	}
	for prefix, owner := range prefixes {
		if strings.HasPrefix(name, prefix) {
			return owner
		}
	}
	return "codebuddy"
}

// FetchModels 获取模型列表（带缓存）
func FetchModels() []Model {
	modelsMu.RLock()
	if modelsCache != nil && modelsExpires > time.Now().Unix() {
		result := make([]Model, len(modelsCache))
		copy(result, modelsCache)
		modelsMu.RUnlock()
		return result
	}
	modelsMu.RUnlock()

	modelsMu.Lock()
	defer modelsMu.Unlock()

	// 双检锁：等待写锁期间可能已被其他 goroutine 刷新
	if modelsCache != nil && modelsExpires > time.Now().Unix() {
		return modelsCache
	}

	// 以 extraModels 为基础
	result := make([]Model, len(extraModels))
	copy(result, extraModels)

	bearer := auth.GetBearerToken()

	// 请求 /v2/config
	headers := map[string]string{
		"X-Domain":  config.Domain,
		"X-Product": "SaaS",
	}
	if bearer != "" {
		headers["Authorization"] = "Bearer " + bearer
		headers["X-User-Id"] = auth.GetUserID()
	}

	req, err := http.NewRequest("GET", config.ConfigURL, nil)
	if err != nil {
		log.Printf("create config request error: %v", err)
		modelsCache = result
		modelsExpires = time.Now().Unix() + modelsCacheTTL
		return result
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	// 模型列表请求用独立超时控制
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	resp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("fetch /v2/config error: %v", err)
		modelsCache = result
		modelsExpires = time.Now().Unix() + modelsCacheTTL
		return result
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Printf("/v2/config returned status %d", resp.StatusCode)
		modelsCache = result
		modelsExpires = time.Now().Unix() + modelsCacheTTL
		return result
	}

	var configResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&configResp); err != nil {
		log.Printf("decode /v2/config error: %v", err)
		modelsCache = result
		modelsExpires = time.Now().Unix() + modelsCacheTTL
		return result
	}

	// 提取模型列表
	existingIDs := make(map[string]bool)
	for _, m := range result {
		existingIDs[m.ID] = true
	}

	models, _ := configResp["models"].([]interface{})
	for _, m := range models {
		mMap, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		// 提取 requestBody.model（实际模型标识）
		rb, _ := mMap["requestBody"].(map[string]interface{})
		realModel, _ := rb["model"].(string)
		if realModel == "" {
			continue
		}

		if !existingIDs[realModel] {
			result = append(result, Model{
				ID:      realModel,
				Object:  "model",
				Created: 1700000000,
				OwnedBy: inferOwnedBy(realModel),
			})
			existingIDs[realModel] = true
		}

		// 暴露 config 中的 name 作为别名
		configName, _ := mMap["name"].(string)
		if configName != "" && configName != realModel && !existingIDs[configName] {
			result = append(result, Model{
				ID:      configName,
				Object:  "model",
				Created: 1700000000,
				OwnedBy: inferOwnedBy(realModel),
			})
			existingIDs[configName] = true
		}
	}

	log.Printf("Fetched %d models from /v2/config", len(result))
	modelsCache = result
	modelsExpires = time.Now().Unix() + modelsCacheTTL
	return result
}
