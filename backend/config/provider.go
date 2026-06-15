package config

import (
	"github.com/enough/enough/backend/auth"
	"github.com/enough/enough/backend/secrets"
)

// EnableOpenCodeProvider stores an API key and switches the active provider to Go.
func EnableOpenCodeProvider(key string) error {
	return enableOpenCodeProvider(key, ProviderOpenCode, DefaultEndpoint)
}

// EnableOpenCodeZenProvider stores an API key and switches the active provider to Zen.
func EnableOpenCodeZenProvider(key string) error {
	return enableOpenCodeProvider(key, ProviderOpenCodeZen, DefaultZenEndpoint)
}

func enableOpenCodeProvider(key, provider, endpoint string) error {
	if err := auth.SaveAPIKey(key); err != nil {
		return err
	}
	cfg, err := Load()
	if err != nil {
		return err
	}
	cfg.Provider = provider
	cfg.Endpoint = endpoint
	if cfg.Model == "" {
		if provider == ProviderOpenCodeZen {
			cfg.Model = DefaultZenModel
		} else {
			cfg.Model = DefaultModel
		}
	}
	return Save(cfg)
}

// EnableCodexProvider switches runtime to OpenAI Codex OAuth.
func EnableCodexProvider() error {
	if !auth.HasCodexAuth() {
		return secrets.ErrNotConnected
	}
	cfg, err := Load()
	if err != nil {
		return err
	}
	cfg.Provider = ProviderCodex
	cfg.Endpoint = auth.CodexDefaultBaseURL()
	if cfg.Model == "" || cfg.Model == DefaultModel {
		cfg.Model = DefaultCodexModel
	}
	return Save(cfg)
}

// ConnectionSettings returns non-secret connection settings.
func ConnectionSettings() (provider, endpoint, model string, err error) {
	cfg, err := Load()
	if err != nil {
		return "", "", "", err
	}
	provider = cfg.Provider
	if provider == "" {
		provider = ProviderOpenCode
	}
	if cfg.Endpoint == "" {
		switch provider {
		case ProviderCodex:
			endpoint = auth.CodexDefaultBaseURL()
		case ProviderOpenCodeZen:
			endpoint = DefaultZenEndpoint
		default:
			endpoint = DefaultEndpoint
		}
	} else {
		endpoint = cfg.Endpoint
	}
	model = cfg.Model
	if model == "" {
		switch provider {
		case ProviderCodex:
			model = DefaultCodexModel
		case ProviderOpenCodeZen:
			model = DefaultZenModel
		default:
			model = DefaultModel
		}
	}
	return provider, endpoint, model, nil
}

// ApplyProviderModel switches provider, endpoint, and model settings.
func ApplyProviderModel(provider, model, thinkingLevel string) error {
	switch provider {
	case ProviderCodex:
		if !auth.HasCodexAuth() {
			return secrets.ErrNotConnected
		}
	case ProviderOpenCode, ProviderOpenCodeZen:
		if !secrets.HasAPIKey() {
			return secrets.ErrNotConnected
		}
	default:
		provider = ProviderOpenCode
	}

	cfg, err := Load()
	if err != nil {
		return err
	}
	cfg.Provider = provider
	cfg.Model = model
	cfg.ThinkingLevel = thinkingLevel

	switch provider {
	case ProviderCodex:
		cfg.Endpoint = auth.CodexDefaultBaseURL()
	case ProviderOpenCodeZen:
		if cfg.Endpoint == "" || cfg.Endpoint == DefaultEndpoint || cfg.Endpoint == auth.CodexDefaultBaseURL() {
			cfg.Endpoint = DefaultZenEndpoint
		}
	default:
		if cfg.Endpoint == "" || cfg.Endpoint == DefaultZenEndpoint || cfg.Endpoint == auth.CodexDefaultBaseURL() {
			cfg.Endpoint = DefaultEndpoint
		}
	}
	return Save(cfg)
}
