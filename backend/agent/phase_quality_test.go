package agent

import (
	"strings"
	"testing"

	"github.com/enough/enough/backend/config"
	"github.com/enough/enough/backend/core"
	"github.com/enough/enough/backend/opencode"
)

func TestGoalLockNoticeInjected(t *testing.T) {
	a := &Agent{
		cfg: config.Runtime{
			Model:    "test-model",
			Evidence: config.DefaultEvidence(),
		},
		workDir: t.TempDir(),
	}

	srv := scriptedServer(t, func(req opencode.ChatRequest) (string, []toolCallJSON) {
		return "ok", nil
	})
	defer srv.Close()

	cfg := a.cfg
	cfg.Endpoint = srv.URL
	cfg.APIKey = "k"
	if err := a.Prompt(t.Context(), cfg, "fix the login bug", func(core.Event) {}); err != nil {
		t.Fatalf("Prompt: %v", err)
	}

	found := false
	for _, m := range a.messages {
		if m.Role != "user" {
			continue
		}
		text := opencode.ContentString(m)
		if strings.HasPrefix(text, core.RuntimeNoticePrefix) && strings.Contains(text, "GOAL LOCK") {
			found = true
		}
	}
	if !found {
		t.Fatal("goal lock notice not injected")
	}
}

func TestVerifySuccessResetsFailureCounter(t *testing.T) {
	a := &Agent{cfg: evidenceRuntime(false)}
	a.noteVerifyFailure()
	a.noteVerifySuccess()
	a.noteVerifyFailure()
	if a.parallelForksAttempted {
		t.Fatal("single failure after reset should not fork")
	}
}
