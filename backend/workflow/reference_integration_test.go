package workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/enough/enough/backend/core"
)

func TestOpenPRAuditReferenceWithMockGH(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "examples", "workflows", "open-pr-audit", "workflow.js"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	runDir := filepath.Join(root, ".enough", "workflows", "reference")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(runDir, "workflow.js")
	if err := os.WriteFile(scriptPath, source, 0o600); err != nil {
		t.Fatal(err)
	}
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	mockGH := `#!/bin/sh
if [ "$1 $2" = "pr list" ]; then
  echo '[{"number":1,"title":"one","headRefName":"one","isDraft":false,"mergeable":"MERGEABLE","updatedAt":"2026-06-19","url":"u1"},{"number":2,"title":"two","headRefName":"two","isDraft":false,"mergeable":"MERGEABLE","updatedAt":"2026-06-19","url":"u2"},{"number":3,"title":"three","headRefName":"three","isDraft":false,"mergeable":"MERGEABLE","updatedAt":"2026-06-19","url":"u3"}]'
elif [ "$1 $2" = "repo view" ]; then
  echo 'owner/repo'
elif [ "$1 $2" = "pr view" ]; then
  if [ "$3" = "1" ]; then echo '[{"path":"src/a.go"}]'
  elif [ "$3" = "2" ]; then echo '[{"path":"src/b.go"}]'
  else echo '[{"path":"docs/readme.md"}]'; fi
else
  echo '{}'
fi
`
	ghPath := filepath.Join(binDir, "gh")
	if err := os.WriteFile(ghPath, []byte(mockGH), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	rt := testRuntime(t, scriptPath)
	rt.workDir = root
	var calls atomic.Int32
	rt.agentRunner = func(ctx context.Context, phase, key string, opts AgentOptions, emit func(core.Event)) AgentResult {
		calls.Add(1)
		switch phase {
		case "audit":
			number, _ := strconv.Atoi(strings.TrimPrefix(key, "audit:"))
			return AgentResult{Key: key, Role: opts.Role, Text: fmt.Sprintf(
				`{"pr":%d,"disposition":"merge-ready","needsRuling":%v,"needsVerify":true,"evidence":["mock"],"summary":"ok"}`,
				number, number < 3)}
		case "rule":
			return AgentResult{Key: key, Role: opts.Role, Text: `{"cluster":"files:src","winner":1,"losers":[2],"dispositions":{"1":"merge-ready","2":"superseded"},"evidence":["mock"]}`}
		default:
			parts := strings.Split(key, ":")
			number, _ := strconv.Atoi(parts[1])
			disposition := parts[2]
			return AgentResult{Key: key, Role: opts.Role, Text: fmt.Sprintf(
				`{"pr":%d,"proposedDisposition":%q,"upheld":true,"finalDisposition":%q,"evidence":["mock"]}`,
				number, disposition, disposition)}
		}
	}
	result, err := rt.Run(context.Background(), scriptPath, RunOptions{ID: "reference"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "done" || calls.Load() != 7 {
		t.Fatalf("status=%s calls=%d snapshot=%+v", result.Status, calls.Load(), rt.Snapshot())
	}
}
