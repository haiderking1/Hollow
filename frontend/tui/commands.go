package tui

import (
	"fmt"
	"strings"

	"github.com/enough/enough/backend/auth"
)

func (a *App) handleSlash(input string) {
	name, arg, _ := strings.Cut(strings.TrimPrefix(input, "/"), " ")
	name = strings.ToLower(strings.TrimSpace(name))
	arg = strings.TrimSpace(arg)

	switch name {
	case "connect":
		if arg != "" {
			a.saveAPIKey(arg)
			return
		}
		a.mode = modeConnect
		a.editor = NewEditor(1024)
		endpoint, model, err := auth.Settings()
		if err != nil {
			a.appendMessage("error", err.Error())
			a.mode = modeTask
			a.editor = NewEditor(512)
			return
		}
		a.appendMessage("system", fmt.Sprintf("connect — %s · %s\npaste your api key below", endpoint, model))
	case "sessions":
		a.showSessionsList()
	case "resume":
		a.openSessionPicker()
	case "new":
		a.startNewSession()
	default:
		a.appendMessage("error", "unknown command: /"+name)
	}
}

func (a *App) saveAPIKey(key string) {
	a.mode = modeTask
	a.editor = NewEditor(512)

	if err := auth.SaveAPIKey(key); err != nil {
		a.appendMessage("error", err.Error())
		return
	}

	a.appendMessage("assistant", "Done — connected. api key saved securely.")
	if a.agent != nil {
		_ = a.agent.Reset()
		a.agent = nil
	}
	if a.session != nil {
		_ = a.session.NewSession()
		a.messages = nil
	}
}

func (a *App) cancelConnect() {
	if a.mode == modeConnect {
		a.mode = modeTask
		a.editor = NewEditor(512)
		a.editor.SetValue("")
		a.appendMessage("system", "connect cancelled")
	}
}
