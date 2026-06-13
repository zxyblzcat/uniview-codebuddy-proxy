package proxy

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"uniview-codebuddy-proxy/internal/auth"
	"uniview-codebuddy-proxy/internal/config"
)

// Model 表示 OpenAI 格式的模型条目
type Model struct {
	ID               string `json:"id"`
	Object           string `json:"object"`
	Created          int64  `json:"created"`
	OwnedBy          string `json:"owned_by"`
	MaxContextWindow int    `json:"max_context_window,omitempty"`
}

// 模型上下文窗口大小映射
// 这些值用于让 Claude Code 等客户端正确判断上下文使用率，从而触发 autocompact
// 值基于各模型官方公布的上下文窗口大小
var modelContextWindows = map[string]int{
	// GLM 系列
	"glm-5.1": 200000,
	"glm-5.0": 200000,
	"glm-4.7": 200000,
	"glm-4.6": 200000,
	"glm-4.6v": 8192,
	"glm-4.5": 200000,
	"glm-4.4": 200000,
	// MiniMax
	"minimax-m2.7": 1000000,
	"minimax-m2.5": 245000,
	// Kimi
	"kimi-k2.5": 131072,
	// DeepSeek
	"deepseek-r1":            131072,
	"deepseek-r1-0528":       131072,
	"deepseek-r1-0528-lkeap": 131072,
	"deepseek-v3-1-lkeap":    131072,
	"deepseek-v3":            131072,
	"deepseek-v3-0324":       131072,
	// 混元
	"hunyuan-2.0-instruct": 131072,
	"hunyuan-chat":         131072,
	"hunyuan":              131072,
	"hunyuan-3b":           32768,
	// Anthropic (兜底)
	"claude-4.0":    200000,
	"claude-3.7":    200000,
	"claude-3.5":    200000,
	"claude-3-opus": 200000,
	// OpenAI
	"gpt-4.1":      1047576,
	"gpt-4.1-mini": 1047576,
	"gpt-4.1-nano": 1047576,
	// Google
	"gemini-2.5-pro":   1048576,
	"gemini-2.5-flash": 1048576,
}

// 额外模型：/v2/config 不返回但实测可用的模型
var extraModels = []Model{
	// 国产模型
	{ID: "glm-5.1", Object: "model", Created: 1700000000, OwnedBy: "zhipu", MaxContextWindow: 200000},
	{ID: "glm-5.0", Object: "model", Created: 1700000000, OwnedBy: "zhipu", MaxContextWindow: 200000},
	{ID: "glm-4.7", Object: "model", Created: 1700000000, OwnedBy: "zhipu", MaxContextWindow: 200000},
	{ID: "glm-4.6", Object: "model", Created: 1700000000, OwnedBy: "zhipu", MaxContextWindow: 200000},
	{ID: "glm-4.6v", Object: "model", Created: 1700000000, OwnedBy: "zhipu", MaxContextWindow: 8192},
	{ID: "minimax-m2.7", Object: "model", Created: 1700000000, OwnedBy: "minimax", MaxContextWindow: 1000000},
	{ID: "minimax-m2.5", Object: "model", Created: 1700000000, OwnedBy: "minimax", MaxContextWindow: 245000},
	{ID: "kimi-k2.5", Object: "model", Created: 1700000000, OwnedBy: "moonshot", MaxContextWindow: 131072},
	{ID: "deepseek-r1", Object: "model", Created: 1700000000, OwnedBy: "deepseek", MaxContextWindow: 131072},
	{ID: "deepseek-v3-1-lkeap", Object: "model", Created: 1700000000, OwnedBy: "deepseek", MaxContextWindow: 131072},
	{ID: "hunyuan-2.0-instruct", Object: "model", Created: 1700000000, OwnedBy: "tencent", MaxContextWindow: 131072},
	// 外部模型（兜底，/v2/config 动态获取失败时使用）
	{ID: "claude-4.0", Object: "model", Created: 1700000000, OwnedBy: "anthropic", MaxContextWindow: 200000},
	{ID: "claude-3.7", Object: "model", Created: 1700000000, OwnedBy: "anthropic", MaxContextWindow: 200000},
	{ID: "gpt-4.1", Object: "model", Created: 1700000000, OwnedBy: "openai", MaxContextWindow: 1047576},
	{ID: "gpt-4.1-mini", Object: "model", Created: 1700000000, OwnedBy: "openai", MaxContextWindow: 1047576},
	{ID: "gpt-4.1-nano", Object: "model", Created: 1700000000, OwnedBy: "openai", MaxContextWindow: 1047576},
	{ID: "gemini-2.5-pro", Object: "model", Created: 1700000000, OwnedBy: "google", MaxContextWindow: 1048576},
	{ID: "gemini-2.5-flash", Object: "model", Created: 1700000000, OwnedBy: "google", MaxContextWindow: 1048576},
}

var (
	modelsCache   []Model
	modelsExpires int64
	modelsMu      sync.RWMutex
)

const modelsCacheTTL = 300     // 5 分钟缓存
const modelsCacheTTLShort = 30 // 上游不可用时 30 秒缓存

// inferOwnedBy 根据模型名前缀推断 owned_by
func inferOwnedBy(name string) string {
	prefixes := map[string]string{
		"deepseek": "deepseek",
		"hunyuan":  "tencent",
		"glm":      "zhipu",
		"minimax":  "minimax",
		"kimi":     "moonshot",
		"claude":   "anthropic",
		"gpt":      "openai",
		"o1":       "openai",
		"o3":       "openai",
		"o4":       "openai",
		"gemini":   "google",
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
		result := make([]Model, len(modelsCache))
		copy(result, modelsCache)
		return result
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
		modelsExpires = time.Now().Unix() + modelsCacheTTLShort
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
		modelsExpires = time.Now().Unix() + modelsCacheTTLShort
		return result
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Printf("/v2/config returned status %d", resp.StatusCode)
		modelsCache = result
		modelsExpires = time.Now().Unix() + modelsCacheTTLShort
		return result
	}

	var configResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&configResp); err != nil {
		log.Printf("decode /v2/config error: %v", err)
		modelsCache = result
		modelsExpires = time.Now().Unix() + modelsCacheTTLShort
		return result
	}

	// 调试：输出上游 config 响应中第一个模型的完整字段，确认是否有上下文窗口信息
	if config.DebugEnabledAtomic() {
		if modelList, ok := configResp["models"].([]interface{}); ok && len(modelList) > 0 {
			if first, ok := modelList[0].(map[string]interface{}); ok {
				if b, err := json.MarshalIndent(first, "", "  "); err == nil {
					log.Printf("[DEBUG] /v2/config first model fields: %s", string(b))
				}
			}
			// 列出所有上游返回的模型 ID
			var ids []string
			for _, m := range modelList {
				if mm, ok := m.(map[string]interface{}); ok {
					if rb, ok := mm["requestBody"].(map[string]interface{}); ok {
						if mid, ok := rb["model"].(string); ok && mid != "" {
							ids = append(ids, mid)
						}
					} else if mid, ok := mm["name"].(string); ok && mid != "" {
						ids = append(ids, mid)
					}
				}
			}
			log.Printf("[DEBUG] /v2/config model IDs (%d): %v", len(ids), ids)
		}
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
				ID:               realModel,
				Object:           "model",
				Created:          1700000000,
				OwnedBy:          inferOwnedBy(realModel),
				MaxContextWindow: getModelContextWindow(realModel),
			})
			existingIDs[realModel] = true
		}

		// 暴露 config 中的 name 作为别名
		configName, _ := mMap["name"].(string)
		if configName != "" && configName != realModel && !existingIDs[configName] {
			result = append(result, Model{
				ID:               configName,
				Object:           "model",
				Created:          1700000000,
				OwnedBy:          inferOwnedBy(realModel),
				MaxContextWindow: getModelContextWindow(realModel),
			})
			existingIDs[configName] = true
		}
	}

	log.Printf("Fetched %d models from /v2/config", len(result))
	modelsCache = result
	modelsExpires = time.Now().Unix() + modelsCacheTTL
	return result
}

// getModelContextWindow 返回模型的上下文窗口大小，未知模型默认 200000
func getModelContextWindow(modelID string) int {
	if w, ok := modelContextWindows[modelID]; ok {
		return w
	}
	// 前缀匹配：按前缀长度降序排序，优先匹配更具体的前缀
	// 避免 "hunyuan-3b" 被 "hunyuan" 覆盖、或 "deepseek-v3-1-lkeap" 被 "deepseek-v3" 覆盖
	var bestPrefix string
	var bestW int
	for prefix, w := range modelContextWindows {
		if strings.HasPrefix(modelID, prefix) && len(prefix) > len(bestPrefix) {
			bestPrefix = prefix
			bestW = w
		}
	}
	if bestPrefix != "" {
		return bestW
	}
	return 200000 // 默认值
}

// AnthropicModelInfo 表示 Anthropic API 格式的模型信息
// 用于 GET /v1/models/:id 和 GET /v1/models 端点
// 必须包含 capabilities 字段，Claude Code 通过 capabilities.context_management 判断是否支持 auto-compact
type AnthropicModelInfo struct {
	ID              string                 `json:"id"`
	Type            string                 `json:"type"`
	DisplayName     string                 `json:"display_name"`
	CreatedAt       string                 `json:"created_at"`
	MaxInputTokens  int                    `json:"max_input_tokens"`
	MaxOutputTokens int                    `json:"max_output_tokens"`
	Capabilities    map[string]interface{} `json:"capabilities"`
}

// buildCapabilities 构建模型能力信息，兼容 Anthropic API 格式
// Claude Code 依赖 capabilities.context_management.compact_20260112.supported 来判断是否支持 auto-compact
// capabilities 是静态的模型能力信息，使用包级别变量避免每次请求都重新分配
// 每个键值使用独立的 map 字面量，避免共享引用导致潜在的 mutation 风险
var capabilities = map[string]interface{}{
	"batch":              map[string]interface{}{"supported": false},
	"citations":          map[string]interface{}{"supported": false},
	"code_execution":     map[string]interface{}{"supported": false},
	"context_management": map[string]interface{}{
		"clear_thinking_20251015": map[string]interface{}{"supported": true},
		"clear_tool_uses_20250919": map[string]interface{}{"supported": true},
		"compact_20260112":        map[string]interface{}{"supported": true},
		"supported":               map[string]interface{}{"supported": true},
	},
	"effort": map[string]interface{}{
		"high":      map[string]interface{}{"supported": true},
		"low":       map[string]interface{}{"supported": true},
		"max":       map[string]interface{}{"supported": true},
		"medium":    map[string]interface{}{"supported": true},
		"supported": map[string]interface{}{"supported": true},
		"xhigh":     map[string]interface{}{"supported": false},
	},
	"image_input":        map[string]interface{}{"supported": false},
	"pdf_input":          map[string]interface{}{"supported": false},
	"structured_outputs": map[string]interface{}{"supported": false},
	"thinking": map[string]interface{}{
		"supported": map[string]interface{}{"supported": false},
		"types": map[string]interface{}{
			"adaptive": map[string]interface{}{"supported": false},
			"enabled":  map[string]interface{}{"supported": false},
		},
	},
}

// buildCapabilities 返回模型能力信息的深拷贝，兼容 Anthropic API 格式
// Claude Code 依赖 capabilities.context_management.compact_20260112.supported 来判断是否支持 auto-compact
// 返回深拷贝而非共享引用，防止调用方意外修改包级别变量
func buildCapabilities() map[string]interface{} {
	// 使用 JSON 序列化/反序列化实现深拷贝，避免手动递归复制嵌套 map
	data, err := json.Marshal(capabilities)
	if err != nil {
		// 不应发生：capabilities 是硬编码的合法 JSON 结构
		return map[string]interface{}{}
	}
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return map[string]interface{}{}
	}
	return result
}

// FindModelByID 根据模型 ID 查找模型信息
func FindModelByID(modelID string) *Model {
	models := FetchModels()
	for i := range models {
		if models[i].ID == modelID {
			return &models[i]
		}
	}
	return nil
}

// HandleModelByID GET /v1/models/:id — Anthropic 格式的单个模型信息端点
// 返回 max_input_tokens 字段，使 Claude Code 能自动发现正确的上下文窗口大小
func HandleModelByID(c *gin.Context) {
	modelID := c.Param("id")
	if modelID == "" {
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{
				"type":    "not_found_error",
				"message": "Model not found",
			},
		})
		return
	}

	model := FindModelByID(modelID)
	if model == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": gin.H{
				"type":    "not_found_error",
				"message": "Model not found: " + modelID,
			},
		})
		return
	}

	// 将 MaxContextWindow 映射为 Anthropic 格式的 max_input_tokens
	maxInputTokens := model.MaxContextWindow
	if maxInputTokens == 0 {
		maxInputTokens = getModelContextWindow(model.ID)
	}

	// MaxOutputTokens 设置为上下文窗口的 1/4，确保客户端有合理的输出空间
	// 过低的值会导致 Claude Code 等客户端过早截断长输出
	maxOutputTokens := maxInputTokens / 4
	if maxOutputTokens < 8192 {
		maxOutputTokens = 8192
	}

	info := AnthropicModelInfo{
		ID:              model.ID,
		Type:            "model",
		DisplayName:     model.ID,
		CreatedAt:       time.Unix(model.Created, 0).UTC().Format(time.RFC3339),
		MaxInputTokens:  maxInputTokens,
		MaxOutputTokens: maxOutputTokens,
		Capabilities:    buildCapabilities(),
	}

	c.JSON(http.StatusOK, info)
}
