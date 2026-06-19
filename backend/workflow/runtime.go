package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"
	"github.com/enough/enough/backend/config"
	"github.com/enough/enough/backend/core"
)

var (
	exportMetaPattern = regexp.MustCompile(`\bexport\s+const\s+meta\s*=`)
	exportRunPattern  = regexp.MustCompile(`\bexport\s+(async\s+)?function\s+run\s*\(`)
	ErrQuotaPaused    = errors.New("workflow paused: provider quota exceeded")
	ErrCancelled      = errors.New("workflow cancelled")
)

type jobControl struct {
	cancel context.CancelFunc
}

type Runtime struct {
	cfg     config.Runtime
	workDir string

	mu               sync.RWMutex
	persistMu        sync.Mutex
	snapshot         Snapshot
	meta             Meta
	state            *State
	emit             func(core.Event)
	cancel           context.CancelFunc
	paused           bool
	pauseReason      string
	wake             chan struct{}
	active           map[string]*jobControl
	restartRequested map[string]bool
	completed        map[string]AgentResult
	totalAgents      int
	maxConcurrency   int
	maxTotalAgents   int
	agentRunner      func(context.Context, string, string, AgentOptions, func(core.Event)) AgentResult
	directSem        chan struct{}
}

func NewRuntime(cfg config.Runtime, workDir string) *Runtime {
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	r := &Runtime{
		cfg:              cfg,
		workDir:          workDir,
		wake:             make(chan struct{}, 1),
		active:           map[string]*jobControl{},
		restartRequested: map[string]bool{},
		completed:        map[string]AgentResult{},
		maxConcurrency:   DefaultMaxConcurrency,
		maxTotalAgents:   DefaultMaxTotalAgents,
	}
	r.agentRunner = r.defaultAgentRunner
	return r
}

func (r *Runtime) Run(ctx context.Context, scriptPath string, opts RunOptions, emit func(core.Event)) (RunResult, error) {
	abs, err := filepath.Abs(scriptPath)
	if err != nil {
		return RunResult{}, err
	}
	_, program, err := compileWorkflow(abs)
	if err != nil {
		return RunResult{}, err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	r.mu.Lock()
	r.emit = emit
	r.cancel = cancel
	r.snapshot = Snapshot{
		ID: opts.ID, ScriptPath: abs, Status: "loading",
		Agents: map[string]AgentSnapshot{}, StartedAt: time.Now(),
	}
	if r.snapshot.ID == "" {
		r.snapshot.ID = filepath.Base(filepath.Dir(abs))
	}
	r.paused = false
	r.pauseReason = ""
	r.mu.Unlock()

	vm := goja.New()
	vmDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			vm.Interrupt(ctx.Err())
		case <-vmDone:
		}
	}()
	defer close(vmDone)
	loop := newVMLoop(vm)
	defer loop.wait()
	sdk := r.sdkObject(vm, loop, ctx)
	if strings.TrimSpace(opts.Args) == "" {
		_ = vm.Set("args", goja.Undefined())
	} else {
		_ = vm.Set("args", parseArgs(opts.Args))
	}
	_ = vm.Set("sdk", sdk)
	if _, err := vm.RunProgram(program); err != nil {
		if ctx.Err() != nil {
			return r.finishError(ctx.Err())
		}
		return r.finishError(fmt.Errorf("load workflow %s: %w", abs, err))
	}
	meta, err := exportMeta(vm)
	if err != nil {
		return r.finishError(err)
	}
	if strings.TrimSpace(meta.Name) == "" || strings.TrimSpace(meta.Description) == "" {
		return r.finishError(errors.New("workflow meta requires non-empty name and description"))
	}
	r.configure(meta)

	state := &State{
		Version: 1, ID: r.snapshot.ID, ScriptPath: abs, Args: opts.Args,
		Meta: meta, Status: "running", Completed: map[string]AgentResult{},
		Agents: map[string]AgentSnapshot{}, StartedAt: time.Now(),
	}
	if !opts.Force {
		if prior, loadErr := LoadState(abs); loadErr == nil && prior.ID == state.ID {
			state = prior
			state.Status = "running"
			state.PauseReason = ""
			state.Args = opts.Args
			if state.Completed == nil {
				state.Completed = map[string]AgentResult{}
			}
		}
	}
	r.mu.Lock()
	r.state = state
	r.completed = cloneJSON(state.Completed)
	r.snapshot.ID = state.ID
	r.snapshot.Name = meta.Name
	r.snapshot.Description = meta.Description
	r.snapshot.Status = "running"
	r.snapshot.StartedAt = state.StartedAt
	r.snapshot.Agents = cloneJSON(state.Agents)
	if r.snapshot.Agents == nil {
		r.snapshot.Agents = map[string]AgentSnapshot{}
	}
	r.totalAgents = len(state.Completed)
	r.recountLocked()
	if r.snapshot.StartedAt.IsZero() {
		r.snapshot.StartedAt = time.Now()
		state.StartedAt = r.snapshot.StartedAt
	}
	r.mu.Unlock()
	r.persist()
	r.emitRun(core.EventWorkflowStart, "")

	runValue := vm.Get("__enough_run")
	runFn, ok := goja.AssertFunction(runValue)
	if !ok {
		return r.finishError(errors.New("workflow must export async function run(sdk)"))
	}
	value, callErr := runFn(goja.Undefined(), sdk)
	if callErr == nil {
		value, callErr = loop.await(ctx, value)
	}
	if callErr != nil && ctx.Err() != nil {
		callErr = ctx.Err()
	}
	if callErr != nil {
		return r.finishError(callErr)
	}

	var exported any
	if value != nil && !goja.IsUndefined(value) && !goja.IsNull(value) {
		exported = value.Export()
	}
	r.mu.Lock()
	r.snapshot.Status = "done"
	r.snapshot.EndedAt = time.Now()
	if r.state != nil {
		r.state.Status = "done"
		r.state.PauseReason = ""
	}
	r.mu.Unlock()
	r.persist()
	summary := "workflow complete"
	if data, err := json.Marshal(exported); err == nil && string(data) != "null" {
		if len(data) > 4000 {
			data = append(data[:4000], []byte("...")...)
		}
		summary = string(data)
	}
	r.emitRun(core.EventWorkflowEnd, summary)
	return RunResult{ID: state.ID, Meta: meta, Value: exported, Status: "done"}, nil
}

func (r *Runtime) finishError(err error) (RunResult, error) {
	if errors.Is(err, ErrQuotaPaused) || r.isQuotaPaused() {
		r.mu.Lock()
		r.snapshot.Status = "paused"
		r.snapshot.Message = "provider quota exceeded"
		if r.state != nil {
			r.state.Status = "paused"
			r.state.PauseReason = "provider quota exceeded"
		}
		r.mu.Unlock()
		r.persist()
		r.emitRun(core.EventWorkflowPaused, "provider quota exceeded")
		return RunResult{ID: r.snapshot.ID, Meta: r.meta, Status: "paused"}, ErrQuotaPaused
	}
	r.mu.RLock()
	status := r.snapshot.Status
	statusMessage := r.snapshot.Message
	r.mu.RUnlock()
	if status == "paused" {
		r.persist()
		r.emitRun(core.EventWorkflowPaused, statusMessage)
		return RunResult{ID: r.snapshot.ID, Meta: r.meta, Status: "paused"}, err
	}
	if errors.Is(err, context.Canceled) || status == "cancelled" {
		r.mu.Lock()
		r.snapshot.Status = "cancelled"
		r.snapshot.EndedAt = time.Now()
		if r.state != nil {
			r.state.Status = "cancelled"
		}
		r.mu.Unlock()
		r.persist()
		r.emitRun(core.EventWorkflowEnd, "workflow cancelled")
		return RunResult{ID: r.snapshot.ID, Meta: r.meta, Status: "cancelled"}, ErrCancelled
	}
	r.mu.Lock()
	r.snapshot.Status = "failed"
	r.snapshot.Message = err.Error()
	r.snapshot.EndedAt = time.Now()
	if r.state != nil {
		r.state.Status = "failed"
		r.state.PauseReason = err.Error()
	}
	r.mu.Unlock()
	r.persist()
	r.emitRun(core.EventWorkflowEnd, err.Error())
	return RunResult{ID: r.snapshot.ID, Meta: r.meta, Status: "failed"}, err
}

func (r *Runtime) configure(meta Meta) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.meta = meta
	r.maxConcurrency = DefaultMaxConcurrency
	if value, err := strconv.Atoi(strings.TrimSpace(os.Getenv("ENOUGH_WORKFLOW_MAX_CONCURRENCY"))); err == nil && value > 0 {
		r.maxConcurrency = value
	}
	if meta.MaxConcurrency > 0 && meta.MaxConcurrency < r.maxConcurrency {
		r.maxConcurrency = meta.MaxConcurrency
	}
	r.maxTotalAgents = DefaultMaxTotalAgents
	if raw := strings.TrimSpace(os.Getenv("ENOUGH_WORKFLOW_MAX_TOTAL_AGENTS")); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil && value >= 0 {
			r.maxTotalAgents = value
		}
	}
	if meta.MaxTotalAgents > 0 {
		r.maxTotalAgents = meta.MaxTotalAgents
	}
	r.directSem = make(chan struct{}, r.maxConcurrency)
	r.snapshot.Phases = make([]PhaseSnapshot, 0, len(meta.Phases))
	for _, phase := range meta.Phases {
		r.snapshot.Phases = append(r.snapshot.Phases, PhaseSnapshot{Name: phase})
	}
}

func Inspect(scriptPath string) (Meta, error) {
	abs, err := filepath.Abs(scriptPath)
	if err != nil {
		return Meta{}, err
	}
	_, program, err := compileWorkflow(abs)
	if err != nil {
		return Meta{}, err
	}
	vm := goja.New()
	done := make(chan struct{})
	go func() {
		select {
		case <-time.After(2 * time.Second):
			vm.Interrupt("workflow inspection timed out")
		case <-done:
		}
	}()
	defer close(done)
	_ = vm.Set("args", goja.Undefined())
	_ = vm.Set("sdk", vm.NewObject())
	if _, err := vm.RunProgram(program); err != nil {
		return Meta{}, fmt.Errorf("load workflow %s: %w", abs, err)
	}
	return exportMeta(vm)
}

func compileWorkflow(path string) (string, *goja.Program, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil, err
	}
	source := string(data)
	if !exportMetaPattern.MatchString(source) {
		return "", nil, errors.New("workflow must export const meta")
	}
	if !exportRunPattern.MatchString(source) {
		return "", nil, errors.New("workflow must export async function run(sdk)")
	}
	transformed := exportMetaPattern.ReplaceAllString(source, "const meta =")
	transformed = exportRunPattern.ReplaceAllStringFunc(transformed, func(match string) string {
		match = strings.TrimPrefix(match, "export ")
		return match
	})
	transformed += "\n;globalThis.__enough_meta = meta; globalThis.__enough_run = run;\n"
	program, err := goja.Compile(path, transformed, false)
	if err != nil {
		return "", nil, fmt.Errorf("parse workflow %s: %w", path, err)
	}
	return source, program, nil
}

func exportMeta(vm *goja.Runtime) (Meta, error) {
	value := vm.Get("__enough_meta")
	if value == nil || goja.IsUndefined(value) {
		return Meta{}, errors.New("workflow must export const meta")
	}
	var meta Meta
	if err := exportJSONValue(value, &meta); err != nil {
		return Meta{}, fmt.Errorf("invalid workflow meta: %w", err)
	}
	return meta, nil
}

func exportJSONValue(value goja.Value, target any) error {
	data, err := json.Marshal(value.Export())
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

func unwrapPromise(value goja.Value) (goja.Value, error) {
	if value == nil {
		return goja.Undefined(), nil
	}
	promise, ok := value.Export().(*goja.Promise)
	if !ok {
		return value, nil
	}
	switch promise.State() {
	case goja.PromiseStateFulfilled:
		return promise.Result(), nil
	case goja.PromiseStateRejected:
		return nil, fmt.Errorf("workflow rejected: %s", promise.Result().String())
	default:
		return nil, errors.New("workflow returned a pending promise; SDK asynchronous work must be awaited")
	}
}

func parseArgs(raw string) any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var value any
	if strings.HasPrefix(raw, "{") || strings.HasPrefix(raw, "[") {
		if jsonErr := json.Unmarshal([]byte(raw), &value); jsonErr == nil {
			return value
		}
	}
	return raw
}

func jsonShape(value any) any {
	data, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var shaped any
	if err := json.Unmarshal(data, &shaped); err != nil {
		return value
	}
	return shaped
}

func (r *Runtime) Snapshot() Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s := cloneJSON(r.snapshot)
	if !s.StartedAt.IsZero() && s.EndedAt.IsZero() {
		// elapsed is emitted in core events; snapshots retain absolute times.
	}
	return s
}

func (r *Runtime) Pause() {
	r.mu.Lock()
	if r.snapshot.Status == "running" {
		r.paused = true
		r.pauseReason = "user"
		r.snapshot.Status = "paused"
		r.snapshot.Message = "paused by user"
		if r.state != nil {
			r.state.Status = "paused"
			r.state.PauseReason = "user"
		}
	}
	r.mu.Unlock()
	r.persist()
	r.emitRun(core.EventWorkflowPaused, "paused by user")
	r.signal()
}

func (r *Runtime) Resume() {
	r.mu.Lock()
	r.paused = false
	r.pauseReason = ""
	if r.snapshot.Status == "paused" {
		r.snapshot.Status = "running"
		r.snapshot.Message = ""
	}
	if r.state != nil {
		r.state.Status = "running"
		r.state.PauseReason = ""
	}
	r.mu.Unlock()
	r.persist()
	r.emitRun(core.EventWorkflowPhase, "resumed")
	r.signal()
}

func (r *Runtime) Cancel() {
	r.mu.Lock()
	r.snapshot.Status = "cancelled"
	cancel := r.cancel
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	r.signal()
}

func (r *Runtime) CheckpointAndStop(reason string) {
	if strings.TrimSpace(reason) == "" {
		reason = "user"
	}
	r.mu.Lock()
	r.paused = true
	r.pauseReason = reason
	r.snapshot.Status = "paused"
	r.snapshot.Message = reason
	if r.state != nil {
		r.state.Status = "paused"
		r.state.PauseReason = reason
	}
	cancel := r.cancel
	r.mu.Unlock()
	r.persist()
	if cancel != nil {
		cancel()
	}
	r.signal()
}

func (r *Runtime) StopAgent(key string) bool {
	r.mu.Lock()
	control := r.active[key]
	r.mu.Unlock()
	if control == nil {
		return false
	}
	control.cancel()
	return true
}

func (r *Runtime) RestartAgent(key string) bool {
	r.mu.Lock()
	control := r.active[key]
	if control != nil {
		r.restartRequested[key] = true
	}
	r.mu.Unlock()
	if control == nil {
		return false
	}
	control.cancel()
	return true
}

func (r *Runtime) signal() {
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

func (r *Runtime) isQuotaPaused() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.pauseReason == "provider quota exceeded"
}

func (r *Runtime) persist() {
	r.persistMu.Lock()
	defer r.persistMu.Unlock()
	r.mu.Lock()
	if r.state == nil {
		r.mu.Unlock()
		return
	}
	r.state.Meta = r.meta
	r.state.Status = r.snapshot.Status
	r.state.Phase = r.snapshot.Phase
	r.state.Completed = cloneJSON(r.completed)
	r.state.Agents = cloneJSON(r.snapshot.Agents)
	state := cloneJSON(*r.state)
	r.mu.Unlock()
	_ = SaveState(&state)
}

func (r *Runtime) emitRun(kind, message string) {
	r.mu.RLock()
	emit := r.emit
	s := cloneJSON(r.snapshot)
	phases := append([]string(nil), r.meta.Phases...)
	r.mu.RUnlock()
	if emit == nil {
		return
	}
	emit(core.Event{Kind: kind, Data: core.WorkflowRunEvent{
		ID: s.ID, Name: s.Name, Description: s.Description, ScriptPath: s.ScriptPath,
		Status: s.Status, Phase: s.Phase, Phases: phases, Queued: s.Queued, Running: s.Running,
		Done: s.Done, Failed: s.Failed, Tokens: s.Tokens, StartedAt: s.StartedAt,
		Elapsed: time.Since(s.StartedAt), Message: message,
	}})
}
