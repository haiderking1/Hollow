package agent

import (
	"testing"

	"github.com/enough/enough/backend/agent/evidence"
)

func TestScoreRejectsRepeatedFailingBash(t *testing.T) {
	a := &Agent{
		cfg: evidenceRuntime(false),
		step: stepTracker{
			lastBashCommand: "go test ./...",
			lastBashFailed:  true,
		},
	}
	result := toolResult{output: "FAIL", isErr: true}
	if rej := a.scoreToolStep("bash", `{"command":"go test ./..."}`, result); rej == nil {
		t.Fatal("expected rejection for repeated failing bash")
	}
}

func TestScoreRejectsOffTargetEdit(t *testing.T) {
	dir := t.TempDir()
	a := &Agent{
		cfg:     evidenceRuntime(false),
		workDir: dir,
		step: stepTracker{
			lastVerifyFailed: true,
			failurePaths:     []string{"pkg/auth/login.go"},
		},
		ledger: evidence.NewLedger("test"),
	}
	rej := a.scoreToolStep("edit_file", `{"path":"pkg/ui/button.go","old_string":"a","new_string":"b"}`, toolResult{output: "ok"})
	if rej == nil {
		t.Fatal("expected rejection for off-target edit")
	}
}
