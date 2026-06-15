package opencode

import "testing"

func TestCodexModels(t *testing.T) {
	models := CodexModels()
	if len(models) == 0 {
		t.Fatal("expected codex models")
	}
	if models[0].ID != "gpt-5.5" {
		t.Fatalf("first model = %q", models[0].ID)
	}
}

func TestModelsForProvider(t *testing.T) {
	codex := ModelsForProvider(ProviderCodex, NewRegistry())
	if len(codex) == 0 {
		t.Fatal("expected codex models")
	}
	open := ModelsForProvider(ProviderOpenCode, NewRegistry())
	if len(open) == 0 {
		t.Fatal("expected opencode fallback models")
	}
}

func TestProviderIndex(t *testing.T) {
	if ProviderIndex(ProviderCodex) != 2 {
		t.Fatalf("codex index = %d", ProviderIndex(ProviderCodex))
	}
	if ProviderIndex(ProviderOpenCodeZen) != 1 {
		t.Fatalf("zen index = %d", ProviderIndex(ProviderOpenCodeZen))
	}
	if ProviderIndex("unknown") != 0 {
		t.Fatal("unknown should default to 0")
	}
}
