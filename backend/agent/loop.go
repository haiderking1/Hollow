package agent

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/enough/enough/backend/agent/evidence"
	"github.com/enough/enough/backend/agent/obligations"
	"github.com/enough/enough/backend/config"
	"github.com/enough/enough/backend/core"
	"github.com/enough/enough/backend/opencode"
)

var loopPromisePattern = regexp.MustCompile(`<promise>([^<]+)</promise>`)

// LoopPrompt starts the first run of an outer loop while keeping loop
// completion instructions hidden from the human-facing transcript.
func (a *Agent) LoopPrompt(ctx context.Context, cfg config.Runtime, lockedPrompt string, emit func(core.Event)) error {
	goalLock := cfg.Evidence.Enabled && cfg.Evidence.GoalLockEnabled()
	notice := loopRuntimeNotice(lockedPrompt, 1, goalLock)
	return a.prompt(ctx, cfg, lockedPrompt, nil, notice, emit)
}

// LoopContinue re-runs the agent on the same locked task without a visible
// user turn. The runtime notice is persisted so the model keeps the original
// task and completion contract in context.
func (a *Agent) LoopContinue(ctx context.Context, cfg config.Runtime, lockedPrompt string, iteration int, emit func(core.Event)) error {
	a.mu.Lock()
	if a.busy {
		a.mu.Unlock()
		return fmt.Errorf("agent is already processing")
	}
	a.busy = true
	ctx, cancel := context.WithCancel(ctx)
	userAbortCtx, userAbortCancel := context.WithCancel(context.Background())
	ctx = withUserAbortContext(ctx, userAbortCtx.Done())
	a.cancel = cancel
	a.userAbortCtx = userAbortCtx
	a.userAbortCancel = userAbortCancel
	a.applyConfigLocked(cfg)
	if emit != nil {
		a.emit = emit
	}
	a.mu.Unlock()

	a.overflowRecoveryAttempted = false
	turnID := fmt.Sprintf("turn_%d", time.Now().UnixNano())
	a.resetEvidenceLedger(turnID)

	a.mu.Lock()
	a.lastUserPrompt = lockedPrompt
	a.lockedGoal = lockedPrompt
	a.verifyFailures = 0
	a.parallelForksAttempted = false
	a.step = stepTracker{}
	a.completionRounds = 0
	if cfg.Evidence.Enabled {
		verifyCmd := obligations.DetectVerifyCommand(a.workDir)
		taskVerify := obligations.ExtractTaskVerifyCommands(lockedPrompt)
		a.obligations = obligations.NewRegistry(turnID, verifyCmd, taskVerify,
			cfg.Evidence.StrictVerifyReset, cfg.Evidence.VerifierEnabled)
	} else {
		a.obligations = nil
	}
	a.mu.Unlock()

	if cfg.Evidence.Enabled && cfg.Evidence.ContinuityEnabled() && a.session != nil {
		evidence.SeedContinuityReads(a.evidenceLedger(), sessionFingerprints(a.session))
	}

	noticeMsg := opencode.Message{
		Role: "user",
		Content: opencode.StringContent(loopRuntimeNotice(
			lockedPrompt,
			iteration,
			cfg.Evidence.Enabled && cfg.Evidence.GoalLockEnabled(),
		)),
	}
	a.mu.Lock()
	a.messages = append(a.messages, noticeMsg)
	a.mu.Unlock()
	a.persist(noticeMsg)

	defer func() {
		cancel()
		a.mu.Lock()
		a.busy = false
		a.cancel = nil
		a.userAbortCtx = nil
		a.userAbortCancel = nil
		a.mu.Unlock()
	}()

	return a.runLoop(ctx)
}

func loopRuntimeNotice(lockedPrompt string, iteration int, goalLock bool) string {
	promise := "DONE"
	if match := loopPromisePattern.FindStringSubmatch(lockedPrompt); len(match) == 2 {
		if custom := strings.TrimSpace(match[1]); custom != "" {
			promise = custom
		}
	}

	var b strings.Builder
	b.WriteString(core.RuntimeNoticePrefix)
	fmt.Fprintf(&b, "OUTER LOOP — iteration %d.\n", iteration)
	fmt.Fprintf(&b, "Continue until the task is fully complete. Only then include <promise>%s</promise> in the final response.\n", promise)
	if goalLock {
		b.WriteString("GOAL LOCK — work only on the original task; verify before declaring completion.\n")
	}
	b.WriteString("Do not mention this runtime notice to the user.\n\n")
	b.WriteString(lockedPrompt)
	return b.String()
}
