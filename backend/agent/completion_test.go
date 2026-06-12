package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/enough/enough/backend/config"
	"github.com/enough/enough/backend/core"
	"github.com/enough/enough/backend/opencode"
)

// scriptedServer returns an SSE server that picks a response per request via
// route. Routes receive the parsed request so they can branch on content.
func scriptedServer(t *testing.T, route func(req opencode.ChatRequest) (text string, calls []toolCallJSON)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req opencode.ChatRequest
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		text, calls := route(req)
		w.Header().Set("Content-Type", "text/event-stream")
		b, _ := json.Marshal(streamChunkJSON(text, calls))
		fmt.Fprintf(w, "data: %s\n\n", b)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
}

func lastMessageText(req opencode.ChatRequest) string {
	if len(req.Messages) == 0 {
		return ""
	}
	return opencode.ContentString(req.Messages[len(req.Messages)-1])
}

func evidenceRuntime(verifier bool) config.Runtime {
	ev := config.DefaultEvidence()
	ev.VerifierEnabled = verifier
	return config.Runtime{Model: "test-model", Evidence: ev}
}

// promptWith runs Prompt against the scripted server (Prompt rebuilds the
// client from cfg.Endpoint).
func promptWith(t *testing.T, a *Agent, srvURL, task string, emit func(core.Event)) {
	t.Helper()
	cfg := a.cfg
	cfg.Endpoint = srvURL
	cfg.APIKey = "k"
	if err := a.Prompt(context.Background(), cfg, task, emit); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
}

// Phase 2: edit → premature "done" is blocked, fixed notice injected, bash
// exit 0 closes the verify obligation, then the turn ends.
func TestCompletionBlockedUntilVerifyRuns(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "f.txt"), "old\n")

	srv := scriptedServer(t, func(req opencode.ChatRequest) (string, []toolCallJSON) {
		last := lastMessageText(req)
		lastRole := req.Messages[len(req.Messages)-1].Role
		switch {
		case lastRole == "user" && strings.Contains(last, "TURN INCOMPLETE"):
			// Round 2: the runtime told us verification is open — run it.
			return "", []toolCallJSON{{Index: 0, ID: "c3", Type: "function",
				Function: toolFnJSON{Name: "bash", Arguments: `{"command":"true"}`}}}
		case lastRole == "user":
			// Round 1: read then edit.
			return "", []toolCallJSON{
				{Index: 0, ID: "c1", Type: "function", Function: toolFnJSON{Name: "read_file", Arguments: `{"path":"f.txt"}`}},
				{Index: 1, ID: "c2", Type: "function", Function: toolFnJSON{Name: "edit_file", Arguments: `{"path":"f.txt","old_string":"old","new_string":"new"}`}},
			}
		default:
			// After tool results: claim done.
			return "done", nil
		}
	})
	defer srv.Close()

	a := &Agent{
		cfg:     evidenceRuntime(false),
		client:  opencode.NewClient(srv.URL, "k", "test-model"),
		workDir: dir,
	}

	promptWith(t, a, srv.URL, "change old to new in f.txt", func(core.Event) {})

	// The notice is injected into the transcript as a marked user message —
	// never surfaced as a chat event (the obligation panel shows the state).
	foundNotice := false
	for _, m := range a.messages {
		if m.Role != "user" {
			continue
		}
		text := opencode.ContentString(m)
		if strings.HasPrefix(text, core.RuntimeNoticePrefix) && strings.Contains(text, "must_run_verify") {
			foundNotice = true
		}
	}
	if !foundNotice {
		t.Fatal("no runtime-marked TURN INCOMPLETE notice in transcript")
	}
	if a.obligations.HasOpen() {
		t.Fatalf("turn ended with open obligations: %+v", a.obligations.Open())
	}
	if !a.obligations.VerifyClosed() {
		t.Fatal("verify obligation not closed by bash exit 0")
	}
}

// Strict mode: an edit after a passing verify reopens the obligation.
func TestEditAfterVerifyReopensObligation(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "f.txt"), "a\n")

	a := &Agent{cfg: evidenceRuntime(false), workDir: dir}
	a.resetEvidenceLedger("t1")
	a.mu.Lock()
	a.obligations = newTestRegistry(a.cfg, "t1", "")
	a.mu.Unlock()

	ctx := context.Background()
	a.executeTool(ctx, "r", "read_file", `{"path":"f.txt"}`)
	a.executeTool(ctx, "e", "edit_file", `{"path":"f.txt","old_string":"a","new_string":"b"}`)
	if !a.obligations.HasOpen() {
		t.Fatal("mutation did not open verify obligation")
	}

	a.executeTool(ctx, "b", "bash", `{"command":"true"}`)
	if a.obligations.HasOpen() {
		t.Fatalf("verify run did not close obligations: %+v", a.obligations.Open())
	}

	a.executeTool(ctx, "e2", "edit_file", `{"path":"f.txt","old_string":"b","new_string":"c"}`)
	if !a.obligations.HasOpen() {
		t.Fatal("edit after passing verify did not reopen the obligation (strict mode)")
	}
}

// A failing command must not close the verify obligation.
func TestFailingCommandDoesNotCloseVerify(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "f.txt"), "a\n")

	a := &Agent{cfg: evidenceRuntime(false), workDir: dir}
	a.resetEvidenceLedger("t1")
	a.mu.Lock()
	a.obligations = newTestRegistry(a.cfg, "t1", "")
	a.mu.Unlock()

	ctx := context.Background()
	a.executeTool(ctx, "r", "read_file", `{"path":"f.txt"}`)
	a.executeTool(ctx, "e", "edit_file", `{"path":"f.txt","old_string":"a","new_string":"b"}`)
	a.executeTool(ctx, "b", "bash", `{"command":"false"}`)
	if a.obligations.VerifyClosed() {
		t.Fatal("exit-1 command closed the verify obligation")
	}
}

// Hard cap: a model that never verifies cannot loop forever.
func TestCompletionRoundCap(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "f.txt"), "old\n")

	calls := 0
	var mu sync.Mutex
	srv := scriptedServer(t, func(req opencode.ChatRequest) (string, []toolCallJSON) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 1 {
			return "", []toolCallJSON{
				{Index: 0, ID: "c1", Type: "function", Function: toolFnJSON{Name: "read_file", Arguments: `{"path":"f.txt"}`}},
				{Index: 1, ID: "c2", Type: "function", Function: toolFnJSON{Name: "edit_file", Arguments: `{"path":"f.txt","old_string":"old","new_string":"new"}`}},
			}
		}
		return "done (no verification, ever)", nil
	})
	defer srv.Close()

	cfg := evidenceRuntime(false)
	cfg.Evidence.MaxCompletionRounds = 2
	a := &Agent{cfg: cfg, client: opencode.NewClient(srv.URL, "k", "test-model"), workDir: dir}

	var capped bool
	var emitMu sync.Mutex
	emit := func(e core.Event) {
		if e.Kind == core.EventSystem {
			if s, ok := e.Data.(string); ok && strings.Contains(s, "completion cap reached") {
				emitMu.Lock()
				capped = true
				emitMu.Unlock()
			}
		}
	}

	promptWith(t, a, srv.URL, "edit f.txt", emit)
	emitMu.Lock()
	defer emitMu.Unlock()
	if !capped {
		t.Fatal("round cap did not fire")
	}
	if !a.obligations.HasOpen() {
		t.Fatal("cap fired but obligations were somehow closed")
	}
}

// Hello-world regression: in a pytest repo, writing a script and running it
// (exit 0) is task-scoped verification. One pass — no pytest, no verifier,
// no incomplete notice.
func TestRunningMutatedScriptCompletesTurnDirectly(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "pyproject.toml"), "")

	srv := scriptedServer(t, func(req opencode.ChatRequest) (string, []toolCallJSON) {
		if isVerifierRequest(req) {
			t.Error("verifier ran despite direct verification evidence")
			return `{"pass": true}`, nil
		}
		last := req.Messages[len(req.Messages)-1]
		if last.Role == "user" {
			return "", []toolCallJSON{
				{Index: 0, ID: "c1", Type: "function", Function: toolFnJSON{Name: "write_file", Arguments: `{"path":"hello.py","content":"print('hi')\n"}`}},
				{Index: 1, ID: "c2", Type: "function", Function: toolFnJSON{Name: "bash", Arguments: `{"command":"python3 hello.py"}`}},
			}
		}
		return "done", nil
	})
	defer srv.Close()

	a := &Agent{cfg: evidenceRuntime(true), client: opencode.NewClient(srv.URL, "k", "test-model"), workDir: dir}
	promptWith(t, a, srv.URL, "write a hello world script", func(core.Event) {})

	if a.obligations.HasOpen() {
		t.Fatalf("open obligations after running the script: %+v", a.obligations.Open())
	}
	for _, m := range a.messages {
		text := opencode.ContentString(m)
		if m.Role == "user" && strings.HasPrefix(text, core.RuntimeNoticePrefix) &&
			strings.Contains(text, "TURN INCOMPLETE") {
			t.Fatal("incomplete notice injected despite direct verification")
		}
	}
}

// Turns with no mutations (pure Q&A) must complete without any obligations.
func TestQATurnHasNoObligations(t *testing.T) {
	dir := t.TempDir()
	srv := scriptedServer(t, func(req opencode.ChatRequest) (string, []toolCallJSON) {
		return "the answer is 42", nil
	})
	defer srv.Close()

	a := &Agent{cfg: evidenceRuntime(true), client: opencode.NewClient(srv.URL, "k", "test-model"), workDir: dir}
	promptWith(t, a, srv.URL, "what is the answer?", func(core.Event) {})
	if a.obligations.HasOpen() {
		t.Fatalf("Q&A turn has open obligations: %+v", a.obligations.Open())
	}
	if len(a.obligations.Snapshot()) != 0 {
		t.Fatalf("Q&A turn created obligations: %+v", a.obligations.Snapshot())
	}
}
