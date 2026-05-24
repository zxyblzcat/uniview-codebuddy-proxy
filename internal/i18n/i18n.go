package i18n

import (
	"embed"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

//go:embed locales/*
var localeFS embed.FS

var (
	bundle    *i18n.Bundle
	localizer *i18n.Localizer
	localeMu  sync.RWMutex
	curLocale language.Tag
	onChange  []func()
)

func init() {
	bundle = i18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("json", json.Unmarshal)

	entries, err := localeFS.ReadDir("locales")
	if err != nil {
		log.Printf("Warning: failed to read locales directory: %v", err)
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := localeFS.ReadFile("locales/" + entry.Name())
		if err != nil {
			log.Printf("Warning: failed to read locale file %s: %v", entry.Name(), err)
			continue
		}
		if _, err := bundle.ParseMessageFileBytes(data, entry.Name()); err != nil {
			log.Printf("Warning: failed to parse locale file %s: %v", entry.Name(), err)
		}
	}

	sysLocale := detectSystemLocale()
	curLocale = sysLocale
	localizer = i18n.NewLocalizer(bundle, sysLocale.String())
}

// T translates a message by key, with optional template data.
func T(key string, data ...map[string]interface{}) string {
	localeMu.RLock()
	loc := localizer
	localeMu.RUnlock()

	cfg := &i18n.LocalizeConfig{MessageID: key}
	if len(data) > 0 {
		cfg.TemplateData = data[0]
	}
	msg, err := loc.Localize(cfg)
	if err != nil {
		return key
	}
	return msg
}

// SetLocale changes the active locale, persists to config, and notifies callbacks.
func SetLocale(tag language.Tag) {
	localeMu.Lock()
	curLocale = tag
	localizer = i18n.NewLocalizer(bundle, tag.String())
	callbacks := make([]func(), len(onChange))
	copy(callbacks, onChange)
	localeMu.Unlock()

	saveLocalePreference(tag.String())

	for _, fn := range callbacks {
		fn()
	}
}

// GetLocale returns the current locale tag.
func GetLocale() language.Tag {
	localeMu.RLock()
	defer localeMu.RUnlock()
	return curLocale
}

// OnChange registers a callback that fires when locale changes.
func OnChange(fn func()) {
	localeMu.Lock()
	onChange = append(onChange, fn)
	localeMu.Unlock()
}

// InitLocaleWithPreference sets locale from persisted preference.
func InitLocaleWithPreference(pref string) {
	if pref == "" {
		return
	}
	tag, err := language.Parse(pref)
	if err != nil {
		return
	}
	localeMu.Lock()
	curLocale = tag
	localizer = i18n.NewLocalizer(bundle, tag.String())
	localeMu.Unlock()
}

func configFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codebuddy-proxy", "config.json")
}

func loadConfigFile() map[string]interface{} {
	path := configFilePath()
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil
	}
	return cfg
}

func saveConfigFile(cfg map[string]interface{}) {
	path := configFilePath()
	if path == "" {
		return
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		log.Printf("Warning: failed to create config dir: %v", err)
		return
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		log.Printf("Warning: failed to marshal config: %v", err)
		return
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		log.Printf("Warning: failed to save config: %v", err)
	}
}

func saveLocalePreference(locale string) {
	cfg := loadConfigFile()
	if cfg == nil {
		cfg = make(map[string]interface{})
	}
	cfg["locale"] = locale
	saveConfigFile(cfg)
}

// LoadSavedLocale reads persisted locale preference from config file.
func LoadSavedLocale() string {
	cfg := loadConfigFile()
	if cfg == nil {
		return ""
	}
	loc, _ := cfg["locale"].(string)
	return loc
}
