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
	if a.slashActive() {
		a.dismissSlashMenu()
		return
	}
}

func (a *App) handleCtrlC() bool {
	if a.running {
		a.abortAgent()
		return false
	}
	if a.mode == modeSessionPicker && a.sessionPickerConfirmDelete != "" {
		return false
	}

	now := time.Now()
	if now.Sub(a.lastSigintTime) < 500*time.Millisecond {
		a.quit = true
		return true
	}

	if strings.TrimSpace(a.editor.Value()) != "" {
		a.editor.SetValue("")
	}
	a.lastSigintTime = now
	return false
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
}
