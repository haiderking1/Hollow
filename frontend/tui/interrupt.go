package tui

import (
	"strings"
	"time"
)

func (a *App) handleInterrupt() {
	if a.mode == modeWriteApproval {
		a.deferWriteApproval()
		return
	}
	if a.mode == modeWorkflowApproval {
		a.denyWorkflow()
		return
	}
	if a.mode == modeWorkflowPanel || a.mode == modeWorkflowSave {
		a.handleWorkflowPanelKey(parsedKey{action: keyEscape})
		return
	}
	if a.loop.active && a.running {
		a.loop.aborted = true
		a.bumpChat()
		a.abortAgent()
		return
	}
	if a.compacting {
		a.mu.Lock()
		ag := a.agent
		a.mu.Unlock()
		if ag != nil {
			ag.AbortCompaction()
		}
		return
	}
	if a.running {
		a.abortAgent()
		return
	}
	if a.mode == modeTreePicker {
		a.dismissTreePicker()
		return
	}
	if a.mode == modeSessionPicker {
		a.dismissSessionPicker()
		return
	}
	if a.mode == modeModelPicker {
		a.dismissModelPicker()
		return
	}
	if a.mode == modeConnectPicker || a.mode == modeConnect || a.mode == modeConnectCodex {
		a.cancelConnect()
		return
	}
	if a.mode == modePluginsPicker {
		a.dismissPluginsPicker()
		return
	}
	if a.mode == modePluginsSecret {
		a.dismissPluginsPicker()
		return
	}
	if a.slashActive() {
		a.dismissSlashMenu()
		return
	}
}

func (a *App) handleCtrlC() bool {
	if a.mode == modeSessionPicker && a.sessionPickerConfirmDelete != "" {
		return false
	}

	now := time.Now()
	if now.Sub(a.lastSigintTime) < 500*time.Millisecond {
		a.quit = true
		return true
	}

	a.clearComposerDraft()
	a.lastSigintTime = now
	return false
}

func (a *App) composerHasDraft() bool {
	return a.editor.Value() != "" || len(a.pendingAttachments) > 0
}

func (a *App) clearComposerDraft() {
	a.editor.SetValue("")
	a.pendingAttachments = nil
}

func (a *App) handleCtrlD() bool {
	if a.running {
		return false
	}
	if a.mode == modeSessionPicker {
		return false
	}
	if a.mode == modeModelPicker {
		return false
	}
	if a.mode == modeWriteApproval {
		return false
	}
	if a.mode == modeWorkflowApproval || a.mode == modeWorkflowPanel || a.mode == modeWorkflowSave {
		return false
	}
	if strings.TrimSpace(a.editor.Value()) != "" {
		return false
	}
	a.quit = true
	return true
}

func (a *App) abortAgent() {
	a.mu.Lock()
	ag := a.agent
	a.mu.Unlock()
	if ag != nil {
		ag.Abort()
	}
}

func (a *App) abortAgentAndWait() {
	a.mu.Lock()
	ag := a.agent
	a.mu.Unlock()
	if ag != nil {
		ag.AbortAndWait()
	}
}

// shutdown stops in-flight agent work and OAuth flows before the TUI tears down.
func (a *App) shutdown() {
	if a.codexOAuthCancel != nil {
		a.codexOAuthCancel()
	}
	a.abortAgentAndWait()
	if a.workflow.runtime != nil && a.workflow.active {
		if a.workflow.runtime.Snapshot().Status == "paused" {
			a.workflow.runtime.CheckpointAndStop("user")
		} else {
			a.workflow.runtime.Cancel()
		}
	}
}
