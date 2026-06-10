package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/enough/enough/backend/agent"
	"github.com/enough/enough/backend/config"
	"github.com/enough/enough/backend/session"
)

func (a *App) handleSessionPickerKey(k parsedKey) bool {
	switch k.action {
	case keyUp:
		if a.sessionPickerCursor > 0 {
			a.sessionPickerCursor--
		}
		a.requestRender()
		return true
	case keyDown:
		a.sessionPickerCursor++
		a.clampSessionPickerCursor()
		a.requestRender()
		return true
	case keyRune:
		if k.r == 'k' || k.r == 'K' {
			if a.sessionPickerCursor > 0 {
				a.sessionPickerCursor--
			}
			a.requestRender()
			return true
		}
		if k.r == 'j' || k.r == 'J' {
			a.sessionPickerCursor++
			a.clampSessionPickerCursor()
			a.requestRender()
			return true
		}
	case keyTab:
		a.toggleSessionPickerScope()
		a.requestRender()
		return true
	case keyEnter:
		a.pickSession()
		a.requestRender()
		return true
	case keyCtrlD, keyCtrlBackspace:
		a.startSessionDeleteConfirm()
		a.requestRender()
		return true
	}
	return false
}

func (a *App) showSessionsList() {
	if a.session == nil {
		a.appendMessage("error", "no active session")
		return
	}

	infos, err := session.ListForCWD(a.session.CWD())
	if err != nil {
		a.appendMessage("error", err.Error())
		return
	}

	a.appendMessage("system", formatSessionList(infos, a.session.SessionFile(), "Sessions (this project)"))
}

func (a *App) openSessionPicker() {
	if a.running {
		a.appendMessage("error", "wait for the agent to finish")
		return
	}

	a.sessionPickerAll = false
	a.sessionPickerCursor = 0
	a.sessionPickerConfirmDelete = ""
	a.sessionPickerStatus = ""
	if err := a.reloadSessionPicker(); err != nil {
		a.appendMessage("error", err.Error())
		return
	}
	if len(a.sessionPickerItems) == 0 {
		a.appendMessage("system", "no sessions found for this project")
		return
	}

	a.mode = modeSessionPicker
	a.editor.SetValue("")
}

func (a *App) reloadSessionPicker() error {
	var (
		infos []session.Info
		err   error
	)
	if a.sessionPickerAll {
		infos, err = session.ListAll()
	} else {
		infos, err = session.ListForCWD("")
	}
	if err != nil {
		return err
	}
	a.sessionPickerItems = infos
	a.clampSessionPickerCursor()
	return nil
}

func (a *App) toggleSessionPickerScope() {
	a.sessionPickerAll = !a.sessionPickerAll
	a.sessionPickerCursor = 0
	a.sessionPickerConfirmDelete = ""
	if err := a.reloadSessionPicker(); err != nil {
		a.appendMessage("error", err.Error())
	}
}

func (a *App) clampSessionPickerCursor() {
	if len(a.sessionPickerItems) == 0 {
		a.sessionPickerCursor = 0
		return
	}
	if a.sessionPickerCursor >= len(a.sessionPickerItems) {
		a.sessionPickerCursor = len(a.sessionPickerItems) - 1
	}
	if a.sessionPickerCursor < 0 {
		a.sessionPickerCursor = 0
	}
}

func (a *App) dismissSessionPicker() {
	a.mode = modeTask
	a.sessionPickerItems = nil
	a.sessionPickerCursor = 0
	a.sessionPickerConfirmDelete = ""
	a.sessionPickerStatus = ""
}

func (a *App) pickSession() {
	if a.sessionPickerConfirmDelete != "" {
		return
	}
	if a.sessionPickerCursor < 0 || a.sessionPickerCursor >= len(a.sessionPickerItems) {
		return
	}
	path := a.sessionPickerItems[a.sessionPickerCursor].Path
	a.dismissSessionPicker()
	a.resumeSession(path)
}

func (a *App) isCurrentSessionPath(path string) bool {
	if a.session == nil {
		return false
	}
	return filepath.Clean(path) == filepath.Clean(a.session.SessionFile())
}

func (a *App) startSessionDeleteConfirm() {
	if a.sessionPickerCursor < 0 || a.sessionPickerCursor >= len(a.sessionPickerItems) {
		return
	}
	path := a.sessionPickerItems[a.sessionPickerCursor].Path
	if a.isCurrentSessionPath(path) {
		a.sessionPickerStatus = "Cannot delete the currently active session"
		a.sessionPickerConfirmDelete = ""
		return
	}
	a.sessionPickerStatus = ""
	a.sessionPickerConfirmDelete = path
}

func (a *App) cancelSessionDeleteConfirm() {
	a.sessionPickerConfirmDelete = ""
	a.sessionPickerStatus = ""
}

func (a *App) confirmSessionDelete() {
	path := a.sessionPickerConfirmDelete
	a.sessionPickerConfirmDelete = ""
	if path == "" {
		return
	}
	if a.isCurrentSessionPath(path) {
		a.sessionPickerStatus = "Cannot delete the currently active session"
		return
	}

	result, err := session.Delete(path)
	if err != nil {
		a.sessionPickerStatus = "Failed to delete: " + err.Error()
		return
	}

	if result.Method == "trash" {
		a.sessionPickerStatus = "Session moved to trash"
	} else {
		a.sessionPickerStatus = "Session deleted"
	}

	if err := a.reloadSessionPicker(); err != nil {
		a.sessionPickerStatus = err.Error()
		return
	}
	if len(a.sessionPickerItems) == 0 {
		a.dismissSessionPicker()
		a.appendMessage("system", "no sessions left")
	}
}

func (a *App) resumeSession(path string) {
	if a.running {
		a.appendMessage("error", "wait for the agent to finish")
		return
	}

	sm, err := session.Open(path)
	if err != nil {
		a.appendMessage("error", err.Error())
		return
	}

	a.session = sm
	a.messages = nil
	for _, line := range sm.ChatLines() {
		a.messages = append(a.messages, chatMsg{
			role:     line.Role,
			text:     line.Text,
			thinking: line.Thinking,
		})
	}

	if a.agent != nil {
		a.agent.LoadSession(sm)
	} else if cfg, err := config.LoadRuntime(); err == nil {
		a.agent = agent.New(cfg, "", sm)
	}

	name := filepath.Base(path)
	a.appendMessage("system", fmt.Sprintf("resumed session · %s", name))
}

func (a *App) startNewSession() {
	if a.running {
		a.appendMessage("error", "wait for the agent to finish")
		return
	}

	if a.agent != nil {
		if err := a.agent.Reset(); err != nil {
			a.appendMessage("error", err.Error())
			return
		}
	} else if a.session != nil {
		if err := a.session.NewSession(); err != nil {
			a.appendMessage("error", err.Error())
			return
		}
	}

	a.messages = nil
	a.appendMessage("system", "new session started")
}

func (a *App) renderSessionPicker(width int) string {
	if a.mode != modeSessionPicker {
		return ""
	}

	a.clampSessionPickerCursor()

	scope := "this project"
	if a.sessionPickerAll {
		scope = "all projects"
	}
	title := fmt.Sprintf("  Resume session (%s)", scope)

	var lines []string
	lines = append(lines, a.styles.SlashSelected.Render(title))

	currentFile := ""
	if a.session != nil {
		currentFile = a.session.SessionFile()
	}

	if len(a.sessionPickerItems) == 0 {
		lines = append(lines, a.styles.SlashDim.Render("  no sessions found"))
	} else {
		for i, info := range a.sessionPickerItems {
			marker := "  "
			if info.Path == currentFile {
				marker = "* "
			}
			if i == a.sessionPickerCursor {
				if info.Path == currentFile {
					marker = "›*"
				} else {
					marker = "› "
				}
			}

			when := session.FormatRelative(info.Modified)
			preview := truncatePreview(info.FirstMessage, 48)
			line := fmt.Sprintf("%s%s  %s  · %d msgs · %s", marker, when, preview, info.MessageCount, session.ShortenPath(info.CWD))

			confirming := info.Path == a.sessionPickerConfirmDelete
			style := a.styles.SlashDim
			if confirming {
				style = a.styles.AssistError
			} else if i == a.sessionPickerCursor {
				style = a.styles.SlashSelected
			}

			lines = append(lines, style.Render(line))
		}
	}

	var hint string
	switch {
	case a.sessionPickerConfirmDelete != "":
		hint = a.styles.AssistError.Render("  Delete session? enter confirm · esc cancel")
	case a.sessionPickerStatus != "":
		if strings.HasPrefix(a.sessionPickerStatus, "Failed") || strings.HasPrefix(a.sessionPickerStatus, "Cannot") {
			hint = a.styles.AssistError.Render("  " + a.sessionPickerStatus)
		} else {
			hint = a.styles.LogAccent.Render("  " + a.sessionPickerStatus)
		}
	default:
		hint = a.styles.SlashDim.Render("  ↑↓ pick   enter resume   tab scope   ctrl+d delete   esc close")
	}
	body := strings.Join(lines, "\n") + "\n" + hint

	return a.styles.SlashMenu.
		Width(width - 2).
		Render(body)
}

func formatSessionList(infos []session.Info, currentFile, title string) string {
	if len(infos) == 0 {
		return title + "\n  (none)"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s\n", title)
	for _, info := range infos {
		marker := "  "
		if info.Path == currentFile {
			marker = "* "
		}
		when := session.FormatRelative(info.Modified)
		preview := truncatePreview(info.FirstMessage, 56)
		fmt.Fprintf(&b, "%s%s  %s  · %d msgs · %s\n", marker, when, preview, info.MessageCount, session.ShortenPath(info.CWD))
	}
	b.WriteString("\n  /resume to switch or delete")
	return strings.TrimSpace(b.String())
}

func truncatePreview(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
