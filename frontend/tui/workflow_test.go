package tui

import (
	"path/filepath"
	"strings"
	"testing"

	workflowpkg "github.com/enough/enough/backend/workflow"
)

func TestParseWorkflowRunCommand(t *testing.T) {
	path, yes := parseWorkflowRunCommand(".enough/workflows/abc/workflow.js --yes")
	if path != ".enough/workflows/abc/workflow.js" || !yes {
		t.Fatalf("path=%q yes=%v", path, yes)
	}
	path, yes = parseWorkflowRunCommand("--yes")
	if path != "" || !yes {
		t.Fatalf("path=%q yes=%v", path, yes)
	}
}

func TestParseWorkflowCommand(t *testing.T) {
	task, yes := parseWorkflowCommand("audit all PRs --yes")
	if task != "audit all PRs" || !yes {
		t.Fatalf("task=%q yes=%v", task, yes)
	}
	task, yes = parseWorkflowCommand("--yes migrate every package")
	if task != "migrate every package" || !yes {
		t.Fatalf("task=%q yes=%v", task, yes)
	}
}

func TestUltracodePromptDetection(t *testing.T) {
	for _, input := range []string{"ultracode: audit PRs", "ultracode audit PRs"} {
		task, ok := isUltracodePrompt(input, false)
		if !ok || task != "audit PRs" {
			t.Fatalf("%q => %q %v", input, task, ok)
		}
	}
	if _, ok := isUltracodePrompt("please use a workflow for this", true); !ok {
		t.Fatal("effort ultracode should detect workflow language")
	}
	if _, ok := isUltracodePrompt("ordinary request", true); ok {
		t.Fatal("ordinary request should remain a normal turn")
	}
}

func TestWorkflowApprovalViewAndDeny(t *testing.T) {
	app := &App{styles: NewStyles(), editor: NewTaskEditor(), mode: modeWorkflowApproval}
	app.workflow.draft = &workflowDraft{
		id: "id", path: filepath.Join(t.TempDir(), "workflow.js"),
		meta: workflowpkg.Meta{Name: "audit", Description: "test", Phases: []string{"audit"}},
	}
	app.workflow.approvalText = "line one\nline two"
	app.handleWorkflowApprovalKey(parsedKey{action: keyRune, r: 'v'})
	if !app.workflow.approvalView {
		t.Fatal("v should show source")
	}
	if rendered := app.renderWorkflowApproval(100); !strings.Contains(rendered, "line one") {
		t.Fatalf("rendered approval missing source: %s", rendered)
	}
	app.handleWorkflowApprovalKey(parsedKey{action: keyRune, r: 'n'})
	if app.mode != modeTask || app.workflow.draft != nil {
		t.Fatal("deny should clear draft and return to task mode")
	}
}

func TestWorkflowPanelNavigation(t *testing.T) {
	app := &App{styles: NewStyles(), editor: NewTaskEditor(), mode: modeWorkflowPanel}
	app.workflow.snapshot = workflowpkg.Snapshot{
		ID: "run", Name: "audit", Status: "running",
		Phases: []workflowpkg.PhaseSnapshot{{Name: "audit", Total: 2}},
		Agents: map[string]workflowpkg.AgentSnapshot{
			"audit:1": {Key: "audit:1", Phase: "audit", Status: "running", Prompt: "one"},
			"audit:2": {Key: "audit:2", Phase: "audit", Status: "queued", Prompt: "two"},
		},
	}
	app.drillWorkflowPanel()
	if app.workflow.panelLevel != 1 || app.workflow.panelPhase != "audit" {
		t.Fatalf("phase drill failed: %+v", app.workflow)
	}
	app.drillWorkflowPanel()
	if app.workflow.panelLevel != 2 || app.workflow.panelAgent == "" {
		t.Fatalf("agent drill failed: %+v", app.workflow)
	}
	app.handleWorkflowPanelKey(parsedKey{action: keyEscape})
	if app.workflow.panelLevel != 1 {
		t.Fatal("escape should go back one level")
	}
}

func TestComposerRemainsEditableDuringWorkflow(t *testing.T) {
	app := &App{
		styles: NewStyles(), editor: NewTaskEditor(), mode: modeTask,
		workflow: workflowState{active: true},
	}
	app.handleKey(parsedKey{action: keyRune, r: 'x'})
	if app.editor.Value() != "x" {
		t.Fatalf("editor = %q", app.editor.Value())
	}
}

func TestWorkflowAuthorPromptRequiresFullRuntimeShape(t *testing.T) {
	prompt := workflowAuthorPrompt("audit", "/tmp/workflow.js")
	for _, required := range []string{"sdk.pipeline", "responseSchema", "dynamic routing", "Do not run", "16-agent"} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("author prompt missing %q", required)
		}
	}
}
