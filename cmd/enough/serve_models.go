package main

import (
	"context"
	"time"

	"github.com/enough/enough/backend/auth"
	"github.com/enough/enough/backend/config"
	"github.com/enough/enough/backend/opencode"
	"github.com/enough/enough/backend/secrets"
)

type wsProviderDTO struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Connected bool   `json:"connected"`
}

type wsModelDTO struct {
	ID                  string   `json:"id"`
	Name                string   `json:"name"`
	Provider            string   `json:"provider"`
	ContextWindow       int      `json:"contextWindow"`
	ContextLabel        string   `json:"contextLabel"`
	Reasoning           bool     `json:"reasoning"`
	ThinkingLevels      []string `json:"thinkingLevels"`
	ThinkingLevelLabels []string `json:"thinkingLevelLabels"`
}

type wsModelStateDTO struct {
	Provider      string `json:"provider"`
	ModelID       string `json:"modelId"`
	ModelName     string `json:"modelName"`
	ThinkingLevel string `json:"thinkingLevel"`
	ContextLabel  string `json:"contextLabel"`
	Reasoning     bool   `json:"reasoning"`
}

type wsModelsCatalog struct {
	Type      string          `json:"type"`
	Providers []wsProviderDTO `json:"providers"`
	Models    []wsModelDTO    `json:"models"`
	State     wsModelStateDTO `json:"state"`
}

func providerConnected(id string) bool {
	switch id {
	case config.ProviderCodex:
		return auth.HasCodexAuth()
	case config.ProviderOpenCode, config.ProviderOpenCodeZen:
		return secrets.HasAPIKey()
	default:
		return secrets.HasAPIKey()
	}
}

func refreshDesktopModelRegistry(ctx context.Context, registry *opencode.Registry) {
	if apiKey, _ := secrets.GetAPIKey(); apiKey != "" {
		_ = registry.Refresh(ctx, opencode.ProviderOpenCode, config.DefaultEndpoint, apiKey)
		_ = registry.Refresh(ctx, opencode.ProviderOpenCodeZen, config.DefaultZenEndpoint, apiKey)
	}
	if auth.HasCodexAuth() {
		if creds, err := auth.ResolveCodexCredentials(ctx); err == nil {
			_ = registry.RefreshCodex(ctx, creds.AccessToken)
		}
	}
}

func mapModelDTO(provider string, m opencode.ModelInfo) wsModelDTO {
	levels := opencode.SupportedThinkingLevels(m.ID)
	outLevels := make([]string, 0, len(levels))
	outLabels := make([]string, 0, len(levels))
	for _, l := range levels {
		outLevels = append(outLevels, string(l))
		outLabels = append(outLabels, opencode.FormatThinkingLevelForModel(m.ID, l))
	}
	return wsModelDTO{
		ID:                  m.ID,
		Name:                m.Name,
		Provider:            provider,
		ContextWindow:       m.ContextWindow,
		ContextLabel:        opencode.FormatContextWindow(m.ContextWindow),
		Reasoning:           m.Reasoning,
		ThinkingLevels:      outLevels,
		ThinkingLevelLabels: outLabels,
	}
}

func resolveModelState(registry *opencode.Registry) wsModelStateDTO {
	provider, _, modelID, err := config.ConnectionSettings()
	if err != nil {
		provider = config.ProviderOpenCode
		modelID = config.DefaultModel
	}
	if provider == "" {
		provider = config.ProviderOpenCode
	}

	cfg, err := config.Load()
	thinking := ""
	if err == nil {
		thinking = cfg.ThinkingLevel
	}
	if thinking == "" && opencode.SupportsThinking(modelID) {
		thinking = string(opencode.ThinkingMedium)
	}

	name := modelID
	contextLabel := ""
	reasoning := false
	for _, m := range opencode.ModelsForProvider(provider, registry) {
		if m.ID == modelID {
			name = m.Name
			contextLabel = opencode.FormatContextWindow(m.ContextWindow)
			reasoning = m.Reasoning
			break
		}
	}
	if contextLabel == "" {
		if m, ok := opencode.LookupCatalogModel(modelID); ok {
			name = m.Name
			contextLabel = opencode.FormatContextWindow(m.ContextWindow)
			reasoning = m.Reasoning
		}
	}

	return wsModelStateDTO{
		Provider:      provider,
		ModelID:       modelID,
		ModelName:     name,
		ThinkingLevel: thinking,
		ContextLabel:  contextLabel,
		Reasoning:     reasoning,
	}
}

func buildModelsCatalog(registry *opencode.Registry) wsModelsCatalog {
	providers := make([]wsProviderDTO, 0, len(opencode.ModelProviders()))
	models := make([]wsModelDTO, 0, 64)
	for _, p := range opencode.ModelProviders() {
		providers = append(providers, wsProviderDTO{
			ID:        p.ID,
			Name:      p.Name,
			Connected: providerConnected(p.ID),
		})
		for _, m := range opencode.ModelsForProvider(p.ID, registry) {
			models = append(models, mapModelDTO(p.ID, m))
		}
	}
	return wsModelsCatalog{
		Type:      "models.catalog",
		Providers: providers,
		Models:    models,
		State:     resolveModelState(registry),
	}
}

func sendModelsCatalog(sendCh chan interface{}, registry *opencode.Registry) {
	sendCh <- buildModelsCatalog(registry)
}

func handleListModels(sendCh chan interface{}, registry *opencode.Registry) {
	sendModelsCatalog(sendCh, registry)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		refreshDesktopModelRegistry(ctx, registry)
		sendModelsCatalog(sendCh, registry)
	}()
}
