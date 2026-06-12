package agent

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/enough/enough/backend/agent/evidence"
	"github.com/enough/enough/backend/agent/obligations"
	"github.com/enough/enough/backend/config"
	"github.com/enough/enough/backend/core"
	"github.com/enough/enough/backend/opencode"
)

func newTestRegistry(cfg config.Runtime, turnID, verifyCmd string) *obligations.Registry {
	return obligations.NewRegistry(turnID, verifyCmd, nil, cfg.Evidence.StrictVerifyReset, cfg.Evidence.VerifierEnabled)
}

// The verifier's tool allowlist is enforced at the registry/guard level, not
// by prompt: write/edit/swarm calls hard-fail.
func TestVerifierCannotMutate(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "f.txt"), "x\n")

	v := &Agent{
		cfg:          evidenceRuntime(true),
		workDir:      dir,
		allowedTools: verifierAllowedTools,
	}

	ctx := context.Background()
	for name, args := range map[string]string{
		"write_file":  `{"path":"f.txt","content":"hacked"}`,
		"edit_file":   `{"path":"f.txt","old_string":"x","new_string":"y"}`,
		"agent_swarm": `{"tasks":[{"id":"a","prompt":"p"}]}`,
		"web_search":  `{"query":"q"}`,
	} {
		res := v.executeTool(ctx, "t", name, args)
		if !res.isErr || !strings.Contains(res.output, "not permitted") {
			t.Fatalf("verifier %s was not rejected: %+v", name, res)
		}
	}

	if res := v.executeTool(ctx, "t", "read_file", `{"path":"f.txt"}`); res.isErr {
		t.Fatalf("verifier read_file rejected: %s", res.output)
	}
}

func isVerifierRequest(req opencode.ChatRequest) bool {
	return len(req.Messages) > 0 && strings.Contains(opencode.ContentString(req.Messages[0]), "verification agent")
}

// Phase 3 end-to-end: worker stops early without verifying; the verifier runs
// the check itself (closing must_run_verify via the shared ledger), reports
// pass, and the runtime signs off. The worker is never re-entered.
func TestVerifierRunsVerificationAndSignsOff(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "f.txt"), "old\n")

	srv := scriptedServer(t, func(req opencode.ChatRequest) (string, []toolCallJSON) {
		if isVerifierRequest(req) {
			if req.Messages[len(req.Messages)-1].Role == "tool" {
				return `{"pass": true, "command_runs": [{"cmd": "true", "exit_code": 0}], "failures": []}`, nil
			}
			return "", []toolCallJSON{{Index: 0, ID: "v1", Type: "function",
				Function: toolFnJSON{Name: "bash", Arguments: `{"command":"true"}`}}}
		}
		// Worker: edit, then immediately claim done without verifying.
		if req.Messages[len(req.Messages)-1].Role == "user" {
			return "", []toolCallJSON{
				{Index: 0, ID: "c1", Type: "function", Function: toolFnJSON{Name: "read_file", Arguments: `{"path":"f.txt"}`}},
				{Index: 1, ID: "c2", Type: "function", Function: toolFnJSON{Name: "edit_file", Arguments: `{"path":"f.txt","old_string":"old","new_string":"new"}`}},
			}
		}
		return "done", nil
	})
	defer srv.Close()

	a := &Agent{cfg: evidenceRuntime(true), client: opencode.NewClient(srv.URL, "k", "test-model"), workDir: dir}
	promptWith(t, a, srv.URL, "change old to new", func(core.Event) {})

	if a.obligations.HasOpen() {
		t.Fatalf("open obligations after verifier pass: %+v", a.obligations.Open())
	}

	var sawPass bool
	for _, e := range a.ledger.Entries() {
		if e.Kind == evidence.KindVerifierPass {
			sawPass = true
		}
	}
	if !sawPass {
		t.Fatal("no verifier_pass evidence in ledger")
	}
}

// A verifier "pass" with no passing verify run in the ledger is not trusted.
func TestVerifierPassClaimRequiresLedgerEvidence(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "f.txt"), "x\n")

	srv := scriptedServer(t, func(req opencode.ChatRequest) (string, []toolCallJSON) {
		// Verifier lies: claims pass without running anything.
		return `{"pass": true, "command_runs": [], "failures": []}`, nil
	})
	defer srv.Close()

	cfg := evidenceRuntime(true)
	a := &Agent{cfg: cfg, client: opencode.NewClient(srv.URL, "k", "test-model"), workDir: dir}
	a.resetEvidenceLedger("t1")
	a.mu.Lock()
	a.obligations = newTestRegistry(cfg, "t1", "")
	a.mu.Unlock()
	a.obligations.NoteMutation()

	failures := a.runVerifier(context.Background())
	if len(failures) == 0 {
		t.Fatal("forged pass accepted without ledger evidence")
	}
	if a.obligations.VerifyClosed() {
		t.Fatal("verify closed without any command run")
	}
	var sawFail bool
	for _, e := range a.ledger.Entries() {
		if e.Kind == evidence.KindVerifierFail {
			sawFail = true
		}
	}
	if !sawFail {
		t.Fatal("no verifier_fail evidence recorded")
	}
}

// Verifier failure facts reach the worker as raw text in the incomplete notice.
func TestVerifierFailureFactsInjectedToWorker(t *testing.T) {
	notice := incompleteNotice(
		[]obligations.Obligation{{Kind: obligations.KindMustRunVerify, Description: "go test ./... must exit 0 (run via bash)"}},
		[]string{"FAIL: TestFoo (0.01s) — expected 2, got 3"},
		"fix the foo test",
	)
	if !strings.Contains(notice, "TURN INCOMPLETE") {
		t.Fatalf("missing header: %q", notice)
	}
	if !strings.Contains(notice, "must_run_verify") {
		t.Fatalf("missing obligation: %q", notice)
	}
	if !strings.Contains(notice, "VERIFIER FAILURE: FAIL: TestFoo") {
		t.Fatalf("missing factual failure: %q", notice)
	}
}

// A verifier that keeps calling tools is cut off after the tool budget and
// forced into a final no-tools report turn.
func TestVerifierForcedReportAfterToolBudget(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "f.txt"), "x\n")

	srv := scriptedServer(t, func(req opencode.ChatRequest) (string, []toolCallJSON) {
		if len(req.Tools) == 0 {
			// Forced final turn: no tools offered, must answer.
			return `{"pass": false, "failures": ["budget exhausted before verification"]}`, nil
		}
		// Stall: keep reading files forever.
		return "", []toolCallJSON{{Index: 0, ID: "v", Type: "function",
			Function: toolFnJSON{Name: "read_file", Arguments: `{"path":"f.txt"}`}}}
	})
	defer srv.Close()

	cfg := evidenceRuntime(true)
	cfg.Endpoint = srv.URL
	a := &Agent{cfg: cfg, client: opencode.NewClient(srv.URL, "k", "test-model"), workDir: dir}
	a.resetEvidenceLedger("t1")
	a.mu.Lock()
	a.obligations = newTestRegistry(cfg, "t1", "")
	a.mu.Unlock()
	a.obligations.NoteMutation()

	failures := a.runVerifier(context.Background())
	if len(failures) == 0 || !strings.Contains(strings.Join(failures, " "), "budget exhausted") {
		t.Fatalf("forced report not collected: %q", failures)
	}
}

func TestParseVerifierReport(t *testing.T) {
	r, err := parseVerifierReport("Here is my report:\n```json\n{\"pass\": false, \"failures\": [\"f1\"]}\n```")
	if err != nil {
		t.Fatal(err)
	}
	if r.Pass || len(r.Failures) != 1 || r.Failures[0] != "f1" {
		t.Fatalf("bad parse: %+v", r)
	}

	if _, err := parseVerifierReport("no json here"); err == nil {
		t.Fatal("expected error for missing JSON")
	}
}
