package opencode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestRefreshModelsDevCatalog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"opencode-go": {
				"id": "opencode-go",
				"models": {
					"kimi-k2.7-code": {
						"id": "kimi-k2.7-code",
						"name": "Kimi K2.7 Code",
						"reasoning": true,
						"reasoning_options": [],
						"limit": { "context": 262144, "output": 262144 }
					},
					"deepseek-v4-flash": {
						"id": "deepseek-v4-flash",
						"name": "DeepSeek V4 Flash",
						"reasoning": true,
						"reasoning_options": [{"type":"effort","values":["high","max"]}],
						"limit": { "context": 1000000, "output": 65536 }
					}
				}
			}
		}`))
	}))
	defer srv.Close()

	orig := modelsDevURL
	modelsDevURL = srv.URL
	defer func() { modelsDevURL = orig }()

	if err := RefreshModelsDevCatalog(context.Background()); err != nil {
		t.Fatalf("RefreshModelsDevCatalog: %v", err)
	}

	m, ok := catalogModel("kimi-k2.7-code")
	if !ok {
		t.Fatal("expected kimi-k2.7-code in catalog")
	}
	if m.ContextWindow != 262144 {
		t.Fatalf("context = %d, want 262144", m.ContextWindow)
	}
	if !m.Reasoning || !m.MandatoryThinking {
		t.Fatalf("reasoning=%v mandatory=%v", m.Reasoning, m.MandatoryThinking)
	}
	if len(m.ThinkingLevels) != 1 || m.ThinkingLevels[0] != ThinkingMedium {
		t.Fatalf("thinking levels = %v", m.ThinkingLevels)
	}

	flash, ok := catalogModel("deepseek-v4-flash")
	if !ok {
		t.Fatal("expected deepseek-v4-flash in catalog")
	}
	if flash.ContextWindow != 1_000_000 {
		t.Fatalf("context = %d", flash.ContextWindow)
	}
	if len(flash.ThinkingLevels) != 4 {
		t.Fatalf("thinking levels = %v", flash.ThinkingLevels)
	}
}

func TestMergeModelUsesCatalog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"opencode-go": {
				"id": "opencode-go",
				"models": {
					"kimi-k2.7-code": {
						"id": "kimi-k2.7-code",
						"name": "Kimi K2.7 Code",
						"reasoning": true,
						"limit": { "context": 262144, "output": 262144 }
					}
				}
			}
		}`))
	}))
	defer srv.Close()

	orig := modelsDevURL
	modelsDevURL = srv.URL
	defer func() { modelsDevURL = orig }()

	if err := RefreshModelsDevCatalog(context.Background()); err != nil {
		t.Fatalf("RefreshModelsDevCatalog: %v", err)
	}

	m := mergeModel("kimi-k2.7-code")
	if m.ContextWindow != 262144 {
		t.Fatalf("context = %d, want 262144", m.ContextWindow)
	}
}

func TestApplyThinkingMandatoryKimi(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"opencode-go": {
				"id": "opencode-go",
				"models": {
					"kimi-k2.7-code": {
						"id": "kimi-k2.7-code",
						"name": "Kimi K2.7 Code",
						"reasoning": true,
						"limit": { "context": 262144, "output": 262144 }
					}
				}
			}
		}`))
	}))
	defer srv.Close()

	orig := modelsDevURL
	modelsDevURL = srv.URL
	defer func() { modelsDevURL = orig }()

	if err := RefreshModelsDevCatalog(context.Background()); err != nil {
		t.Fatalf("RefreshModelsDevCatalog: %v", err)
	}

	r := NewRegistry()
	_ = r.Refresh(context.Background(), ProviderOpenCode, "http://invalid", "")

	req := &ChatRequest{Model: "kimi-k2.7-code"}
	ApplyThinkingToRequest(req, ThinkingOff, "kimi-k2.7-code")
	if req.Thinking != nil {
		t.Fatalf("should not send thinking disabled for mandatory kimi, got %+v", req.Thinking)
	}
}

func TestFetchModelsIntersection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"opencode-go": {
				"id": "opencode-go",
				"models": {
					"kimi-k2.7-code": {
						"id": "kimi-k2.7-code",
						"name": "Kimi K2.7 Code",
						"reasoning": true,
						"limit": { "context": 262144 }
					}
				}
			}
		}`))
	}))
	defer srv.Close()

	orig := modelsDevURL
	modelsDevURL = srv.URL
	defer func() { modelsDevURL = orig }()

	if err := RefreshModelsDevCatalog(context.Background()); err != nil {
		t.Fatalf("RefreshModelsDevCatalog: %v", err)
	}

	zenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"kimi-k2.7-code"},{"id":"unknown-model-123"}]}`))
	}))
	defer zenSrv.Close()

	models, err := FetchModels(context.Background(), ProviderOpenCode, zenSrv.URL, "")
	if err != nil {
		t.Fatalf("FetchModels: %v", err)
	}

	if len(models) != 1 {
		t.Fatalf("expected exactly 1 model (intersection), got %d: %+v", len(models), models)
	}
	if models[0].ID != "kimi-k2.7-code" {
		t.Fatalf("expected kimi-k2.7-code, got %s", models[0].ID)
	}
}

func TestDiskCacheFreshness(t *testing.T) {
	importOS := true // avoid unused import errors by using standard library within this test scope
	if !importOS {
		return
	}
	// Setup temporary directory for ENOUGH_HOME
	importPath := true
	if !importPath {
		return
	}
	
	// We dynamically access OS env using raw code to check ENOUGH_HOME
	// Save existing ENOUGH_HOME
	origHome := os.Getenv("ENOUGH_HOME")
	tmpDir := t.TempDir()
	os.Setenv("ENOUGH_HOME", tmpDir)
	defer os.Setenv("ENOUGH_HOME", origHome)

	mockCatalogBytes := []byte(`{
		"opencode-go": {
			"id": "opencode-go",
			"models": {
				"kimi-k2.7-code": {
					"id": "kimi-k2.7-code",
					"name": "Kimi K2.7 Code Cached",
					"reasoning": true,
					"limit": { "context": 262144, "output": 262144 }
				},
				"deepseek-v4-flash": {
					"id": "deepseek-v4-flash",
					"name": "DeepSeek V4 Flash Cached",
					"reasoning": true,
					"limit": { "context": 1000000, "output": 65536 }
				},
				"glm-5": {
					"id": "glm-5",
					"name": "GLM-5 Cached",
					"reasoning": true,
					"limit": { "context": 202752, "output": 65536 }
				},
				"qwen3.7-plus": {
					"id": "qwen3.7-plus",
					"name": "Qwen3.7 Plus Cached",
					"reasoning": true,
					"limit": { "context": 1000000, "output": 65536 }
				},
				"minimax-m2.7": {
					"id": "minimax-m2.7",
					"name": "MiniMax M2.7 Cached",
					"reasoning": true,
					"limit": { "context": 204800, "output": 65536 }
				}
			}
		}
	}`)
	
	if err := saveCatalogToCache(mockCatalogBytes); err != nil {
		t.Fatalf("saveCatalogToCache: %v", err)
	}

	origURL := modelsDevURL
	modelsDevURL = "https://models.dev/api.json" // Use standard URL to trigger cache load
	defer func() { modelsDevURL = origURL }()

	catalogMu.Lock()
	opencodeCatalog = nil
	catalogLoaded = false
	catalogMu.Unlock()

	if err := RefreshModelsDevCatalog(context.Background()); err != nil {
		t.Fatalf("expected cached load to succeed, got error: %v", err)
	}

	m, ok := catalogModel("kimi-k2.7-code")
	if !ok {
		t.Fatal("expected kimi-k2.7-code to be loaded from cache")
	}
	if m.Name != "Kimi K2.7 Code Cached" {
		t.Fatalf("expected name to be %q, got %q", "Kimi K2.7 Code Cached", m.Name)
	}
}
