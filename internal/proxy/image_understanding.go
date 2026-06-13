package proxy

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"uniview-codebuddy-proxy/internal/auth"
	"uniview-codebuddy-proxy/internal/config"
	"uniview-codebuddy-proxy/internal/telemetry"
)

// understandImages 遍历 body 中的 messages 和 system 字段，
// 对每个图片块调用 Vision 模型（glm-4.6v）自动解析为文本描述，用文本块替换图片块。
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
// 理解失败时保留占位文本，不丢失图片存在的信息。
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
					log.Printf("images: failed to auto-parse image_url block: %v", err)
					// 理解失败，保留占位文本（不丢失图片存在的信息）
					result = append(result, map[string]interface{}{
						"type": "text",
						"text": "[图片内容解析失败，原始图片已移除]",
					})
					telemetry.ReportImageUnderstandingFailure("image_url", err.Error())
				} else {
					result = append(result, map[string]interface{}{
						"type": "text",
						"text": fmt.Sprintf("[图片描述] %s", desc),
					})
					telemetry.ReportImageUnderstandingSuccess("image_url")
				}
				replaced = true
				continue
			}

			// Anthropic 格式: {"type": "image", "source": {"type": "base64"/"url", ...}}
			if typ == "image" {
				if src, _ := part["source"].(map[string]interface{}); src != nil {
					if srcType, _ := src["type"].(string); srcType == "base64" || srcType == "url" {
						desc, err := understandImage(part)
						if err != nil {
							log.Printf("images: failed to auto-parse image block: %v", err)
							result = append(result, map[string]interface{}{
								"type": "text",
								"text": "[图片内容解析失败，原始图片已移除]",
							})
							telemetry.ReportImageUnderstandingFailure("image", err.Error())
						} else {
							result = append(result, map[string]interface{}{
								"type": "text",
								"text": fmt.Sprintf("[图片描述] %s", desc),
							})
							telemetry.ReportImageUnderstandingSuccess("image")
						}
						replaced = true
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
			// 如果全部被替换后结果为空，保留空文本占位
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

// understandImage 调用 Vision 模型（默认 glm-4.6v）理解单张图片，返回文本描述。
// imageBlock 支持 OpenAI 格式 (type: "image_url") 和 Anthropic 格式 (type: "image")。
// 因为主模型（如 glm-5.1）不支持图片输入，所以通过专门的 Vision 模型来理解图片。
func understandImage(imageBlock map[string]interface{}) (string, error) {
	imageURL, err := extractImageData(imageBlock)
	if err != nil {
		return "", fmt.Errorf("extract image data failed: %w", err)
	}

	// 检查图片大小（base64 data URL 过大时可能导致超时）
	if strings.HasPrefix(imageURL, "data:") {
		// 粗略估算：base64 编码后大小约为原始数据的 4/3
		// 超过 10MB 的 base64 图片直接拒绝（约 7.5MB 原始数据）
		if len(imageURL) > 10*1024*1024 {
			return "", fmt.Errorf("image too large (%d bytes base64, limit 10MB)", len(imageURL))
		}
	}

	// 构造 Vision 模型请求
	// 使用 glm-4.6v 等支持 Vision 的模型，而非主请求模型（如 glm-5.1 不支持图片）
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
						"text": "请仔细观察这张图片并详细描述其内容。如果图片中包含文字，请逐字转录所有可见文字；如果是界面截图，描述界面布局和元素；如果是图表，描述数据和趋势；如果是照片，描述场景和物体。请用中文回答。",
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

	// 调用上游 API（带超时：base64 大图可能较慢，给 60s）
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 使用 "craft" intent，与正常对话请求一致
	// 上游根据 model 字段路由到 glm-4.6v，不依赖 intent 区分
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
