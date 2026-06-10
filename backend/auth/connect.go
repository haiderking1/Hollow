package auth

import (
	"fmt"
	"strings"

	"github.com/enough/enough/backend/config"
	"github.com/enough/enough/backend/secrets"
)

// SaveAPIKey stores the OpenCode API key for the current user.
func SaveAPIKey(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("api key cannot be empty")
	}
	return secrets.SetAPIKey(key)
}

// Settings returns non-secret connection settings.
func Settings() (endpoint, model string, err error) {
	cfg, err := config.Load()
	if err != nil {
		return "", "", err
	}
	return cfg.Endpoint, cfg.Model, nil
}

// Connected reports whether an API key is stored.
func Connected() bool {
	return config.Connected()
}
