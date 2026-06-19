package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	workflowpkg "github.com/enough/enough/backend/workflow"
)

func (a *App) handleWorkflowPanelKey(k parsedKey) bool {
	if a.mode == modeWorkflowSave {
		switch k.action {
		case keyEscape:
			a.mode = modeWorkflowPanel
			a.editor = NewTaskEditor()
			return true
		case keyTab:
			a.workflow.saveProject = !a.workflow.saveProject
			return true
		case keyEnter:
			name := strings.TrimSpace(a.editor.Value())
			if name == "" {
				a.workflow.saveStatus = "name is required"
				return true
			}
			a.saveLastWorkflow(name, a.workflow.saveProject)
			return true
		}
		return false
	}

	switch k.action {
	case keyEscape:
		if a.workflow.panelHelp {
			a.workflow.panelHelp = false
		} else if a.workflow.panelLevel > 0 {
			a.workflow.panelLevel--
			a.workflow.panelCursor = 0
			a.workflow.panelScroll = 0
		} else if a.workflow.panelLevel == 0 && len(a.workflow.runs) > 0 {
			a.workflow.panelLevel = -1
			a.workflow.panelCursor = 0
		} else {
			a.mode = modeTask
		}
		return true
	case keyUp:
		a.moveWorkflowCursor(-1)
		return true
	case keyDown:
		a.moveWorkflowCursor(1)
		return true
	case keyEnter, keyRight:
		a.drillWorkflowPanel()
		return true
	case keyRune:
		switch k.r {
		case 'j':
			if a.workflow.panelLevel == 2 {
				a.workflow.panelScroll++
			} else {
				a.moveWorkflowCursor(1)
			}
		case 'k':
			if a.workflow.panelLevel == 2 && a.workflow.panelScroll > 0 {
				a.workflow.panelScroll--
			} else {
				a.moveWorkflowCursor(-1)
			}
		case 'p', 'P':
			a.toggleWorkflowPause()
		case 'x', 'X':
			a.stopWorkflowFocus()
		case 'r', 'R':
			a.restartWorkflowFocus()
		case 's', 'S':
			if a.workflow.panelLevel == -1 && len(a.workflow.runs) > 0 {
				a.moveWorkflowCursor(0)
				state := a.workflow.runs[a.workflow.panelCursor]
				a.workflow.lastScript = state.ScriptPath
				a.workflow.lastID = state.ID
			}
			a.mode = modeWorkflowSave
			a.workflow.saveProject = true
			a.workflow.saveStatus = ""
			a.editor = NewTaskEditor()
		case '?':
			a.workflow.panelHelp = !a.workflow.panelHelp
		default:
			return false
		}
		return true
	}
	return false
}

func (a *App) moveWorkflowCursor(delta int) {
	count := a.workflowPanelItemCount()
	if count == 0 {
		a.workflow.panelCursor = 0
		return
	}
	a.workflow.panelCursor += delta
	if a.workflow.panelCursor < 0 {
		a.workflow.panelCursor = 0
	}
	if a.workflow.panelCursor >= count {
		a.workflow.panelCursor = count - 1
	}
}

func (a *App) workflowPanelItemCount() int {
	s := a.workflowSnapshot()
	switch a.workflow.panelLevel {
	case -1:
		return len(a.workflow.runs)
	case 0:
		return len(s.Phases)
	case 1:
		return len(workflowAgentsForPhase(s, a.workflow.panelPhase))
	default:
		return 1
	}
}

func (a *App) drillWorkflowPanel() {
	s := a.workflowSnapshot()
	switch a.workflow.panelLevel {
	case -1:
		if len(a.workflow.runs) == 0 {
			return
		}
		a.moveWorkflowCursor(0)
		state := a.workflow.runs[a.workflow.panelCursor]
		if a.workflow.runtime != nil && a.workflow.runtime.Snapshot().ID == state.ID {
			a.workflow.snapshot = a.workflow.runtime.Snapshot()
		} else {
			a.workflow.snapshot = workflowpkg.SnapshotFromState(state)
		}
		a.workflow.lastScript = state.ScriptPath
		a.workflow.lastID = state.ID
		a.workflow.panelLevel = 0
		a.workflow.panelCursor = 0
	case 0:
		if len(s.Phases) == 0 {
			return
		}
		a.moveWorkflowCursor(0)
		a.workflow.panelPhase = s.Phases[a.workflow.panelCursor].Name
		a.workflow.panelLevel = 1
		a.workflow.panelCursor = 0
	case 1:
		agents := workflowAgentsForPhase(s, a.workflow.panelPhase)
		if len(agents) == 0 {
			return
		}
		a.moveWorkflowCursor(0)
		a.workflow.panelAgent = agents[a.workflow.panelCursor].Key
		a.workflow.panelLevel = 2
		a.workflow.panelScroll = 0
	}
}

func (a *App) toggleWorkflowPause() {
	if a.workflow.panelLevel == -1 && len(a.workflow.runs) > 0 {
		a.moveWorkflowCursor(0)
		state := a.workflow.runs[a.workflow.panelCursor]
		if a.workflow.runtime == nil || a.workflow.runtime.Snapshot().ID != state.ID {
			if state.Status == "paused" {
				a.resumeWorkflow(state.ID)
			}
			return
		}
	}
	if a.workflow.runtime == nil {
		if a.workflow.snapshot.Status == "paused" {
			a.resumeWorkflow(a.workflow.snapshot.ID)
		}
		return
	}
	s := a.workflow.runtime.Snapshot()
	if s.Status == "paused" {
		if a.workflow.active {
			a.workflow.runtime.Resume()
		} else {
			a.resumeWorkflow(s.ID)
		}
	} else if s.Status == "running" {
		a.workflow.runtime.Pause()
	}
}

func (a *App) stopWorkflowFocus() {
	if a.workflow.runtime == nil {
		return
	}
	if a.workflow.panelLevel == -1 {
		if len(a.workflow.runs) == 0 {
			return
		}
		a.moveWorkflowCursor(0)
		if a.workflow.runs[a.workflow.panelCursor].ID == a.workflow.runtime.Snapshot().ID {
			a.workflow.runtime.Cancel()
		}
		return
	}
	if a.workflow.panelLevel == 0 {
		a.workflow.runtime.Cancel()
		return
	}
	key := a.focusedWorkflowAgent()
	if key != "" {
		a.workflow.runtime.StopAgent(key)
	}
}

func (a *App) restartWorkflowFocus() {
	if a.workflow.runtime == nil {
		return
	}
	if key := a.focusedWorkflowAgent(); key != "" {
		a.workflow.runtime.RestartAgent(key)
	}
}

func (a *App) focusedWorkflowAgent() string {
	if a.workflow.panelLevel == 2 {
		return a.workflow.panelAgent
	}
	if a.workflow.panelLevel != 1 {
		return ""
	}
	agents := workflowAgentsForPhase(a.workflowSnapshot(), a.workflow.panelPhase)
	if len(agents) == 0 {
		return ""
	}
	a.moveWorkflowCursor(0)
	return agents[a.workflow.panelCursor].Key
}

func (a *App) workflowSnapshot() workflowpkg.Snapshot {
	if a.workflow.runtime != nil {
		current := a.workflow.runtime.Snapshot()
		if a.mode != modeWorkflowPanel && a.mode != modeWorkflowSave ||
			a.workflow.snapshot.ID == "" || a.workflow.snapshot.ID == current.ID ||
			a.workflow.panelLevel == -1 {
			a.workflow.snapshot = current
		}
	}
	return a.workflow.snapshot
}

func workflowAgentsForPhase(s workflowpkg.Snapshot, phase string) []workflowpkg.AgentSnapshot {
	var out []workflowpkg.AgentSnapshot
	for _, agent := range s.Agents {
		if agent.Phase == phase {
			out = append(out, agent)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func (a *App) renderWorkflowTaskLine(width int) string {
	if !a.workflow.active && !a.workflow.paused {
		return ""
	}
	s := a.workflowSnapshot()
	line := fmt.Sprintf("workflow %s · %s · %d running · %d queued · %d done", s.Name, s.Phase, s.Running, s.Queued, s.Done)
	if s.Status == "paused" {
		line = fmt.Sprintf("workflow %s · paused · %d done · %d queued", s.Name, s.Done, s.Queued)
	}
	return a.styles.LogDim.Render(truncatePreview("↓ "+line, width))
}

func (a *App) renderWorkflowPanel(width int) string {
	if a.mode != modeWorkflowPanel && a.mode != modeWorkflowSave {
		return ""
	}
	s := a.workflowSnapshot()
	elapsed := time.Since(s.StartedAt).Round(time.Second)
	if !s.EndedAt.IsZero() {
		elapsed = s.EndedAt.Sub(s.StartedAt).Round(time.Second)
	}
	lines := []string{
		a.styles.SlashSelected.Render(fmt.Sprintf("  %s  [%s]", s.Name, s.Status)),
		a.styles.SlashDim.Render(fmt.Sprintf("  %s · tokens %s · %s", s.Description, formatFooterTokens(s.Tokens), elapsed)),
	}
	if a.workflow.panelLevel == -1 {
		lines[0] = a.styles.SlashSelected.Render("  Workflow runs")
		lines[1] = a.styles.SlashDim.Render(fmt.Sprintf("  %d persisted run(s)", len(a.workflow.runs)))
	}
	if a.workflow.panelHelp {
		lines = append(lines,
			a.styles.Text.Render("  ↑↓ select · enter/→ drill · esc back"),
			a.styles.Text.Render("  j/k scroll · p pause/resume · x stop · r restart · s save · ? help"),
		)
	} else {
		switch a.workflow.panelLevel {
		case -1:
			for index, state := range a.workflow.runs {
				prefix := "  "
				style := a.styles.Text
				if index == a.workflow.panelCursor {
					prefix = "▸ "
					style = a.styles.SlashSelected
				}
				line := fmt.Sprintf("%s%-22s %-9s %-16s %s",
					prefix, state.Meta.Name, state.Status, state.Phase, state.UpdatedAt.Format("2006-01-02 15:04"))
				lines = append(lines, style.Render(truncatePreview(line, width-4)))
			}
		case 0:
			for index, phase := range s.Phases {
				prefix := "  "
				style := a.styles.Text
				if index == a.workflow.panelCursor {
					prefix = "▸ "
					style = a.styles.SlashSelected
				}
				line := fmt.Sprintf("%s%-16s agents %d/%d · running %d · queued %d · tokens %s",
					prefix, phase.Name, phase.Done+phase.Failed, phase.Total, phase.Running, phase.Queued, formatFooterTokens(phase.Tokens))
				lines = append(lines, style.Render(truncatePreview(line, width-4)))
			}
		case 1:
			agents := workflowAgentsForPhase(s, a.workflow.panelPhase)
			lines = append(lines, a.styles.SlashDim.Render("  phase: "+a.workflow.panelPhase))
			for index, item := range agents {
				prefix := "  "
				style := a.styles.Text
				if index == a.workflow.panelCursor {
					prefix = "▸ "
					style = a.styles.SlashSelected
				}
				line := fmt.Sprintf("%s%-24s %-9s turn %d · tokens %s", prefix, item.Key, item.Status, item.Turns, formatFooterTokens(item.Tokens))
				lines = append(lines, style.Render(truncatePreview(line, width-4)))
			}
		case 2:
			item, ok := s.Agents[a.workflow.panelAgent]
			if ok {
				lines = append(lines,
					a.styles.Text.Render(fmt.Sprintf("  %s · %s · %s", item.Key, item.Role, item.Status)),
					a.styles.SlashDim.Render("  prompt:"),
				)
				detail := item.Prompt
				if len(item.LastTools) > 0 {
					detail += "\n\nrecent tools: " + strings.Join(item.LastTools, ", ")
				}
				if item.Result != "" {
					detail += "\n\nresult:\n" + item.Result
				}
				if item.JSON != nil {
					detail += fmt.Sprintf("\n\nparsed JSON:\n%v", item.JSON)
				}
				if item.Error != "" {
					detail += "\n\nerror: " + item.Error
				}
				detailLines := strings.Split(detail, "\n")
				start := a.workflow.panelScroll
				if start > len(detailLines) {
					start = len(detailLines)
				}
				end := start + 18
				if end > len(detailLines) {
					end = len(detailLines)
				}
				for _, line := range detailLines[start:end] {
					lines = append(lines, a.styles.LogDim.Render("  "+truncatePreview(line, width-4)))
				}
			}
		}
	}
	if a.mode == modeWorkflowSave {
		scope := "project"
		if !a.workflow.saveProject {
			scope = "personal"
		}
		lines = append(lines, a.styles.SlashSelected.Render("  Save workflow as /"+a.editor.Value()))
		lines = append(lines, a.styles.SlashDim.Render("  scope: "+scope+" · tab toggle · enter save · esc cancel"))
		if a.workflow.saveStatus != "" {
			lines = append(lines, a.styles.AssistError.Render("  "+a.workflow.saveStatus))
		}
	} else {
		lines = append(lines, a.styles.SlashDim.Render("  ↑↓ select · enter drill · p pause · x stop · r restart · s save · ? help · esc back"))
	}
	return a.styles.SlashMenu.Width(width - 2).Render(strings.Join(lines, "\n"))
}
