package opencode

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchModelsMergesMetadata(t *testing.T) {
	catalogSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"opencode-go": {
				"id": "opencode-go",
				"models": {
					"deepseek-v4-flash": {
						"id": "deepseek-v4-flash",
						"name": "DeepSeek V4 Flash",
						"reasoning": true,
						"reasoning_options": [{"type":"effort","values":["high","max"]}],
						"limit": { "context": 1000000, "output": 65536 }
					},
					"hy3-preview": {
						"id": "hy3-preview",
						"name": "HY3 Preview",
						"reasoning": true,
						"limit": { "context": 256000, "output": 65536 }
					}
				}
			}
		}`))
	}))
	defer catalogSrv.Close()

	origCatalog := modelsDevURL
	modelsDevURL = catalogSrv.URL
	defer func() { modelsDevURL = origCatalog }()
	_ = RefreshModelsDevCatalog(context.Background())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"deepseek-v4-flash"},{"id":"hy3-preview"}]}`))
	}))
	defer srv.Close()

	models, err := FetchModels(context.Background(), srv.URL, "")
	if err != nil {
		t.Fatalf("FetchModels: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("len(models) = %d, want 2", len(models))
	}

	var flash ModelInfo
	for _, m := range models {
		if m.ID == "deepseek-v4-flash" {
			flash = m
			break
		}
	}
	if flash.ContextWindow != 1_000_000 {
		t.Fatalf("context window = %d, want 1000000", flash.ContextWindow)
	}
	if flash.Name != "DeepSeek V4 Flash" {
		t.Fatalf("name = %q", flash.Name)
	}
	if len(flash.ThinkingLevels) != 4 {
		t.Fatalf("thinking levels = %v", flash.ThinkingLevels)
	}

	var hy3 ModelInfo
	for _, m := range models {
		if m.ID == "hy3-preview" {
			hy3 = m
			break
		}
	}
	if hy3.ContextWindow != 256000 {
		t.Fatalf("hy3 context = %d, want 256000", hy3.ContextWindow)
	}
}

func TestRegistryLookupFallback(t *testing.T) {
	r := NewRegistry()
	m, ok := r.Lookup("deepseek-v4-pro")
	if !ok {
		t.Fatal("expected lookup ok")
	}
	if m.ContextWindow != 1_000_000 {
		t.Fatalf("context = %d", m.ContextWindow)
	}
}

func TestFormatContextWindow(t *testing.T) {
	if got := FormatContextWindow(1_000_000); got != "1M" {
		t.Fatalf("got %q", got)
	}
	if got := FormatContextWindow(262144); got != "262.1k" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveContextWindowCodex(t *testing.T) {
	if got := ResolveContextWindow(ProviderCodex, "gpt-5.3-codex"); got != 272_000 {
		t.Fatalf("codex context = %d, want 272000", got)
	}
	if got := ResolveContextWindow(ProviderCodex, "gpt-5.3-codex-spark"); got != 128_000 {
		t.Fatalf("spark context = %d, want 128000", got)
	}
	if got := ResolveContextWindow(ProviderOpenCode, "gpt-5-codex"); got != 0 {
		t.Fatalf("codex model on opencode provider = %d, want 0 fallback", got)
	}
}

func TestRegistryRefreshCodex(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"models":[
			{"slug":"gpt-5-codex","title":"GPT-5 Codex","context_window":272000,"visibility":"visible","priority":1},
			{"slug":"gpt-5.3-codex-spark","title":"Spark","context_window":128000,"visibility":"visible","priority":2}
		]}`))
	}))
	defer srv.Close()

	orig := codexModelsURL
	codexModelsURL = srv.URL
	defer func() { codexModelsURL = orig }()

	r := NewRegistry()
	if err := r.RefreshCodex(context.Background(), "test-token"); err != nil {
		t.Fatalf("RefreshCodex: %v", err)
	}
	if got := r.resolveContextWindow(ProviderCodex, "gpt-5-codex"); got != 272_000 {
		t.Fatalf("live codex context = %d", got)
	}
}

func TestFormatThinkingBadge(t *testing.T) {
	// minimax-m3
	m3 := ModelInfo{ID: "minimax-m3", Reasoning: true}
	if got := FormatThinkingBadge(m3, ThinkingOff); got != "none" {
		t.Fatalf("expected none, got %q", got)
	}
	if got := FormatThinkingBadge(m3, ""); got != "none" {
		t.Fatalf("expected none, got %q", got)
	}
	if got := FormatThinkingBadge(m3, ThinkingMedium); got != "thinking" {
		t.Fatalf("expected thinking, got %q", got)
	}

	// deepseek-v4-flash
	ds := ModelInfo{ID: "deepseek-v4-flash", Reasoning: true}
	if got := FormatThinkingBadge(ds, ThinkingLow); got != "low" {
		t.Fatalf("expected low, got %q", got)
	}

	// kimi (no variants)
	kimi := ModelInfo{ID: "kimi-k2.7-code", Reasoning: true}
	if got := FormatThinkingBadge(kimi, ThinkingMedium); got != "reasoning" {
		t.Fatalf("expected reasoning, got %q", got)
	}
}

