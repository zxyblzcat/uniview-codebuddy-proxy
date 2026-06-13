package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"uniview-codebuddy-proxy/internal/auth"
	"uniview-codebuddy-proxy/internal/config"
	"uniview-codebuddy-proxy/internal/telemetry"
)

// understandImages 遍历 body 中的 messages 和 system 字段，
// 对每个图片块调用 Vision 模型自动解析为文本描述，用文本块替换图片块。
// 返回是否进行了替换。
func understandImages(body map[string]interface{}) bool {
	replaced := false

	// 处理 messages 中的图片
	if messages, ok := body["messages"].([]interface{}); ok {
		for _, msg := range messages {
			m, _ := msg.(map[string]interface{})
			if m == nil {
				continue
			}
			if replaceImagesWithDescriptions(m, "content") {
				replaced = true
			}
		}
	}

	// 处理 system 中的图片（Anthropic 格式）
	if _, ok := body["system"]; ok {
		if replaceImagesWithDescriptions(body, "system") {
			replaced = true
		}
	}

	return replaced
}

// replaceImagesWithDescriptions 遍历指定字段中的图片内容块，
// 调用 Vision 模型生成描述后替换为文本块。
// 递归处理 tool_result 嵌套内容。
func replaceImagesWithDescriptions(parent map[string]interface{}, key string) bool {
	content, exists := parent[key]
	if !exists {
		return false
	}

	switch c := content.(type) {
	case []interface{}:
		replaced := false
		result := make([]interface{}, 0, len(c))
		for _, item := range c {
			part, _ := item.(map[string]interface{})
			if part == nil {
				result = append(result, item)
				continue
			}
			typ, _ := part["type"].(string)

			// OpenAI 格式: {"type": "image_url", "image_url": {"url": "..."}}
			if typ == "image_url" {
				desc, err := understandImage(part)
				if err != nil {
					log.Printf("images: failed to auto-parse image_url block: %v, stripping instead", err)
					// 理解失败，跳过（等同于剥离）
					telemetry.ReportImageUnderstandingFailure("image_url", err.Error())
					replaced = true
					continue
				}
				result = append(result, map[string]interface{}{
					"type": "text",
					"text": fmt.Sprintf("[图片描述] %s", desc),
				})
				replaced = true
				telemetry.ReportImageUnderstandingSuccess("image_url")
				continue
			}

			// Anthropic 格式: {"type": "image", "source": {"type": "base64"/"url", ...}}
			if typ == "image" {
				if src, _ := part["source"].(map[string]interface{}); src != nil {
					if srcType, _ := src["type"].(string); srcType == "base64" || srcType == "url" {
						desc, err := understandImage(part)
						if err != nil {
							log.Printf("images: failed to auto-parse image block: %v, stripping instead", err)
							telemetry.ReportImageUnderstandingFailure("image", err.Error())
							replaced = true
							continue
						}
						result = append(result, map[string]interface{}{
							"type": "text",
							"text": fmt.Sprintf("[图片描述] %s", desc),
						})
						replaced = true
						telemetry.ReportImageUnderstandingSuccess("image")
						continue
					}
				}
			}

			// 递归处理 tool_result 嵌套 content 中的图片
			if typ == "tool_result" {
				if replaceImagesWithDescriptions(part, "content") {
					replaced = true
				}
			}

			result = append(result, item)
		}

		if replaced {
			// 如果全部被替换/剥离后结果为空，保留空文本占位
			if len(result) == 0 {
				parent[key] = []interface{}{map[string]interface{}{"type": "text", "text": ""}}
			} else {
				parent[key] = result
			}
		}
		return replaced
	}

	return false
}

// understandImage 调用 Vision 模型理解单张图片，返回文本描述。
// imageBlock 支持 OpenAI 格式 (type: "image_url") 和 Anthropic 格式 (type: "image")。
func understandImage(imageBlock map[string]interface{}) (string, error) {
	imageURL, err := extractImageData(imageBlock)
	if err != nil {
		return "", fmt.Errorf("extract image data failed: %w", err)
	}

	// 构造 Vision 模型请求
	model := config.ImageUnderstandingModelAtomic()
	payload := map[string]interface{}{
		"model": model,
		"messages": []interface{}{
			map[string]interface{}{
				"role": "user",
				"content": []interface{}{
					map[string]interface{}{
						"type":      "image_url",
						"image_url": map[string]interface{}{"url": imageURL},
					},
					map[string]interface{}{
						"type": "text",
						"text": "请详细描述这张图片的内容，包括其中的文字、界面元素、图表数据等所有可见信息。",
					},
				},
			},
		},
		"stream":     false,
		"max_tokens": 1024,
	}

	// 获取 Bearer token
	bearer := auth.GetBearerToken()
	if bearer == "" {
		return "", fmt.Errorf("no bearer token available")
	}

	// 调用上游 API（带超时）
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := CollectUpstreamChunks(ctx, payload, bearer, nil)
	if err != nil {
		return "", fmt.Errorf("vision model request failed: %w", err)
	}

	if result.StatusCode != 200 {
		return "", fmt.Errorf("vision model returned status %d: %s", result.StatusCode, result.ErrorText)
	}

	// 拼接所有内容片段
	var sb strings.Builder
	for i, part := range result.ContentParts {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(part)
	}

	description := strings.TrimSpace(sb.String())
	if description == "" {
		return "", fmt.Errorf("vision model returned empty description")
	}

	log.Printf("images: auto-parsed image using %s (%d tokens in, %d tokens out)",
		model, result.PromptTokens, result.CompletionTokens)

	return description, nil
}

// extractImageData 从图片内容块中提取统一的图片 URL（data URL 或 http URL）。
// 支持 OpenAI 格式和 Anthropic 格式。
func extractImageData(imageBlock map[string]interface{}) (string, error) {
	typ, _ := imageBlock["type"].(string)

	// OpenAI 格式: {"type": "image_url", "image_url": {"url": "..."}}
	if typ == "image_url" {
		iu, _ := imageBlock["image_url"].(map[string]interface{})
		if iu == nil {
			return "", fmt.Errorf("missing image_url field")
		}
		url, _ := iu["url"].(string)
		if url == "" {
			return "", fmt.Errorf("empty url in image_url")
		}
		return url, nil
	}

	// Anthropic 格式: {"type": "image", "source": {...}}
	if typ == "image" {
		src, _ := imageBlock["source"].(map[string]interface{})
		if src == nil {
			return "", fmt.Errorf("missing source field in image block")
		}
		srcType, _ := src["type"].(string)

		switch srcType {
		case "base64":
			mediaType, _ := src["media_type"].(string)
			if mediaType == "" {
				mediaType = "image/png"
			}
			data, _ := src["data"].(string)
			if data == "" {
				return "", fmt.Errorf("empty data in base64 image source")
			}
			return fmt.Sprintf("data:%s;base64,%s", mediaType, data), nil

		case "url":
			url, _ := src["url"].(string)
			if url == "" {
				return "", fmt.Errorf("empty url in image source")
			}
			return url, nil

		default:
			return "", fmt.Errorf("unsupported image source type: %s", srcType)
		}
	}

	return "", fmt.Errorf("unsupported image block type: %s", typ)
}

// extractImageDataFromJSON 从 json.RawMessage 中提取图片数据（用于 Responses API）。
// input 可能是数组或单条消息。
func extractImageDataFromRawMessages(raw json.RawMessage) ([]map[string]interface{}, error) {
	// 尝试解析为数组
	var arr []map[string]interface{}
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr, nil
	}

	// 尝试解析为单条消息
	var single map[string]interface{}
	if err := json.Unmarshal(raw, &single); err == nil {
		return []map[string]interface{}{single}, nil
	}

	return nil, fmt.Errorf("cannot parse input as message(s)")
}
