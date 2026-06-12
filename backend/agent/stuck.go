package agent

import (
	"fmt"

	"github.com/enough/enough/backend/core"
	"github.com/enough/enough/backend/opencode"
)

func (a *Agent) noteVerifyFailure() {
	a.mu.Lock()
	a.verifyFailures++
	failures := a.verifyFailures
	shouldFork := !a.parallelForksAttempted &&
		a.cfg.Evidence.ParallelForksEnabled() &&
		a.swarmDepth == 0 &&
		failures >= a.cfg.Evidence.StuckThreshold()
	a.mu.Unlock()

	if shouldFork {
		a.maybeParallelForks()
	}
}

func (a *Agent) noteVerifySuccess() {
	a.mu.Lock()
	a.verifyFailures = 0
	a.step.lastVerifyFailed = false
	a.step.failurePaths = nil
	a.mu.Unlock()
}

func (a *Agent) maybeParallelForks() {
	a.mu.Lock()
	if a.parallelForksAttempted || !a.cfg.Evidence.ParallelForksEnabled() || a.swarmDepth > 0 {
		a.mu.Unlock()
		return
	}
	if a.verifyFailures < a.cfg.Evidence.StuckThreshold() {
		a.mu.Unlock()
		return
	}
	a.parallelForksAttempted = true
	lockedGoal := a.lockedGoal
	lastOutput := a.step.lastVerifyOutput
	verifyCmd := ""
	if reg := a.obligations; reg != nil {
		verifyCmd = reg.VerifyCommand()
	}
	forkCount := a.cfg.Evidence.ForkCount()
	ctx := a.turnCtx
	a.mu.Unlock()

	if ctx == nil {
		return
	}

	summary, merged := a.runParallelForks(ctx, lockedGoal, lastOutput, verifyCmd, forkCount)

	if a.emit != nil {
		msg := fmt.Sprintf("parallel forks: %d workers", forkCount)
		if merged {
			msg += " — winning patch applied"
		} else {
			msg += " — no passing patch yet"
		}
		a.emit(core.Event{Kind: core.EventSystem, Data: msg})
	}

	notice := parallelForkNotice(forkCount, lockedGoal, summary)
	inject := opencode.Message{Role: "user", Content: opencode.StringContent(notice)}
	a.mu.Lock()
	a.messages = append(a.messages, inject)
	a.mu.Unlock()
	a.persist(inject)
}
