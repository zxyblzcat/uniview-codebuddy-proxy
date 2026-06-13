package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPersistedConfigSaveAndLoad(t *testing.T) {
	// 保存原始值
	origTelemetry := TelemetryEnabledAtomic()
	origImageUnderstanding := ImageUnderstandingAtomic()
	origModel := ImageUnderstandingModelAtomic()
	defer func() {
		SetTelemetryEnabled(origTelemetry)
		SetImageUnderstanding(origImageUnderstanding)
		SetImageUnderstandingModel(origModel)
	}()

	// 设置新值
	SetTelemetryEnabled(false)
	SetImageUnderstanding(false)
	SetImageUnderstandingModel("test-model-v1")

	// 验证 config.json 被写入
	path := persistConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("config.json should exist after Set: %v", err)
	}

	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("config.json should be valid JSON: %v", err)
	}

	if cfg["telemetry_enabled"] != false {
		t.Errorf("telemetry_enabled should be false, got %v", cfg["telemetry_enabled"])
	}
	if cfg["image_understanding"] != false {
		t.Errorf("image_understanding should be false, got %v", cfg["image_understanding"])
	}
	if cfg["image_understanding_model"] != "test-model-v1" {
		t.Errorf("image_understanding_model should be test-model-v1, got %v", cfg["image_understanding_model"])
	}

	// 模拟重启：手动调用 loadPersistedConfig 验证能读回
	// 先重置为默认值
	telemetryEnabled.Store(true)
	imageUnderstanding.Store(true)
	imageUnderstandingModel.Store("glm-4.6v")

	// 加载持久化配置
	loadPersistedConfig()

	// 验证读回
	if TelemetryEnabledAtomic() != false {
		t.Errorf("after loadPersistedConfig, telemetry should be false, got %v", TelemetryEnabledAtomic())
	}
	if ImageUnderstandingAtomic() != false {
		t.Errorf("after loadPersistedConfig, image_understanding should be false, got %v", ImageUnderstandingAtomic())
	}
	if ImageUnderstandingModelAtomic() != "test-model-v1" {
		t.Errorf("after loadPersistedConfig, model should be test-model-v1, got %v", ImageUnderstandingModelAtomic())
	}

	// 恢复原始值（同时清理 config.json）
	SetTelemetryEnabled(true)
	SetImageUnderstanding(true)
	SetImageUnderstandingModel("glm-4.6v")
}

func TestPersistedConfigPreservesOtherFields(t *testing.T) {
	// 确保写入不会覆盖 config.json 中其他字段（如 locale）
	path := persistConfigPath()
	dir := filepath.Dir(path)
	os.MkdirAll(dir, 0700)

	// 先写入一个带 locale 的 config.json
	initial := map[string]interface{}{
		"locale": "zh-CN",
	}
	data, _ := json.MarshalIndent(initial, "", "  ")
	os.WriteFile(path, data, 0600)

	// 调用 Set
	SetTelemetryEnabled(true)

	// 验证 locale 还在
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("config.json should exist: %v", err)
	}
	var cfg map[string]interface{}
	json.Unmarshal(data, &cfg)
	if cfg["locale"] != "zh-CN" {
		t.Errorf("locale should be preserved, got %v", cfg["locale"])
	}
	if cfg["telemetry_enabled"] != true {
		t.Errorf("telemetry_enabled should be true, got %v", cfg["telemetry_enabled"])
	}
}
