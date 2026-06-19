package tui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/enough/enough/backend/auth"
	"github.com/enough/enough/backend/config"
	"github.com/enough/enough/backend/core"
	workflowpkg "github.com/enough/enough/backend/workflow"
)

type workflowDraft struct {
	id        string
	task      string
	path      string
	autoRun   bool
	meta      workflowpkg.Meta
	lineCount int
}

type workflowState struct {
	active      bool
	writing     bool
	paused      bool
	ultracode   bool
	draft       *workflowDraft
	runtime     *workflowpkg.Runtime
	snapshot    workflowpkg.Snapshot
	lastScript  string
	lastID      string
	notifiedEnd bool

	approvalView bool
	approvalText string

	panelLevel  int
	panelCursor int
	panelPhase  string
	panelAgent  string
	panelScroll int
	panelHelp   bool
	runs        []workflowpkg.State

	saveProject bool
	saveStatus  string
}

func (a *App) startWorkflow(arg string) {
	if a.workflow.active || a.workflow.writing {
		a.appendMessage("error", "workflow already running — /workflow-cancel first")
		return
	}
	if a.running {
		a.appendMessage("error", "wait for the foreground agent to finish")
		return
	}
	if !auth.Connected() {
		a.appendMessage("error", "not connected — type / and pick connect")
		return
	}
	task, autoRun := parseWorkflowCommand(arg)
	if task == "" {
		a.appendMessage("error", "Usage: /workflow <task> [--yes]")
		return
	}
	id := newWorkflowID()
	path := filepath.Join(a.workDir(), ".enough", "workflows", id, "workflow.js")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		a.appendMessage("error", err.Error())
		return
	}
	a.workflow.writing = true
	a.workflow.draft = &workflowDraft{id: id, task: task, path: path, autoRun: autoRun}
	a.appendUserMessage("/workflow "+task, nil)
	a.appendMessage("system", "workflow · writing orchestration script…")
	a.startAgent(workflowAuthorPrompt(task, path), nil)
}

func parseWorkflowCommand(arg string) (string, bool) {
	fields := strings.Fields(arg)
	out := make([]string, 0, len(fields))
	auto := false
	for _, field := range fields {
		if field == "--yes" {
			auto = true
			continue
		}
		out = append(out, field)
	}
	return strings.TrimSpace(strings.Join(out, " ")), auto
}

func parseWorkflowRunCommand(arg string) (path string, autoRun bool) {
	fields := strings.Fields(arg)
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if field == "--yes" {
			autoRun = true
			continue
		}
		out = append(out, field)
	}
	return strings.Join(out, " "), autoRun
}

func workflowIDFromPath(path string) string {
	dir := filepath.Base(filepath.Dir(path))
	if dir != "" && dir != "workflows" && dir != ".enough" && dir != "saved" {
		return dir
	}
	return newWorkflowID()
}

func (a *App) prepareWorkflowRun(path string, autoRun bool) error {
	meta, err := workflowpkg.Inspect(path)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	id := workflowIDFromPath(path)
	a.workflow.lastScript = path
	a.workflow.lastID = id
	a.workflow.draft = &workflowDraft{
		id:        id,
		path:      path,
		autoRun:   autoRun,
		meta:      meta,
		lineCount: strings.Count(string(data), "\n") + 1,
	}
	a.workflow.approvalText = string(data)
	a.workflow.approvalView = false
	return nil
}

func (a *App) runExistingWorkflow(arg string) {
	if a.workflow.active || a.workflow.writing {
		a.appendMessage("error", "workflow already running — /workflow-cancel first")
		return
	}
	if a.running {
		a.appendMessage("error", "wait for the foreground agent to finish")
		return
	}
	if !auth.Connected() {
		a.appendMessage("error", "not connected — type / and pick connect")
		return
	}
	pathArg, autoRun := parseWorkflowRunCommand(arg)
	path := pathArg
	var err error
	if path == "" {
		path, err = workflowpkg.FindLatestScript(a.workDir())
		if err != nil {
			a.appendMessage("error", err.Error())
			return
		}
	} else {
		path, err = workflowpkg.ResolveScriptPath(a.workDir(), path)
		if err != nil {
			a.appendMessage("error", err.Error())
			return
		}
	}
	if err := a.prepareWorkflowRun(path, autoRun); err != nil {
		a.appendMessage("error", "workflow script invalid: "+err.Error())
		a.workflow.draft = nil
		return
	}
	if autoRun || a.workflowAlwaysApproved(a.workflow.draft.meta.Name) {
		draft := a.workflow.draft
		a.workflow.draft = nil
		a.startWorkflowRuntime(draft.path, draft.id, "", false)
		a.appendMessage("system", "workflow · running "+draft.meta.Name)
		return
	}
	a.mode = modeWorkflowApproval
	a.editor.SetValue("")
	a.requestRender()
}

func workflowAuthorPrompt(task, path string) string {
	return fmt.Sprintf(`Write a complete task-specific dynamic workflow for this task:

%s

Save JavaScript only to:
%s

Do not run the workflow. The user must review the source before the runtime executes it.

Required contract:
- export const meta = { name, description, phases?, maxConcurrency? }
- export async function run(sdk) { ... }
- use sdk.runBash/fetchJSON for pre-fetch and filtering
- use sdk.pipeline(input, ...asyncStages) for staged dynamic routing
- each stage is async (ctx) => SubJob[] with ONE ctx argument only (not (items, ctx))
- ctx.input is the pipeline input; ctx.previousResults is the prior stage's agent results array; ctx.results is the cumulative map of completed subjobs by key
- subagent results use result.ok, result.text, result.json, result.key — not result.output
- sdk.pipeline returns { input, stages, results } — read final report from results["aggregate:report"].json (or your subjob key), not [0].output
- stage functions return subjob arrays with stable key, role, prompt, optional systemPrompt/tools/model/responseSchema/maxTurns/readonly
- use responseSchema for machine-routable agent output
- audit/rule/verify roles must carry different prompts and read-only policies
- later stages route from ctx.previousResults; do not send every item through every phase
- sdk.spawnAgent, sdk.pipeline, sdk.runBash, sdk.fetchJSON, sdk.log, and sdk.today are available
- no require, fs, process, or network globals exist in the JavaScript VM
- omit maxConcurrency for the default dynamic 16-agent pool unless this task specifically needs a lower ceiling
- do not add cost gates or token-saving throttles

The orchestration program is the implementation: include all clustering, prompt generation, schemas, conditions, and aggregate return value needed for this exact task.`, task, path)
}

func (a *App) finishWorkflowDraft() bool {
	if !a.workflow.writing || a.workflow.draft == nil {
		return false
	}
	draft := a.workflow.draft
	a.workflow.writing = false
	meta, err := workflowpkg.Inspect(draft.path)
	if err != nil {
		a.appendMessage("error", "workflow script invalid: "+err.Error())
		a.workflow.draft = nil
		return true
	}
	data, err := os.ReadFile(draft.path)
	if err != nil {
		a.appendMessage("error", "workflow script missing: "+err.Error())
		a.workflow.draft = nil
		return true
	}
	draft.meta = meta
	draft.lineCount = strings.Count(string(data), "\n") + 1
	a.workflow.lastScript = draft.path
	a.workflow.lastID = draft.id

	if draft.autoRun || a.workflowAlwaysApproved(meta.Name) {
		a.startWorkflowRuntime(draft.path, draft.id, "", false)
		a.workflow.draft = nil
		return true
	}
	a.workflow.approvalText = string(data)
	a.workflow.approvalView = false
	a.mode = modeWorkflowApproval
	a.editor.SetValue("")
	a.requestRender()
	return true
}

func (a *App) workflowAlwaysApproved(name string) bool {
	if workflowpkg.IsAlwaysApproved(a.workDir(), name) {
		return true
	}
	cfg, err := config.Load()
	if err != nil || cfg.Workflows == nil {
		return false
	}
	key := a.workDir() + "::" + name
	for _, approved := range cfg.Workflows.AlwaysApprove {
		if approved == key {
			return true
		}
	}
	return false
}

func (a *App) startWorkflowRuntime(path, id, args string, force bool) {
	if a.workflow.active {
		a.appendMessage("error", "workflow already running — /workflow-cancel first")
		return
	}
	cfg, err := config.LoadRuntime()
	if err != nil {
		a.appendMessage("error", err.Error())
		return
	}
	rt := workflowpkg.NewRuntime(cfg, a.workDir())
	ch := make(chan core.Event, 256)
	a.workflow.active = true
	a.workflow.paused = false
	a.workflow.runtime = rt
	a.workflowCh = ch
	a.workflow.lastScript = path
	a.workflow.lastID = id
	a.workflow.notifiedEnd = false
	a.mode = modeTask

	go func() {
		defer close(ch)
		emit := func(event core.Event) {
			ch <- event
		}
		_, runErr := rt.Run(context.Background(), path, workflowpkg.RunOptions{ID: id, Args: args, Force: force}, emit)
		if runErr != nil && rt.Snapshot().Status == "" {
			emit(core.Event{Kind: core.EventWorkflowEnd, Data: core.WorkflowRunEvent{
				ID: id, ScriptPath: path, Status: "failed", Message: runErr.Error(),
			}})
		}
	}()
}

func (a *App) handleWorkflowEvent(event core.Event) {
	if a.workflow.runtime != nil {
		a.workflow.snapshot = a.workflow.runtime.Snapshot()
	}
	switch event.Kind {
	case core.EventWorkflowPaused:
		a.workflow.paused = true
		if !a.workflow.notifiedEnd {
			message := "workflow paused"
			if ev, ok := event.Data.(core.WorkflowRunEvent); ok && ev.Message != "" {
				message += ": " + ev.Message
			}
			a.appendMessage("system", message+" — use /workflow-resume")
			a.workflow.notifiedEnd = true
		}
	case core.EventWorkflowEnd:
		if !a.workflow.notifiedEnd {
			ev, _ := event.Data.(core.WorkflowRunEvent)
			switch ev.Status {
			case "done":
				message := fmt.Sprintf("workflow %s complete · %d done · %d failed", ev.Name, ev.Done, ev.Failed)
				if ev.Message != "" && ev.Message != "workflow complete" {
					message += "\n" + ev.Message
				}
				a.appendMessage("system", message)
			case "cancelled":
				a.appendMessage("system", "workflow cancelled")
			default:
				message := ev.Message
				if message == "" {
					message = "workflow failed"
				}
				a.appendMessage("error", message)
			}
			a.workflow.notifiedEnd = true
		}
	}
	a.bumpChat()
}

func (a *App) finishWorkflowRun() {
	if a.workflow.runtime != nil {
		a.workflow.snapshot = a.workflow.runtime.Snapshot()
	}
	a.workflowCh = nil
	a.workflow.active = false
	a.workflow.paused = a.workflow.snapshot.Status == "paused"
	a.requestRender()
}

func (a *App) cancelWorkflow() {
	if a.workflow.writing {
		a.workflow.writing = false
		a.workflow.draft = nil
		a.abortAgent()
		a.appendMessage("system", "workflow authoring cancelled")
		return
	}
	if !a.workflow.active || a.workflow.runtime == nil {
		a.appendMessage("system", "no active workflow")
		return
	}
	a.workflow.runtime.Cancel()
}

func (a *App) resumeWorkflow(id string) {
	if a.workflow.active || a.workflow.writing {
		a.appendMessage("error", "workflow already running — /workflow-cancel first")
		return
	}
	fields := strings.Fields(id)
	force := false
	var runID string
	for _, field := range fields {
		if field == "--force" {
			force = true
		} else {
			runID = field
		}
	}
	path, err := workflowpkg.FindResumable(a.workDir(), runID)
	if err != nil {
		a.appendMessage("error", err.Error())
		return
	}
	state, err := workflowpkg.LoadState(path)
	if err != nil {
		a.appendMessage("error", err.Error())
		return
	}
	a.startWorkflowRuntime(path, state.ID, state.Args, force)
}

func (a *App) openWorkflowPanel() {
	a.workflow.runs = workflowpkg.ListStates(a.workDir())
	if a.workflow.snapshot.ID == "" && a.workflow.runtime == nil && len(a.workflow.runs) == 0 {
		a.appendMessage("system", "no workflow runs in this process")
		return
	}
	if a.workflow.runtime != nil {
		a.workflow.snapshot = a.workflow.runtime.Snapshot()
	}
	a.workflow.panelLevel = -1
	a.workflow.panelCursor = 0
	a.workflow.panelScroll = 0
	a.workflow.panelHelp = false
	a.mode = modeWorkflowPanel
	a.editor.SetValue("")
}

func (a *App) saveLastWorkflow(name string, project bool) {
	path := a.workflow.lastScript
	if path == "" {
		var err error
		path, err = workflowpkg.FindLatestScript(a.workDir())
		if err != nil {
			a.appendMessage("error", "no workflow script to save")
			return
		}
	}
	item, err := workflowpkg.SaveWorkflow(path, name, a.workDir(), project)
	if err != nil {
		a.appendMessage("error", err.Error())
		return
	}
	scope := "personal"
	if item.Project {
		scope = "project"
	}
	a.appendMessage("system", fmt.Sprintf("saved /%s workflow (%s)", item.Name, scope))
	a.mode = modeTask
	a.editor = NewTaskEditor()
}

func (a *App) startSavedWorkflow(saved workflowpkg.SavedWorkflow, args string) {
	id := newWorkflowID()
	dir := filepath.Join(a.workDir(), ".enough", "workflows", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		a.appendMessage("error", err.Error())
		return
	}
	data, err := os.ReadFile(saved.Path)
	if err != nil {
		a.appendMessage("error", err.Error())
		return
	}
	path := filepath.Join(dir, "workflow.js")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		a.appendMessage("error", err.Error())
		return
	}
	a.workflow.lastScript, a.workflow.lastID = path, id
	a.startWorkflowRuntime(path, id, args, false)
}

func (a *App) savedWorkflow(name string) (workflowpkg.SavedWorkflow, bool) {
	for _, saved := range workflowpkg.ScanSaved(a.workDir()) {
		if saved.Name == name {
			return saved, true
		}
	}
	return workflowpkg.SavedWorkflow{}, false
}

func (a *App) workDir() string {
	if a.session != nil && a.session.CWD() != "" {
		return a.session.CWD()
	}
	dir, _ := os.Getwd()
	return dir
}

func newWorkflowID() string {
	var data [8]byte
	if _, err := rand.Read(data[:]); err != nil {
		return fmt.Sprintf("run-%d", os.Getpid())
	}
	return hex.EncodeToString(data[:])
}

func isUltracodePrompt(text string, enabled bool) (string, bool) {
	trimmed := strings.TrimSpace(text)
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "ultracode:") {
		return strings.TrimSpace(trimmed[len("ultracode:"):]), true
	}
	if strings.HasPrefix(lower, "ultracode ") {
		return strings.TrimSpace(trimmed[len("ultracode "):]), true
	}
	if enabled && (strings.Contains(lower, "use a workflow") || strings.Contains(lower, "run a workflow") || strings.Contains(lower, "write a workflow script")) {
		return trimmed, true
	}
	return trimmed, false
}

func isWorkflowControlCommand(text string) bool {
	name, _, _ := strings.Cut(strings.TrimPrefix(strings.TrimSpace(text), "/"), " ")
	switch strings.ToLower(name) {
	case "workflow-cancel", "workflows", "workflow-resume", "workflow-run":
		return strings.HasPrefix(strings.TrimSpace(text), "/")
	default:
		return false
	}
}
