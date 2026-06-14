package opencode

const (
	ProviderOpenCode = "opencode-go"
	ProviderCodex    = "openai-codex"
)

// ProviderInfo describes a model provider shown in the /model picker.
type ProviderInfo struct {
	ID   string
	Name string
}

// ModelProviders returns the providers available in the model picker.
func ModelProviders() []ProviderInfo {
	return []ProviderInfo{
		{ID: ProviderOpenCode, Name: "OpenCode Go"},
		{ID: ProviderCodex, Name: "OpenAI Codex"},
	}
}

// codexModelOrder mirrors Hermes' curated Codex OAuth catalog (chatgpt.com backend).
var codexModelOrder = []string{
	"gpt-5.5",
	"gpt-5.4-mini",
	"gpt-5.4",
	"gpt-5.3-codex",
	"gpt-5.3-codex-spark",
	"gpt-5-codex",
}

var codexKnownModels = map[string]ModelInfo{
	"gpt-5.5": {
		ID: "gpt-5.5", Name: "GPT-5.5",
		ContextWindow: 272_000, Reasoning: true,
	},
	"gpt-5.4-mini": {
		ID: "gpt-5.4-mini", Name: "GPT-5.4 Mini",
		ContextWindow: 272_000, Reasoning: true,
	},
	"gpt-5.4": {
		ID: "gpt-5.4", Name: "GPT-5.4",
		ContextWindow: 272_000, Reasoning: true,
	},
	"gpt-5.3-codex": {
		ID: "gpt-5.3-codex", Name: "GPT-5.3 Codex",
		ContextWindow: 272_000, Reasoning: true,
	},
	"gpt-5.3-codex-spark": {
		ID: "gpt-5.3-codex-spark", Name: "GPT-5.3 Codex Spark",
		ContextWindow: 128_000, Reasoning: true,
	},
	"gpt-5-codex": {
		ID: "gpt-5-codex", Name: "GPT-5 Codex",
		ContextWindow: 272_000, Reasoning: true,
	},
}

// CodexModels returns the static Codex OAuth model catalog.
func CodexModels() []ModelInfo {
	out := make([]ModelInfo, 0, len(codexModelOrder))
	for _, id := range codexModelOrder {
		var m ModelInfo
		if known, ok := codexKnownModels[id]; ok {
			m = known
		} else {
			m = ModelInfo{ID: id, Name: id, Reasoning: true, ContextWindow: 272_000}
		}
		m.ThinkingLevels = append([]ThinkingLevel(nil), defaultReasoningLevels...)
		out = append(out, normalizeModel(m))
	}
	return out
}

// ModelsForProvider returns models for a provider, using the registry for OpenCode.
func ModelsForProvider(provider string, registry *Registry) []ModelInfo {
	switch provider {
	case ProviderCodex:
		if registry != nil {
			return registry.CodexModelsList()
		}
		return CodexModels()
	default:
		models := registry.Models()
		if len(models) == 0 {
			models = FallbackModels()
		}
		out := make([]ModelInfo, len(models))
		copy(out, models)
		sortModels(out)
		return out
	}
}

// LookupCatalogModel resolves model metadata from OpenCode or Codex catalogs.
func LookupCatalogModel(id string) (ModelInfo, bool) {
	if m, ok := LookupModel(id); ok {
		return m, true
	}
	if m, ok := defaultRegistry.LookupCodex(id); ok {
		return m, true
	}
	return ModelInfo{}, false
}

// ProviderIndex returns the index of provider id in ModelProviders, or 0.
func ProviderIndex(provider string) int {
	for i, p := range ModelProviders() {
		if p.ID == provider {
			return i
		}
	}
	return 0
}
