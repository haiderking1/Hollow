package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/enough/enough/backend/config"
	workflowpkg "github.com/enough/enough/backend/workflow"
)

func (a *App) handleWorkflowApprovalKey(k parsedKey) bool {
	if a.workflow.draft == nil {
		a.mode = modeTask
		return true
	}
	switch k.action {
	case keyEnter:
		a.approveWorkflow(false)
		return true
	case keyEscape:
		a.denyWorkflow()
		return true
	case keyRune:
		switch k.r {
		case 'y', 'Y':
			a.approveWorkflow(false)
		case 'a', 'A':
			a.approveWorkflow(true)
		case 'n', 'N':
			a.denyWorkflow()
		case 'v', 'V':
			a.viewWorkflowDraft()
		case 'e', 'E':
			a.editWorkflowDraft()
		default:
			return false
		}
		return true
	}
	return false
}

func (a *App) viewWorkflowDraft() {
	draft := a.workflow.draft
	if draft == nil {
		return
	}
	a.workflow.approvalView = true
	if a.term == nil {
		a.requestRender()
		return
	}
	pager := strings.TrimSpace(os.Getenv("PAGER"))
	if pager == "" {
		if _, err := exec.LookPath("less"); err == nil {
			pager = "less -R"
		} else {
			pager = "more"
		}
	}
	parts := strings.Fields(pager)
	if len(parts) == 0 {
		return
	}
	if err := a.term.RunExternal(parts[0], append(parts[1:], draft.path)...); err != nil {
		a.appendMessage("error", "pager: "+err.Error())
	}
	a.requestRenderNow()
}

func (a *App) approveWorkflow(always bool) {
	draft := a.workflow.draft
	if draft == nil {
		a.mode = modeTask
		return
	}
	if always {
		if err := workflowpkg.SetAlwaysApproved(a.workDir(), draft.meta.Name); err != nil {
			a.appendMessage("error", err.Error())
			return
		}
		cfg, err := config.Load()
		if err != nil {
			a.appendMessage("error", err.Error())
			return
		}
		if cfg.Workflows == nil {
			cfg.Workflows = &config.WorkflowSettings{}
		}
		key := a.workDir() + "::" + draft.meta.Name
		found := false
		for _, name := range cfg.Workflows.AlwaysApprove {
			found = found || name == key
		}
		if !found {
			cfg.Workflows.AlwaysApprove = append(cfg.Workflows.AlwaysApprove, key)
		}
		if err := config.Save(cfg); err != nil {
			a.appendMessage("error", err.Error())
			return
		}
	}
	a.workflow.draft = nil
	a.workflow.approvalView = false
	a.startWorkflowRuntime(draft.path, draft.id, "", false)
}

func (a *App) denyWorkflow() {
	a.workflow.draft = nil
	a.workflow.approvalView = false
	a.mode = modeTask
	a.appendMessage("system", "workflow cancelled before execution")
}

func (a *App) editWorkflowDraft() {
	draft := a.workflow.draft
	if draft == nil {
		return
	}
	editor := strings.TrimSpace(os.Getenv("EDITOR"))
	if editor == "" {
		editor = "vi"
	}
	parts := strings.Fields(editor)
	if len(parts) == 0 {
		return
	}
	if err := a.term.RunExternal(parts[0], append(parts[1:], draft.path)...); err != nil {
		a.appendMessage("error", "editor: "+err.Error())
		return
	}
	meta, err := workflowpkg.Inspect(draft.path)
	if err != nil {
		a.appendMessage("error", "workflow script invalid after edit: "+err.Error())
		return
	}
	data, err := os.ReadFile(draft.path)
	if err != nil {
		a.appendMessage("error", err.Error())
		return
	}
	draft.meta = meta
	draft.lineCount = strings.Count(string(data), "\n") + 1
	a.workflow.approvalText = string(data)
	a.requestRenderNow()
}

func (a *App) renderWorkflowApproval(width int) string {
	if a.mode != modeWorkflowApproval || a.workflow.draft == nil {
		return ""
	}
	draft := a.workflow.draft
	lines := []string{
		a.styles.SlashSelected.Render("  Review dynamic workflow"),
		a.styles.Text.Render("  " + draft.meta.Name + " — " + draft.meta.Description),
		a.styles.SlashDim.Render(fmt.Sprintf("  phases: %s · %d lines", strings.Join(draft.meta.Phases, ", "), draft.lineCount)),
		a.styles.SlashDim.Render("  " + draft.path),
	}
	if a.workflow.approvalView {
		sourceLines := strings.Split(a.workflow.approvalText, "\n")
		max := 22
		if len(sourceLines) < max {
			max = len(sourceLines)
		}
		for index := 0; index < max; index++ {
			line := fmt.Sprintf("%4d  %s", index+1, sourceLines[index])
			lines = append(lines, a.styles.LogDim.Render("  "+truncatePreview(line, width-4)))
		}
		if len(sourceLines) > max {
			lines = append(lines, a.styles.SlashDim.Render(fmt.Sprintf("  … %d more lines", len(sourceLines)-max)))
		}
	}
	lines = append(lines, a.styles.SlashDim.Render("  enter/y run once · a always · v view source · e edit · n/esc deny"))
	return a.styles.SlashMenu.Width(width - 2).Render(strings.Join(lines, "\n"))
}
