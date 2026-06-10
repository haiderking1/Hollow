package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/enough/enough/backend/secrets"
)

const (
	DefaultEndpoint = "https://opencode.ai/zen/go/v1"
	DefaultModel    = "deepseek-v4-flash"
)

// Config holds non-secret settings persisted to disk.
type Config struct {
	Endpoint      string `json:"endpoint"`
	Model         string `json:"model"`
	ThinkingLevel string `json:"thinking_level,omitempty"`
	HideThinking  bool   `json:"hide_thinking,omitempty"`

	// legacy field — migrated to secrets store on load, never written back
	apiKeyLegacy string `json:"-"`
}

// Runtime bundles config with the in-memory API key (never saved to config.json).
type Runtime struct {
	Endpoint      string
	Model         string
	APIKey        string
	ThinkingLevel string
	HideThinking  bool
}

func Default() Config {
	return Config{
		Endpoint: DefaultEndpoint,
		Model:    DefaultModel,
	}
}

func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "enough"), nil
}

func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

type fileConfig struct {
	Endpoint      string `json:"endpoint"`
	Model         string `json:"model"`
	ThinkingLevel string `json:"thinking_level,omitempty"`
	HideThinking  bool   `json:"hide_thinking,omitempty"`
	APIKey        string `json:"api_key,omitempty"`
}

func Load() (Config, error) {
	cfg := Default()

	path, err := Path()
	if err != nil {
		return cfg, err
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}

	var raw fileConfig
	if err := json.Unmarshal(data, &raw); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}

	cfg.Endpoint = raw.Endpoint
	cfg.Model = raw.Model
	cfg.ThinkingLevel = raw.ThinkingLevel
	cfg.HideThinking = raw.HideThinking
	cfg.apiKeyLegacy = raw.APIKey

	if cfg.Endpoint == "" {
		cfg.Endpoint = DefaultEndpoint
	}
	if cfg.Model == "" {
		cfg.Model = DefaultModel
	}

	// one-time migration: move api key from config.json into secret store
	if raw.APIKey != "" && !secrets.HasAPIKey() {
		if err := secrets.SetAPIKey(raw.APIKey); err == nil {
			_ = Save(cfg)
		}
	}

	return cfg, nil
}

func Save(cfg Config) error {
	if cfg.Endpoint == "" {
		cfg.Endpoint = DefaultEndpoint
	}
	if cfg.Model == "" {
		cfg.Model = DefaultModel
	}

	dir, err := Dir()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	path, err := Path()
	if err != nil {
		return err
	}

	raw := fileConfig{
		Endpoint:      cfg.Endpoint,
		Model:         cfg.Model,
		ThinkingLevel: cfg.ThinkingLevel,
		HideThinking:  cfg.HideThinking,
	}

	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o600)
}

func LoadRuntime() (Runtime, error) {
	cfg, err := Load()
	if err != nil {
		return Runtime{}, err
	}

	key, err := secrets.GetAPIKey()
	if err != nil {
		return Runtime{}, err
	}

	return Runtime{
		Endpoint:      cfg.Endpoint,
		Model:         cfg.Model,
		APIKey:        key,
		ThinkingLevel: cfg.ThinkingLevel,
		HideThinking:  cfg.HideThinking,
	}, nil
}

func Connected() bool {
	return secrets.HasAPIKey()
}
