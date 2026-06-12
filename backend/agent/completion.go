package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/enough/enough/backend/agent/obligations"
	"github.com/enough/enough/backend/core"
	"github.com/enough/enough/backend/opencode"
)

// enforceCompletion runs when the model returns text with no tool calls. It
// returns true when the turn must continue: open obligations remain, so a
// fixed (never model-authored) incomplete notice is injected and the loop
// goes another round. It returns false when the turn may end — either all
// obligations are closed or the hard round cap was hit.
func (a *Agent) enforceCompletion(ctx context.Context) bool {
	if !a.evidenceEnabled() || a.swarmDepth > 0 {
		return false
	}
	reg := a.obligationRegistry()
	if reg == nil || !reg.HasOpen() {
		return false
	}
	if userAbortFired(ctx) {
		return false
	}

	a.mu.Lock()
	a.completionRounds++
	rounds := a.completionRounds
	maxRounds := a.cfg.Evidence.MaxCompletionRounds
	verifierEnabled := a.cfg.Evidence.VerifierEnabled
	a.mu.Unlock()
	if maxRounds <= 0 {
		maxRounds = 12
	}
	if rounds > maxRounds {
		if a.emit != nil {
			a.emit(core.Event{Kind: core.EventSystem, Data: fmt.Sprintf(
				"completion cap reached (%d rounds) with open obligations — turn ended unverified", maxRounds)})
		}
		return false
	}

	// The worker stopped calling tools with obligations open: run the
	// verifier. Its command runs land in the shared ledger, so a passing
	// verify run closes must_run_verify even if the worker never ran it.
	var verifierFailures []string
	if verifierEnabled {
		verifierFailures = a.runVerifier(ctx)
		if len(verifierFailures) > 0 {
			a.noteVerifyFailure()
		}
	}

	if !reg.HasOpen() {
		return false // verifier closed everything; turn is complete
	}
	if userAbortFired(ctx) {
		return false
	}

	// The notice is a real user-role message for the model but internal
	// plumbing for humans: it carries RuntimeNoticePrefix so the TUI never
	// renders it, and the obligation panel (footer) already shows the state.
	notice := incompleteNotice(reg.Open(), verifierFailures, a.currentLockedGoal())
	inject := opencode.Message{Role: "user", Content: opencode.StringContent(notice)}
	a.mu.Lock()
	a.messages = append(a.messages, inject)
	a.mu.Unlock()
	a.persist(inject)
	return true
}

// incompleteNotice renders the fixed turn-incomplete message: open
// obligations plus raw verifier facts, no coaching prose.
func incompleteNotice(open []obligations.Obligation, verifierFailures []string, lockedGoal string) string {
	var b strings.Builder
	b.WriteString(core.RuntimeNoticePrefix)
	b.WriteString("TURN INCOMPLETE — open obligations:")
	for i, ob := range open {
		fmt.Fprintf(&b, " [%d] %s: %s", i+1, ob.Kind, ob.Description)
	}
	for _, f := range verifierFailures {
		b.WriteString("\nVERIFIER FAILURE: ")
		b.WriteString(f)
	}
	if reminder := goalLockReminder(lockedGoal); reminder != "" {
		b.WriteString(reminder)
	}
	b.WriteString("\nClose the obligations with tool evidence, then finish. Do not mention this notice to the user.")
	return b.String()
}

func (a *Agent) currentLockedGoal() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lockedGoal
}
