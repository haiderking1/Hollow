package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// ModelInfo describes an OpenCode Go model for UI and compaction.
type ModelInfo struct {
	ID                string
	Name              string
	ContextWindow     int
	Reasoning         bool
	MandatoryThinking bool
	ThinkingLevels    []ThinkingLevel
	ReasoningField    string
}

type modelsListResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// knownModels is static metadata for OpenCode Go models (Flame-compatible).
var knownModels = map[string]ModelInfo{
	"deepseek-v4-flash": {
		ID: "deepseek-v4-flash", Name: "DeepSeek V4 Flash",
		ContextWindow: 1_000_000, Reasoning: true, ThinkingLevels: deepseekV4FlashLevels,
	},
	"deepseek-v4-pro": {
		ID: "deepseek-v4-pro", Name: "DeepSeek V4 Pro",
		ContextWindow: 1_000_000, Reasoning: true, ThinkingLevels: deepseekV4FlashLevels,
	},
	"glm-5": {
		ID: "glm-5", Name: "GLM-5",
		ContextWindow: 202752, Reasoning: true,
	},
	"glm-5.1": {
		ID: "glm-5.1", Name: "GLM-5.1",
		ContextWindow: 202752, Reasoning: true,
	},
	"kimi-k2.5": {
		ID: "kimi-k2.5", Name: "Kimi K2.5",
		ContextWindow: 262144, Reasoning: true,
	},
	"kimi-k2.6": {
		ID: "kimi-k2.6", Name: "Kimi K2.6",
		ContextWindow: 262144, Reasoning: true,
	},
	"mimo-v2.5": {
		ID: "mimo-v2.5", Name: "MiMo V2.5",
		ContextWindow: 1_000_000, Reasoning: true,
	},
	"mimo-v2.5-pro": {
		ID: "mimo-v2.5-pro", Name: "MiMo V2.5 Pro",
		ContextWindow: 1_048_576, Reasoning: true,
	},
	"mimo-v2-pro": {
		ID: "mimo-v2-pro", Name: "MiMo V2 Pro",
		ContextWindow: 1_000_000, Reasoning: true,
	},
	"mimo-v2-omni": {
		ID: "mimo-v2-omni", Name: "MiMo V2 Omni",
		ContextWindow: 1_000_000, Reasoning: true,
	},
	"minimax-m2.5": {
		ID: "minimax-m2.5", Name: "MiniMax M2.5",
		ContextWindow: 204800, Reasoning: true,
	},
	"minimax-m2.7": {
		ID: "minimax-m2.7", Name: "MiniMax M2.7",
		ContextWindow: 204800, Reasoning: true,
	},
	"minimax-m3": {
		ID: "minimax-m3", Name: "MiniMax M3",
		ContextWindow: 512000, Reasoning: true,
	},
	"qwen3.6-plus": {
		ID: "qwen3.6-plus", Name: "Qwen3.6 Plus",
		ContextWindow: 1_000_000, Reasoning: true,
	},
	"qwen3.5-plus": {
		ID: "qwen3.5-plus", Name: "Qwen3.5 Plus",
		ContextWindow: 1_000_000, Reasoning: true,
	},
	"qwen3.7-max": {
		ID: "qwen3.7-max", Name: "Qwen3.7 Max",
		ContextWindow: 1_000_000, Reasoning: true,
	},
	"qwen3.7-plus": {
		ID: "qwen3.7-plus", Name: "Qwen3.7 Plus",
		ContextWindow: 1_000_000, Reasoning: true,
	},
	"hy3-preview": {
		ID: "hy3-preview", Name: "HY3 Preview",
		ContextWindow: 256000, Reasoning: true,
	},
}

var defaultRegistry = NewRegistry()

// Registry caches models fetched from provider APIs at startup.
type Registry struct {
	mu          sync.RWMutex
	models      []ModelInfo
	codexModels []ModelInfo
	err         error
	codexErr    error
}

func NewRegistry() *Registry {
	return &Registry{}
}

func DefaultRegistry() *Registry {
	return defaultRegistry
}

func (r *Registry) Models() []ModelInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ModelInfo, len(r.models))
	copy(out, r.models)
	return out
}

func (r *Registry) Err() error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.err
}

func (r *Registry) Lookup(id string) (ModelInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, m := range r.models {
		if m.ID == id {
			return m, true
		}
	}
	if m, ok := catalogModel(id); ok {
		return m, true
	}
	if m, ok := knownModels[id]; ok {
		return normalizeModel(m), true
	}
	return ModelInfo{}, false
}

func (r *Registry) Refresh(ctx context.Context, endpoint, apiKey string) error {
	_ = RefreshModelsDevCatalog(ctx)
	models, err := FetchModels(ctx, endpoint, apiKey)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.models = models
	r.err = err
	return err
}

func (r *Registry) RefreshCodex(ctx context.Context, accessToken string) error {
	models, err := FetchCodexModels(ctx, accessToken)
	r.mu.Lock()
	defer r.mu.Unlock()
	if err == nil {
		r.codexModels = models
		r.codexErr = nil
		return nil
	}
	r.codexErr = err
	if len(r.codexModels) == 0 {
		r.codexModels = CodexModels()
	}
	return err
}

func (r *Registry) CodexErr() error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.codexErr
}

func (r *Registry) CodexModelsList() []ModelInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.codexModels) > 0 {
		out := make([]ModelInfo, len(r.codexModels))
		copy(out, r.codexModels)
		return out
	}
	return CodexModels()
}

func (r *Registry) LookupCodex(id string) (ModelInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, m := range r.codexModels {
		if m.ID == id {
			return m, true
		}
	}
	if m, ok := codexKnownModels[id]; ok {
		m.ThinkingLevels = append([]ThinkingLevel(nil), defaultReasoningLevels...)
		return normalizeModel(m), true
	}
	return ModelInfo{}, false
}

func LookupModel(id string) (ModelInfo, bool) {
	return defaultRegistry.Lookup(id)
}

func ModelContextWindow(id string) int {
	return ResolveContextWindow(ProviderOpenCode, id)
}

func FetchModels(ctx context.Context, endpoint, apiKey string) ([]ModelInfo, error) {
	endpoint = strings.TrimRight(endpoint, "/")
	url := endpoint + "/models"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fallbackModels(), err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fallbackModels(), err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fallbackModels(), err
	}
	if resp.StatusCode >= 400 {
		return fallbackModels(), fmt.Errorf("models %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var list modelsListResponse
	if err := json.Unmarshal(raw, &list); err != nil {
		return fallbackModels(), fmt.Errorf("decode models: %w", err)
	}

	seen := make(map[string]struct{}, len(list.Data))
	out := make([]ModelInfo, 0, len(list.Data))
	for _, entry := range list.Data {
		id := strings.TrimSpace(entry.ID)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		catalogMu.RLock()
		m, ok := opencodeCatalog[id]
		catalogMu.RUnlock()
		if ok {
			out = append(out, m)
		}
	}

	if len(out) == 0 {
		return fallbackModels(), nil
	}

	sortModels(out)
	return out, nil
}

func fallbackModels() []ModelInfo {
	catalogMu.RLock()
	defer catalogMu.RUnlock()
	if len(opencodeCatalog) > 0 {
		out := make([]ModelInfo, 0, len(opencodeCatalog))
		for _, m := range opencodeCatalog {
			out = append(out, m)
		}
		sortModels(out)
		return out
	}
	out := make([]ModelInfo, 0, len(knownModels))
	for id := range knownModels {
		out = append(out, mergeModel(id))
	}
	sortModels(out)
	return out
}

// FallbackModels returns the static catalog when the API is unavailable.
func FallbackModels() []ModelInfo {
	return fallbackModels()
}

func mergeModel(id string) ModelInfo {
	if m, ok := catalogModel(id); ok {
		return m
	}
	if m, ok := knownModels[id]; ok {
		return normalizeModel(m)
	}
	return ModelInfo{}
}

func normalizeModel(m ModelInfo) ModelInfo {
	if !m.MandatoryThinking {
		m.MandatoryThinking = opencodeMandatoryThinkingID(m.ID)
	}
	m.ThinkingLevels = SupportedThinkingLevels(m.ID)
	return m
}


func sortModels(models []ModelInfo) {
	sort.Slice(models, func(i, j int) bool {
		return strings.ToLower(models[i].Name) < strings.ToLower(models[j].Name)
	})
}

func FormatContextWindow(n int) string {
	switch {
	case n >= 1_000_000:
		if n%1_000_000 == 0 {
			return fmt.Sprintf("%dM", n/1_000_000)
		}
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1000:
		if n%1000 == 0 {
			return fmt.Sprintf("%dk", n/1000)
		}
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func FormatThinkingBadge(m ModelInfo, level ThinkingLevel) string {
	if !SupportsThinking(m.ID) {
		if m.Reasoning {
			return "reasoning"
		}
		return ""
	}
	return FormatThinkingLevelForModel(m.ID, level)
}

func SupportsImages(model string) bool {
	m := strings.ToLower(model)
	if strings.HasPrefix(m, "gpt-5") {
		return true
	}
	if strings.HasPrefix(m, "gpt-5.3-codex-spark") {
		return true
	}
	if strings.HasPrefix(m, "kimi-k2") {
		return true
	}
	if strings.HasPrefix(m, "glm-") {
		return true
	}
	if strings.HasPrefix(m, "mimo-v2-omni") {
		return true
	}
	return false
}
