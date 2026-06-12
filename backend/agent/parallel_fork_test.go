package agent

import (
	"strings"
	"testing"

	"github.com/enough/enough/backend/config"
)

func TestParallelForksDisabledSkips(t *testing.T) {
	ev := config.DefaultEvidence()
	falseVal := false
	ev.ParallelForks = &falseVal
	a := &Agent{cfg: config.Runtime{Evidence: ev}}
	a.verifyFailures = 5
	a.noteVerifyFailure()
	if a.parallelForksAttempted {
		t.Fatal("should not fork when disabled")
	}
}

func TestBuildForkPromptIncludesGoal(t *testing.T) {
	p := buildForkPrompt("fix login", "exit 1", "go test ./...", forkAngles[0])
	if !strings.Contains(p, "fix login") || !strings.Contains(p, "go test ./...") {
		t.Fatalf("prompt missing parts: %q", p)
	}
}
