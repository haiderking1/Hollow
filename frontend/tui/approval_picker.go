package tui

import (
	"fmt"
	"strings"

	"github.com/enough/enough/backend/approval"
)

type writeApprovalItem struct {
	subsystem string
	id        string
}

func (a *App) approvalPromptAsync(subsystem, id string) {
	select {
	case a.approvalPromptCh <- writeApprovalItem{subsystem: subsystem, id: id}:
	default:
	}
}

func (a *App) promptWriteApproval(item writeApprovalItem) {
	if a.mode == modeWriteApproval {
		a.writeApprovalQueue = append(a.writeApprovalQueue, item)
		a.requestRender()
		return
	}
	a.openWriteApproval(item)
}

func (a *App) openWriteApproval(item writeApprovalItem) {
	r, err := approval.GetPending(item.subsystem, item.id)
	if err != nil || r == nil {
		a.advanceWriteApprovalQueue()
		return
	}
	a.writeApprovalSubsystem = item.subsystem
	a.writeApprovalID = item.id
	a.writeApprovalRecord = r
	a.writeApprovalShowDiff = false
	a.writeApprovalStatus = ""
	a.mode = modeWriteApproval
	a.editor.SetValue("")
	a.requestRender()
}

func (a *App) dismissWriteApproval() {
	a.mode = modeTask
	a.writeApprovalSubsystem = ""
	a.writeApprovalID = ""
	a.writeApprovalRecord = nil
	a.writeApprovalShowDiff = false
	a.writeApprovalStatus = ""
	a.writeApprovalQueue = nil
	a.requestRender()
}

func (a *App) deferWriteApproval() {
	a.writeApprovalSubsystem = ""
	a.writeApprovalID = ""
	a.writeApprovalRecord = nil
	a.writeApprovalShowDiff = false
	a.writeApprovalStatus = ""
	if len(a.writeApprovalQueue) > 0 {
		next := a.writeApprovalQueue[0]
		a.writeApprovalQueue = a.writeApprovalQueue[1:]
		a.openWriteApproval(next)
		return
	}
	a.mode = modeTask
	a.requestRender()
}

func (a *App) advanceWriteApprovalQueue() {
	if len(a.writeApprovalQueue) > 0 {
		next := a.writeApprovalQueue[0]
		a.writeApprovalQueue = a.writeApprovalQueue[1:]
		a.openWriteApproval(next)
		return
	}
	a.dismissWriteApproval()
}

func (a *App) finishWriteApprovalAction(message string, ok bool) {
	if message != "" {
		role := "system"
		if !ok {
			role = "error"
		}
		a.appendMessage(role, message)
	}
	a.advanceWriteApprovalQueue()
}

func (a *App) applyCurrentWriteApproval() {
	if a.writeApprovalRecord == nil {
		a.deferWriteApproval()
		return
	}
	msg, err := a.approvePendingWrite(a.writeApprovalSubsystem, a.writeApprovalID)
	if err != nil {
		a.writeApprovalStatus = err.Error()
		a.requestRender()
		return
	}
	a.finishWriteApprovalAction(msg, true)
}

func (a *App) rejectCurrentWriteApproval() {
	if a.writeApprovalRecord == nil {
		a.deferWriteApproval()
		return
	}
	msg, err := a.rejectPendingWrite(a.writeApprovalSubsystem, a.writeApprovalID)
	if err != nil {
		a.writeApprovalStatus = err.Error()
		a.requestRender()
		return
	}
	a.finishWriteApprovalAction(msg, true)
}

func (a *App) toggleWriteApprovalDiff() {
	a.writeApprovalShowDiff = !a.writeApprovalShowDiff
	a.requestRender()
}

func (a *App) handleWriteApprovalKey(k parsedKey) bool {
	switch k.action {
	case keyRune:
		switch k.r {
		case 'y', 'Y':
			a.applyCurrentWriteApproval()
			return true
		case 'n', 'N':
			a.rejectCurrentWriteApproval()
			return true
		case 'd', 'D':
			a.toggleWriteApprovalDiff()
			return true
		}
	case keyEnter:
		a.applyCurrentWriteApproval()
		return true
	case keyEscape:
		a.deferWriteApproval()
		return true
	}
	return false
}

func (a *App) renderWriteApprovalPicker(width int) string {
	if a.mode != modeWriteApproval || a.writeApprovalRecord == nil {
		return ""
	}

	r := a.writeApprovalRecord
	label := "skill"
	if a.writeApprovalSubsystem == approval.SubsystemMemory {
		label = "memory"
	}

	var lines []string
	title := fmt.Sprintf("  Approve %s write (%s)", label, r.ID)
	if queued := len(a.writeApprovalQueue); queued > 0 {
		title += fmt.Sprintf(" · +%d queued", queued)
	}
	lines = append(lines, a.styles.SlashSelected.Render(title))

	summary := strings.TrimSpace(r.Summary)
	if summary == "" {
		summary = "(no summary)"
	}
	lines = append(lines, a.styles.Text.Render("  "+truncatePreview(summary, width-4)))

	meta := fmt.Sprintf("  action: %s · origin: %s", strings.TrimSpace(r.Action), strings.TrimSpace(r.Origin))
	if meta == "  action:  · origin: " {
		meta = ""
	}
	if meta != "" {
		lines = append(lines, a.styles.SlashDim.Render(meta))
	}

	if a.writeApprovalShowDiff {
		diff, err := a.pendingWriteDiff(a.writeApprovalSubsystem, a.writeApprovalID)
		if err != nil {
			lines = append(lines, a.styles.AssistError.Render("  "+err.Error()))
		} else {
			for _, line := range strings.Split(diff, "\n") {
				line = strings.TrimRight(line, " ")
				if line == "" {
					continue
				}
				style := a.styles.SlashDim
				if strings.HasPrefix(line, "+") {
					style = a.styles.LogAccent
				} else if strings.HasPrefix(line, "-") {
					style = a.styles.AssistError
				}
				lines = append(lines, style.Render("  "+truncatePreview(line, width-4)))
			}
		}
	}

	var hint string
	switch {
	case a.writeApprovalStatus != "":
		hint = a.styles.AssistError.Render("  " + a.writeApprovalStatus)
	default:
		diffHint := "show diff"
		if a.writeApprovalShowDiff {
			diffHint = "hide diff"
		}
		hint = a.styles.SlashDim.Render(fmt.Sprintf("  y/enter approve · n reject · d %s · esc decide later", diffHint))
	}
	lines = append(lines, hint)

	box := a.styles.SlashMenu.Width(width - 2).Render(strings.Join(lines, "\n"))
	return box
}
