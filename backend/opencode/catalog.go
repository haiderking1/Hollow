package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/enough/enough/backend/enoughhome"
)

var modelsDevURL = "https://models.dev/api.json"

// minOpencodeGoCatalogModels guards against corrupted/partial cache files (e.g. test mocks).
const minOpencodeGoCatalogModels = 5

const modelsDevProviderGo = "opencode-go"
const modelsDevProviderZen = "opencode"

type modelsDevCatalog struct {
	Providers map[string]modelsDevProvider `json:"-"`
}

type modelsDevProvider struct {
	ID     string                     `json:"id"`
	Models map[string]modelsDevModel `json:"models"`
}

type modelsDevReasoningOption struct {
	Type   string   `json:"type"`
	Values []string `json:"values"`
}

type modelsDevModel struct {
	ID               string                     `json:"id"`
	Name             string                     `json:"name"`
	Family           string                     `json:"family"`
	Reasoning        bool                       `json:"reasoning"`
	ReasoningOptions []modelsDevReasoningOption `json:"reasoning_options"`
	Limit            struct {
		Context int `json:"context"`
		Output  int `json:"output"`
	} `json:"limit"`
	Interleaved      json.RawMessage            `json:"interleaved"`
}

var (
	catalogMu             sync.RWMutex
	opencodeCatalog       map[string]ModelInfo // OpenCode Go (models.dev opencode-go)
	opencodeZenCatalog    map[string]ModelInfo // OpenCode Zen (models.dev opencode)
	catalogLoaded         bool
	backgroundRefreshOnce sync.Once
)

func cacheFilePath() string {
	return filepath.Join(enoughhome.HomeDir(), "cache", "models.json")
}

func loadCatalogFromCache() (goCatalog, zenCatalog map[string]ModelInfo, modTime time.Time, err error) {
	path := cacheFilePath()
	fi, err := os.Stat(path)
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, time.Time{}, err
	}
	var all map[string]modelsDevProvider
	if err := json.Unmarshal(data, &all); err != nil {
		return nil, nil, time.Time{}, err
	}
	goCatalog = catalogFromModelsDevProvider(all[modelsDevProviderGo])
	zenCatalog = catalogFromModelsDevProvider(all[modelsDevProviderZen])
	if len(goCatalog) == 0 {
		return nil, nil, time.Time{}, fmt.Errorf("missing go provider in cached catalog")
	}
	return goCatalog, zenCatalog, fi.ModTime(), nil
}

func catalogFromModelsDevProvider(provider modelsDevProvider) map[string]ModelInfo {
	if len(provider.Models) == 0 {
		return nil
	}
	next := make(map[string]ModelInfo, len(provider.Models))
	for id, m := range provider.Models {
		next[id] = modelInfoFromModelsDev(m)
	}
	return next
}

func saveCatalogToCache(data []byte) error {
	path := cacheFilePath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func startBackgroundRefreshLoop() {
	go func() {
		for {
			time.Sleep(60 * time.Minute)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			_ = RefreshModelsDevCatalog(ctx)
			cancel()
		}
	}()
}

func RefreshModelsDevCatalog(ctx context.Context) error {
	backgroundRefreshOnce.Do(startBackgroundRefreshLoop)

	// 1. Try to load from disk cache first (only if using default URL)
	var cachedGo, cachedZen map[string]ModelInfo
	var modTime time.Time
	var err error
	if modelsDevURL == "https://models.dev/api.json" {
		cachedGo, cachedZen, modTime, err = loadCatalogFromCache()
		if err == nil && len(cachedGo) >= minOpencodeGoCatalogModels {
			// Cache is present. Check freshness (60 minutes).
			if time.Since(modTime) < 60*time.Minute {
				catalogMu.Lock()
				opencodeCatalog = cachedGo
				opencodeZenCatalog = cachedZen
				catalogLoaded = true
				catalogMu.Unlock()
				return nil
			}
		}
	}

	// 2. Fetch from network
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsDevURL, nil)
	if err != nil {
		if cachedGo != nil {
			catalogMu.Lock()
			opencodeCatalog = cachedGo
			opencodeZenCatalog = cachedZen
			catalogLoaded = true
			catalogMu.Unlock()
			return nil
		}
		return err
	}

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		if cachedGo != nil {
			catalogMu.Lock()
			opencodeCatalog = cachedGo
			opencodeZenCatalog = cachedZen
			catalogLoaded = true
			catalogMu.Unlock()
			return nil
		}
		return err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		if cachedGo != nil {
			catalogMu.Lock()
			opencodeCatalog = cachedGo
			opencodeZenCatalog = cachedZen
			catalogLoaded = true
			catalogMu.Unlock()
			return nil
		}
		return err
	}
	if resp.StatusCode >= 400 {
		if cachedGo != nil {
			catalogMu.Lock()
			opencodeCatalog = cachedGo
			opencodeZenCatalog = cachedZen
			catalogLoaded = true
			catalogMu.Unlock()
			return nil
		}
		return fmt.Errorf("models.dev %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var all map[string]modelsDevProvider
	if err := json.Unmarshal(raw, &all); err != nil {
		if cachedGo != nil {
			catalogMu.Lock()
			opencodeCatalog = cachedGo
			opencodeZenCatalog = cachedZen
			catalogLoaded = true
			catalogMu.Unlock()
			return nil
		}
		return fmt.Errorf("decode models.dev: %w", err)
	}

	goProvider, ok := all[modelsDevProviderGo]
	if !ok {
		if cachedGo != nil {
			catalogMu.Lock()
			opencodeCatalog = cachedGo
			opencodeZenCatalog = cachedZen
			catalogLoaded = true
			catalogMu.Unlock()
			return nil
		}
		return fmt.Errorf("models.dev: missing %q provider", modelsDevProviderGo)
	}

	catalogMu.Lock()
	opencodeCatalog = catalogFromModelsDevProvider(goProvider)
	opencodeZenCatalog = catalogFromModelsDevProvider(all[modelsDevProviderZen])
	catalogLoaded = true
	catalogMu.Unlock()

	if modelsDevURL == "https://models.dev/api.json" {
		_ = saveCatalogToCache(raw)
	}
	return nil
}

func catalogModel(id string) (ModelInfo, bool) {
	return catalogModelForProvider(ProviderOpenCode, id)
}

func catalogModelForProvider(provider, id string) (ModelInfo, bool) {
	catalogMu.RLock()
	defer catalogMu.RUnlock()
	cat := opencodeCatalog
	if provider == ProviderOpenCodeZen {
		cat = opencodeZenCatalog
	}
	if m, ok := cat[id]; ok {
		return m, true
	}
	return ModelInfo{}, false
}

func catalogLoadedOnce() bool {
	catalogMu.RLock()
	defer catalogMu.RUnlock()
	return catalogLoaded
}

func parseReasoningField(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		if b {
			return "reasoning_content"
		}
		return ""
	}
	var obj struct {
		Field string `json:"field"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		return obj.Field
	}
	return ""
}

func modelInfoFromModelsDev(m modelsDevModel) ModelInfo {
	info := ModelInfo{
		ID:             m.ID,
		Name:           m.Name,
		ContextWindow:  m.Limit.Context,
		Reasoning:      m.Reasoning,
		ReasoningField: parseReasoningField(m.Interleaved),
	}
	if info.Name == "" {
		info.Name = titleCaseModelID(m.ID)
	}
	return normalizeModel(info)
}

func opencodeMandatoryThinkingID(id string) bool {
	id = strings.ToLower(id)
	if strings.Contains(id, "deepseek-chat") ||
		strings.Contains(id, "deepseek-reasoner") ||
		strings.Contains(id, "deepseek-r1") ||
		strings.Contains(id, "deepseek-v3") {
		return true
	}
	for _, part := range []string{"minimax", "glm", "kimi", "k2p", "qwen", "big-pickle"} {
		if strings.Contains(id, part) {
			return true
		}
	}
	return false
}



func titleCaseModelID(id string) string {
	parts := strings.Split(id, "-")
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, " ")
}
