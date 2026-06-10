package tui

import (
	"strings"
	"time"
)

func (a *App) handleInterrupt() {
	if a.running {
		a.abortAgent()
		return
	}
	if a.mode == modeSessionPicker {
		a.dismissSessionPicker()
		return
	}
	if a.slashActive() {
		a.dismissSlashMenu()
		return
	}
	if a.mode == modeConnect {
		a.cancelConnect()
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
