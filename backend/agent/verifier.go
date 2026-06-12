package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/enough/enough/backend/agent/evidence"
	"github.com/enough/enough/backend/opencode"
)

// maxVerifierToolRounds caps how many tool-calling rounds the verifier gets
// before it is forced to report. The forced final request carries no tools,
// so the model cannot stall the report by calling tools forever.
const maxVerifierToolRounds = 4

// verifierAllowedTools is the verifier's hard tool allowlist — read-only
// discovery plus bash for running verification commands. Enforced in
// guardTool, not by prompt.
var verifierAllowedTools = map[string]bool{
	"read_file": true,
	"list_dir":  true,
	"glob":      true,
	"grep":      true,
	"bash":      true,
}

const verifierSystemPrompt = `You are a verification agent. You cannot edit code. Your only job is to check, with commands, whether the worker's changes satisfy the task.

Your FIRST tool call must be bash running the verification command you were given. Optionally inspect changed files with read_file/grep after. You have very few tool calls — do not browse.

Then respond with ONLY a JSON object, no other text:
{"pass": true|false, "command_runs": [{"cmd": "...", "exit_code": 0}], "failures": ["specific, factual failure"]}

Rules:
- pass=true only if the verification command exited 0 AND the changes address the task.
- failures must be factual: raw error lines, failing test names, missing behavior. No advice, no coaching.`

const verifierForceReport = `Output the JSON report now. No more tool calls are available. Respond with ONLY the JSON object.`

type verifierReport struct {
	Pass     bool     `json:"pass"`
	Failures []string `json:"failures"`
}

// runVerifier executes the verifier role once and returns factual failure
// strings (empty on sign-off). The verifier shares the worker's ledger and
// obligation registry, so its bash runs are recorded as real evidence and a
// passing verify run closes must_run_verify. Sign-off is granted only when
// the verifier reports pass AND the ledger actually holds a passing verify
// run — the verifier's claim alone is never trusted.
func (a *Agent) runVerifier(ctx context.Context) []string {
	a.mu.Lock()
	reg := a.obligations
	ledger := a.ledger
	task := a.lockedGoal
	if task == "" {
		task = a.lastUserPrompt
	}
	a.mu.Unlock()
	if reg == nil || ledger == nil {
		return nil
	}

	verifier := &Agent{
		cfg:          a.cfg,
		client:       a.client,
		workDir:      a.workDir,
		ledger:       ledger,
		obligations:  reg,
		allowedTools: verifierAllowedTools,
		emit:         a.emit,
	}

	report, err := verifier.verifierLoop(ctx, task)

	failures := report.Failures
	if err != nil {
		failures = append(failures, fmt.Sprintf("verifier error: %v", err))
	}

	pass := err == nil && report.Pass && reg.VerifyClosed()
	if pass {
		if entry, aerr := ledger.Append(evidence.KindVerifierPass, evidence.VerifierPayload{TurnID: ledger.TurnID()}); aerr == nil {
			if reg.NoteVerifierPass(entry.ID) {
				a.emitObligations()
			}
		}
		return nil
	}

	if err == nil && report.Pass && !reg.VerifyClosed() {
		failures = append(failures, "verifier claimed pass but no passing verification run is recorded in the evidence ledger")
	}
	_, _ = ledger.Append(evidence.KindVerifierFail, evidence.VerifierPayload{TurnID: ledger.TurnID(), Failures: failures})
	a.emitObligations()
	return failures
}

// verifierLoop runs the restricted agent until it emits its JSON report.
func (a *Agent) verifierLoop(ctx context.Context, task string) (verifierReport, error) {
	reg := a.obligations

	var b strings.Builder
	fmt.Fprintf(&b, "User task:\n%s\n\n", task)
	if cmd := reg.VerifyCommand(); cmd != "" {
		fmt.Fprintf(&b, "Verification command: %s\n", cmd)
	} else {
		b.WriteString("Verification command: none detected — run the most appropriate explicit check via bash.\n")
	}
	if extras := reg.ExtraVerifyCommands(); len(extras) > 0 {
		b.WriteString("Task-specific checks from the user:\n")
		for _, c := range extras {
			fmt.Fprintf(&b, "- %s\n", c)
		}
	}
	if paths := a.ledger.MutatedPaths(); len(paths) > 0 {
		fmt.Fprintf(&b, "Files changed this turn:\n%s\n", strings.Join(paths, "\n"))
	}
	b.WriteString("\nOpen obligations:\n")
	for _, ob := range reg.Open() {
		fmt.Fprintf(&b, "- %s: %s\n", ob.Kind, ob.Description)
	}

	messages := []opencode.Message{
		{Role: "system", Content: opencode.StringContent(verifierSystemPrompt)},
		{Role: "user", Content: opencode.StringContent(b.String())},
	}

	for round := 0; round < maxVerifierToolRounds; round++ {
		if userAbortFired(ctx) {
			return verifierReport{}, fmt.Errorf("aborted")
		}

		req := opencode.ChatRequest{
			Model:    a.cfg.Model,
			Messages: messages,
			Tools:    verifierTools(),
		}
		msg, err := a.client.ChatStreamRetry(ctx, req, opencode.StreamCallbacks{})
		if err != nil {
			return verifierReport{}, err
		}
		messages = append(messages, msg)

		if len(msg.ToolCalls) == 0 {
			return parseVerifierReport(opencode.ContentString(msg))
		}

		for idx, call := range msg.ToolCalls {
			id := call.ID
			if id == "" {
				id = fmt.Sprintf("verifier_call_%d", idx)
			}
			a.toolStart(id, call.Function.Name, call.Function.Arguments)
			result := a.executeTool(ctx, id, call.Function.Name, call.Function.Arguments)
			a.toolResult(id, result.output, result.isErr)
			messages = append(messages, opencode.Message{
				Role:       "tool",
				ToolCallID: id,
				Name:       call.Function.Name,
				Content:    opencode.StringContent(result.output),
			})
		}
	}

	// Tool budget exhausted: force the report. No tools in the request, so
	// the model can only answer with text.
	if userAbortFired(ctx) {
		return verifierReport{}, fmt.Errorf("aborted")
	}
	messages = append(messages, opencode.Message{
		Role:    "user",
		Content: opencode.StringContent(verifierForceReport),
	})
	msg, err := a.client.ChatStreamRetry(ctx, opencode.ChatRequest{
		Model:    a.cfg.Model,
		Messages: messages,
	}, opencode.StreamCallbacks{})
	if err != nil {
		return verifierReport{}, err
	}
	return parseVerifierReport(opencode.ContentString(msg))
}

// parseVerifierReport extracts the JSON object from the verifier's final
// message, tolerating surrounding prose or code fences.
func parseVerifierReport(text string) (verifierReport, error) {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return verifierReport{}, fmt.Errorf("verifier returned no JSON report: %q", truncateForError(text))
	}
	var r verifierReport
	if err := json.Unmarshal([]byte(text[start:end+1]), &r); err != nil {
		return verifierReport{}, fmt.Errorf("verifier report unparsable: %v", err)
	}
	return r, nil
}

func truncateForError(s string) string {
	const max = 200
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}
