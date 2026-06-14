package opencode

import "strings"

func codexContextFallbackFor(modelID string) int {
	if w, ok := codexContextFallback[modelID]; ok {
		return w
	}
	return 272_000
}

// ResolveContextWindow returns the context limit for a provider/model pair.
func ResolveContextWindow(provider, modelID string) int {
	return defaultRegistry.resolveContextWindow(provider, modelID)
}

func (r *Registry) resolveContextWindow(provider, modelID string) int {
	if provider == "" {
		provider = ProviderOpenCode
	}
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return 0
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	switch provider {
	case ProviderCodex:
		for _, m := range r.codexModels {
			if m.ID == modelID && m.ContextWindow > 0 {
				return m.ContextWindow
			}
		}
		if m, ok := codexKnownModels[modelID]; ok && m.ContextWindow > 0 {
			return m.ContextWindow
		}
		return codexContextFallbackFor(modelID)
	default:
		for _, m := range r.models {
			if m.ID == modelID && m.ContextWindow > 0 {
				return m.ContextWindow
			}
		}
		if m, ok := catalogModel(modelID); ok && m.ContextWindow > 0 {
			return m.ContextWindow
		}
		if m, ok := knownModels[modelID]; ok && m.ContextWindow > 0 {
			return m.ContextWindow
		}
	}
	return 0
}
