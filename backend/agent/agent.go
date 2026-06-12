package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sync"
	"time"

	"github.com/enough/enough/backend/agent/evidence"
	"github.com/enough/enough/backend/agent/obligations"
	"github.com/enough/enough/backend/config"
	"github.com/enough/enough/backend/core"
	"github.com/enough/enough/backend/memory"
	"github.com/enough/enough/backend/opencode"
	"github.com/enough/enough/backend/session"
)

// Agent holds a persistent transcript and runs the tool loop on each prompt,
// matching Flame's Agent.state.messages + prompt() pattern.
type Agent struct {
	mu sync.Mutex

	cfg     config.Runtime
	client  *opencode.Client
	workDir string
	emit    func(core.Event)
	session *session.Manager

	messages        []opencode.Message
	busy            bool
	cancel          context.CancelFunc
	userAbortCtx    context.Context
	userAbortCancel context.CancelFunc

	// swarmDepth tracks nested agent_swarm nesting (0 = main agent).
	swarmDepth int

	// ledger is the per-turn evidence ledger; reset on each user prompt.
	// Swarm workers are separate Agent values, so each holds its own fork.
	ledger *evidence.Ledger

	// obligations tracks the current turn's proof obligations (main agent
	// only; nil for swarm workers and the verifier shares the parent's).
	obligations *obligations.Registry

	// allowedTools, when non-nil, restricts this agent to the listed tools
	// (used by the verifier role). Enforced in guardTool, not by prompt.
	allowedTools map[string]bool

	// lastUserPrompt is the task text of the current turn, handed to the
	// verifier as context.
	lastUserPrompt string

	lockedGoal string

	verifyFailures           int
	parallelForksAttempted   bool
	turnCtx                  context.Context
	step                     stepTracker

	// completionRounds counts worker/verifier cycles this turn, capped by
	// cfg.Evidence.MaxCompletionRounds.
	completionRounds int

	compactionCancel          context.CancelFunc
	overflowRecoveryAttempted bool

	// memStore is the built-in persistent memory (MEMORY.md / USER.md).
	// Shared by reference with background-review forks so their writes land
	// on the parent's disk state. Nil when memory is disabled.
	memStore *memory.Store

	// cachedSystemPrompt is the session system prompt, built once per
	// session and replayed verbatim every turn (prefix-cache invariant).
	// Invalidated only on /new, session switch, compaction, or explicit
	// invalidation — never by mid-session memory writes.
	cachedSystemPrompt string

	// notify delivers out-of-band system lines to the frontend (background
	// review and curator summaries). Unlike emit, it stays valid after the
	// turn's event channel closes.
	notify func(string)

	// approvalPrompt opens the TUI write-approval overlay when a tool stages
	// a pending skill or memory write (Hermes approval.request parity).
	approvalPrompt func(subsystem, pendingID string)

	// writeOrigin distinguishes foreground (user-directed) tool writes from
	// background-review writes. Only background-review skill creates are
	// marked agent-created (curator-eligible).
	writeOrigin string

	// Nudge counters for the background self-improvement review.
	userTurnCount    int
	turnsSinceMemory int
	itersSinceSkill  int

	// maxIterations caps model calls per turn when > 0 (used by review and
	// curator forks; the main agent runs unbounded).
	maxIterations int

	// reviewWG tracks in-flight background review goroutines (tests and
	// shutdown can wait on it).
	reviewWG sync.WaitGroup
}

func New(cfg config.Runtime, workDir string, sm *session.Manager) *Agent {
	if workDir == "" {
		workDir, _ = os.Getwd()
	}

	a := &Agent{
		cfg:         cfg,
		client:      opencode.NewClientForRuntime(cfg),
		workDir:     workDir,
		session:     sm,
		writeOrigin: WriteOriginForeground,
	}
	a.initMemoryStore()

	// Resumed sessions replay their stored system prompt verbatim so the
	// upstream prefix cache stays warm; fresh sessions build a new one.
	if sm != nil {
		if stored := sm.StoredSystemPrompt(); stored != "" {
			a.cachedSystemPrompt = stored
		}
	}

	a.messages = []opencode.Message{
		{Role: "system", Content: opencode.StringContent(a.systemPrompt())},
	}

	if sm != nil {
		a.messages = append(a.messages, opencode.RepairToolMessages(sm.Messages())...)
	}

	return a
}

// initMemoryStore creates and loads the memory store (frozen snapshot) when
// the memory stack is enabled.
func (a *Agent) initMemoryStore() {
	if !a.cfg.Memory.Enabled && !a.cfg.Memory.UserProfileEnabled {
		a.memStore = nil
		return
	}
	a.memStore = memory.NewStore(a.cfg.Memory.MemoryCharLimit, a.cfg.Memory.UserCharLimit)
	a.memStore.LoadFromDisk()
}

type userAbortContextKey struct{}

func withUserAbortContext(ctx context.Context, done <-chan struct{}) context.Context {
	if done == nil {
		return ctx
	}
	return context.WithValue(ctx, userAbortContextKey{}, done)
}

func userAbortDone(ctx context.Context) <-chan struct{} {
	if done, ok := ctx.Value(userAbortContextKey{}).(<-chan struct{}); ok && done != nil {
		return done
	}
	return ctx.Done()
}

func userAbortFired(ctx context.Context) bool {
	select {
	case <-userAbortDone(ctx):
		return true
	default:
		return false
	}
}

func (a *Agent) Session() *session.Manager {
	return a.session
}

// LoadSession switches the agent transcript to a different persisted session.
func (a *Agent) LoadSession(sm *session.Manager) {
	a.mu.Lock()
	defer a.mu.Unlock()
	sessionChanged := a.session == nil || sm == nil || a.session.SessionID() != sm.SessionID()
	a.session = sm
	if sessionChanged {
		a.invalidateSystemPrompt()
		a.userTurnCount = 0
		a.turnsSinceMemory = 0
		a.itersSinceSkill = 0
		if sm != nil {
			if stored := sm.StoredSystemPrompt(); stored != "" {
				a.cachedSystemPrompt = stored
			}
		}
	}
	a.messages = []opencode.Message{
		{Role: "system", Content: opencode.StringContent(a.systemPrompt())},
	}
	a.messages = append(a.messages, opencode.RepairToolMessages(sm.Messages())...)
}

func (a *Agent) Reset() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// New session boundary: fresh memory snapshot, fresh prompt.
	a.invalidateSystemPrompt()
	a.userTurnCount = 0
	a.turnsSinceMemory = 0
	a.itersSinceSkill = 0

	var err error
	if a.session != nil {
		err = a.session.NewSession()
	}

	a.messages = []opencode.Message{
		{Role: "system", Content: opencode.StringContent(a.systemPrompt())},
	}
	return err
}

func (a *Agent) persist(msg opencode.Message) {
	if a.session == nil || msg.Role == "system" {
		return
	}
	_ = a.session.AppendMessage(msg)
}

func (a *Agent) Abort() {
	a.mu.Lock()
	cancel := a.cancel
	compactionCancel := a.compactionCancel
	userAbortCancel := a.userAbortCancel
	a.mu.Unlock()
	if userAbortCancel != nil {
		userAbortCancel()
	}
	if cancel != nil {
		cancel()
	}
	if compactionCancel != nil {
		compactionCancel()
	}
}

func (a *Agent) AbortCompaction() {
	a.mu.Lock()
	compactionCancel := a.compactionCancel
	a.mu.Unlock()
	if compactionCancel != nil {
		compactionCancel()
	}
}

func (a *Agent) AbortAndWait() {
	a.mu.Lock()
	cancel := a.cancel
	compactionCancel := a.compactionCancel
	userAbortCancel := a.userAbortCancel
	a.mu.Unlock()
	if userAbortCancel != nil {
		userAbortCancel()
	}
	if cancel != nil {
		cancel()
	}
	if compactionCancel != nil {
		compactionCancel()
	}

	for {
		a.mu.Lock()
		busy := a.busy
		a.mu.Unlock()
		if !busy {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (a *Agent) UpdateConfig(cfg config.Runtime) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.applyConfigLocked(cfg)
}

// applyConfigLocked swaps the runtime config. A SOUL/MEMORY/skills policy
// change is a prompt-affecting boundary, so the cached system prompt is
// invalidated; routine changes (model, thinking level) leave it untouched.
func (a *Agent) applyConfigLocked(cfg config.Runtime) {
	promptAffecting := !reflect.DeepEqual(a.cfg.Memory, cfg.Memory) ||
		!reflect.DeepEqual(a.cfg.Skills, cfg.Skills)
	a.cfg = cfg
	a.client = opencode.NewClientForRuntime(cfg)
	if promptAffecting {
		a.initMemoryStore()
		a.invalidateSystemPrompt()
	}
}

func (a *Agent) SetEmit(emit func(core.Event)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.emit = emit
}

// SetNotify installs the out-of-band system-line callback used by background
// review and curator summaries. Must be safe to call from any goroutine and
// must outlive individual turns.
func (a *Agent) SetNotify(notify func(string)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.notify = notify
}

// SetApprovalPrompt installs the callback that opens the inline write-approval
// overlay when a tool result is staged for user review.
func (a *Agent) SetApprovalPrompt(fn func(subsystem, pendingID string)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.approvalPrompt = fn
}

// MemoryStore exposes the built-in memory store (nil when disabled).
func (a *Agent) MemoryStore() *memory.Store {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.memStore
}

// Prompt appends a user message and runs the agent loop until the model stops
// calling tools or the context is cancelled.
func (a *Agent) Prompt(ctx context.Context, cfg config.Runtime, userText string, emit func(core.Event)) error {
	a.mu.Lock()
	if a.busy {
		a.mu.Unlock()
		return fmt.Errorf("agent is already processing")
	}
	a.busy = true
	ctx, cancel := context.WithCancel(ctx)
	userAbortCtx, userAbortCancel := context.WithCancel(context.Background())
	ctx = withUserAbortContext(ctx, userAbortCtx.Done())
	a.cancel = cancel
	a.userAbortCtx = userAbortCtx
	a.userAbortCancel = userAbortCancel
	a.applyConfigLocked(cfg)
	if emit != nil {
		a.emit = emit
	}

	// Hydrate per-session nudge counters from persisted history on resume,
	// then track this user turn for the memory review trigger.
	a.hydrateNudgeCountersLocked()
	a.userTurnCount++
	shouldReviewMemory := false
	if a.cfg.Memory.NudgeInterval > 0 && a.memStore != nil && a.cfg.Memory.Enabled {
		a.turnsSinceMemory++
		if a.turnsSinceMemory >= a.cfg.Memory.NudgeInterval {
			shouldReviewMemory = true
			a.turnsSinceMemory = 0
		}
	}
	if a.cfg.Memory.Enabled && a.cfg.Memory.UserProfileEnabled && a.memStore != nil &&
		userMessageSignalsProfileCorrection(userText) {
		shouldReviewMemory = true
	}
	a.mu.Unlock()

	a.overflowRecoveryAttempted = false
	turnID := fmt.Sprintf("turn_%d", time.Now().UnixNano())
	a.resetEvidenceLedger(turnID)

	a.mu.Lock()
	a.lastUserPrompt = userText
	a.lockedGoal = userText
	a.verifyFailures = 0
	a.parallelForksAttempted = false
	a.step = stepTracker{}
	a.completionRounds = 0
	if a.cfg.Evidence.Enabled {
		verifyCmd := obligations.DetectVerifyCommand(a.workDir)
		taskVerify := obligations.ExtractTaskVerifyCommands(userText)
		a.obligations = obligations.NewRegistry(turnID, verifyCmd, taskVerify,
			a.cfg.Evidence.StrictVerifyReset, a.cfg.Evidence.VerifierEnabled)
	} else {
		a.obligations = nil
	}
	a.mu.Unlock()

	// Session continuity: silently restore read credit for files this agent
	// authored in prior turns, iff their on-disk content is unchanged. The
	// guard stays hostile to everything else.
	if a.cfg.Evidence.Enabled && a.cfg.Evidence.ContinuityEnabled() && a.session != nil {
		evidence.SeedContinuityReads(a.evidenceLedger(), sessionFingerprints(a.session))
	}

	// Pre-prompt compaction check
	if a.session != nil && a.cfg.Compaction.Enabled {
		pathEntries := a.session.GetBranch(a.session.LeafID())
		compactionEntry := session.GetLatestCompactionEntry(pathEntries)

		// Find the last assistant message with usage in history
		var lastUsageEntry *session.FileEntry
		for i := len(pathEntries) - 1; i >= 0; i-- {
			if pathEntries[i].Type == session.TypeMessage && pathEntries[i].Message != nil && pathEntries[i].Message.Role == "assistant" && pathEntries[i].Message.Usage != nil {
				lastUsageEntry = &pathEntries[i]
				break
			}
		}

		skip := false
		if compactionEntry != nil && lastUsageEntry != nil {
			tUsage, _ := time.Parse(time.RFC3339Nano, lastUsageEntry.Timestamp)
			tComp, _ := time.Parse(time.RFC3339Nano, compactionEntry.Timestamp)
			if tUsage.Before(tComp) || tUsage.Equal(tComp) {
				skip = true
			}
		}

		if !skip {
			contextWindow := ModelContextWindow(a.cfg.Provider, a.cfg.Model, a.cfg.Compaction.ContextWindow)
			sessionMsgs := a.session.BuildSessionContext().Messages
			tokens := session.EstimateContextTokens(sessionMsgs).Tokens
			if session.ShouldCompact(tokens, contextWindow, a.cfg.Compaction) {
				_, _ = a.RunAutoCompaction(ctx, "threshold", false)
			}
		}
	}

	userMsg := opencode.Message{
		Role:    "user",
		Content: opencode.StringContent(userText),
	}
	a.mu.Lock()
	a.messages = append(a.messages, userMsg)
	a.mu.Unlock()

	a.persist(userMsg)

	if a.cfg.Evidence.Enabled && a.cfg.Evidence.GoalLockEnabled() {
		lockMsg := opencode.Message{
			Role:    "user",
			Content: opencode.StringContent(goalLockNotice(userText)),
		}
		a.mu.Lock()
		a.messages = append(a.messages, lockMsg)
		a.mu.Unlock()
		a.persist(lockMsg)
	}

	defer func() {
		cancel()
		a.mu.Lock()
		a.busy = false
		a.cancel = nil
		a.userAbortCtx = nil
		a.userAbortCancel = nil
		a.mu.Unlock()
	}()

	err := a.runLoop(ctx)

	// Background self-improvement review — runs AFTER the response is
	// delivered so it never competes with the user's turn. Skipped on error
	// or interrupt.
	if err == nil && ctx.Err() == nil && !userAbortFired(ctx) {
		a.maybeSpawnBackgroundReview(shouldReviewMemory)
	}

	return err
}

// hydrateNudgeCountersLocked restores per-session nudge counters from
// persisted history when this agent resumes a session it hasn't counted yet.
// Caller holds a.mu.
func (a *Agent) hydrateNudgeCountersLocked() {
	if a.userTurnCount != 0 || a.session == nil {
		return
	}
	priorUserTurns := 0
	for _, msg := range a.session.Messages() {
		if msg.Role == "user" {
			priorUserTurns++
		}
	}
	if priorUserTurns == 0 {
		return
	}
	a.userTurnCount = priorUserTurns
	if a.cfg.Memory.NudgeInterval > 0 && a.turnsSinceMemory == 0 {
		a.turnsSinceMemory = priorUserTurns % a.cfg.Memory.NudgeInterval
	}
}

func (a *Agent) runLoop(ctx context.Context) error {
	a.mu.Lock()
	a.turnCtx = ctx
	a.mu.Unlock()

	tools := a.toolMenu()

	iterations := 0
	for {
		// Iteration budget (review/curator forks only; 0 = unbounded).
		iterations++
		if a.maxIterations > 0 && iterations > a.maxIterations {
			return nil
		}

		if err := ctx.Err(); err != nil {
			if errors.Is(err, context.Canceled) {
				a.interrupted()
				return nil
			}
			return err
		}

		a.mu.Lock()
		var messages []opencode.Message
		if a.session != nil {
			sessionMsgs := a.session.BuildSessionContext().Messages
			llmMsgs := session.ConvertToLlm(sessionMsgs)
			repaired := opencode.RepairToolMessages(llmMsgs)
			messages = append([]opencode.Message{{Role: "system", Content: opencode.StringContent(a.systemPrompt())}}, repaired...)
		} else {
			messages = append([]opencode.Message(nil), a.messages...)
			if len(messages) > 0 && messages[0].Role == "system" {
				messages[0].Content = opencode.StringContent(a.systemPrompt())
			}
		}
		cfg := a.cfg
		a.mu.Unlock()

		streamStarted := false
		startStream := func() {
			if streamStarted {
				return
			}
			streamStarted = true
			a.streamStart()
		}

		req := opencode.ChatRequest{
			Model:    cfg.Model,
			Messages: messages,
			Tools:    tools,
		}
		opencode.ApplyThinkingToRequest(&req, opencode.ParseThinkingLevel(cfg.ThinkingLevel), cfg.Model)

		resp, err := a.client.ChatStream(ctx, req, opencode.StreamCallbacks{
			OnThinking: func(delta string) {
				startStream()
				a.thinkingDelta(delta)
			},
			OnText: func(delta string) {
				startStream()
				a.streamDelta(delta)
			},
		})
		if err != nil {
			if errors.Is(err, context.Canceled) {
				a.interrupted()
				return nil
			}
			// Check for context overflow
			if a.session != nil && IsContextOverflowError(err) {
				if !a.overflowRecoveryAttempted {
					a.overflowRecoveryAttempted = true

					a.mu.Lock()
					if len(a.messages) > 0 && a.messages[len(a.messages)-1].Role == "assistant" {
						a.messages = a.messages[:len(a.messages)-1]
					}
					a.mu.Unlock()

					compacted, compErr := a.RunAutoCompaction(ctx, "overflow", true)
					if compErr == nil && compacted {
						continue
					}
				} else {
					a.emitCompactionEnd("overflow", nil, false, false, "Context overflow recovery failed after one compact-and-retry attempt. Try reducing context or switching to a larger-context model.")
				}
			}

			a.err(err.Error())
			return err
		}

		msg := resp

		if len(msg.ToolCalls) == 0 {
			text := opencode.ContentString(msg)
			if !streamStarted {
				if msg.ReasoningContent != nil && *msg.ReasoningContent != "" {
					a.streamStart()
					a.thinkingDelta(*msg.ReasoningContent)
					streamStarted = true
				}
				if text != "" {
					startStream()
					a.streamDelta(text)
				}
			}
			a.mu.Lock()
			a.messages = append(a.messages, msg)
			a.mu.Unlock()
			a.persist(msg)

			// Completion contract: a text-only response does not end the turn
			// while proof obligations are open. enforceCompletion runs the
			// verifier and, if obligations remain, injects the fixed
			// turn-incomplete notice and sends the loop around again.
			if a.enforceCompletion(ctx) {
				continue
			}

			// Perform post-turn compaction check
			if a.session != nil && a.cfg.Compaction.Enabled {
				pathEntries := a.session.GetBranch(a.session.LeafID())
				compactionEntry := session.GetLatestCompactionEntry(pathEntries)

				var lastAsst *session.FileEntry
				for i := len(pathEntries) - 1; i >= 0; i-- {
					if pathEntries[i].Type == session.TypeMessage && pathEntries[i].Message != nil && pathEntries[i].Message.Role == "assistant" {
						lastAsst = &pathEntries[i]
						break
					}
				}

				skip := false
				if lastAsst != nil && compactionEntry != nil {
					tAsst, _ := time.Parse(time.RFC3339Nano, lastAsst.Timestamp)
					tComp, _ := time.Parse(time.RFC3339Nano, compactionEntry.Timestamp)
					if tAsst.Before(tComp) || tAsst.Equal(tComp) {
						skip = true
					}
				}

				if !skip {
					contextWindow := ModelContextWindow(a.cfg.Provider, a.cfg.Model, a.cfg.Compaction.ContextWindow)
					var tokens int
					if msg.Usage != nil {
						tokens = session.CalculateContextTokens(*msg.Usage)
					} else {
						var lastUsageEntry *session.FileEntry
						for i := len(pathEntries) - 1; i >= 0; i-- {
							if pathEntries[i].Type == session.TypeMessage && pathEntries[i].Message != nil && pathEntries[i].Message.Role == "assistant" && pathEntries[i].Message.Usage != nil {
								lastUsageEntry = &pathEntries[i]
								break
							}
						}

						usageSkip := false
						if compactionEntry != nil && lastUsageEntry != nil {
							tUsage, _ := time.Parse(time.RFC3339Nano, lastUsageEntry.Timestamp)
							tComp, _ := time.Parse(time.RFC3339Nano, compactionEntry.Timestamp)
							if tUsage.Before(tComp) || tUsage.Equal(tComp) {
								usageSkip = true
							}
						}

						if !usageSkip {
							sessionMsgs := a.session.BuildSessionContext().Messages
							tokens = session.EstimateContextTokens(sessionMsgs).Tokens
						} else {
							skip = true
						}
					}

					if !skip && session.ShouldCompact(tokens, contextWindow, a.cfg.Compaction) {
						_, _ = a.RunAutoCompaction(ctx, "threshold", false)
					}
				}
			}

			return nil
		}

		a.mu.Lock()
		a.messages = append(a.messages, msg)
		// Tool iteration — feeds the background skill-review nudge.
		if a.cfg.Memory.SkillNudgeInterval > 0 {
			a.itersSinceSkill++
		}
		a.mu.Unlock()
		a.persist(msg)

		for idx, call := range msg.ToolCalls {
			if err := ctx.Err(); err != nil {
				a.appendToolStubs(msg.ToolCalls[idx:], "Interrupted")
				a.interrupted()
				return nil
			}

			id := call.ID
			if id == "" {
				id = fmt.Sprintf("call_%d", idx)
				call.ID = id
			}

			a.toolStart(id, call.Function.Name, call.Function.Arguments)

			result := a.executeTool(ctx, id, call.Function.Name, call.Function.Arguments)
			a.toolResult(id, result.output, result.isErr)
			a.notifyStagedWrite(result.output)
			if call.Function.Name == memory.ToolName {
				a.notifyDirectMemoryWrite(call.Function.Arguments, result.output)
			}

			toolMsg := opencode.Message{
				Role:       "tool",
				ToolCallID: id,
				Name:       call.Function.Name,
				Content:    opencode.StringContent(result.output),
			}

			a.mu.Lock()
			a.messages = append(a.messages, toolMsg)
			a.mu.Unlock()
			a.persist(toolMsg)
		}
	}
}

func (a *Agent) streamStart() {
	if a.emit != nil {
		a.emit(core.Event{Kind: core.EventAssistantStart})
	}
}

func (a *Agent) streamDelta(text string) {
	if a.emit != nil && text != "" {
		a.emit(core.Event{Kind: core.EventAssistantDelta, Data: text})
	}
}

func (a *Agent) thinkingDelta(text string) {
	if a.emit != nil && text != "" {
		a.emit(core.Event{Kind: core.EventAssistantThinkingDelta, Data: text})
	}
}

func (a *Agent) toolStart(id, name, args string) {
	if a.emit != nil {
		a.emit(core.Event{
			Kind: core.EventToolStart,
			Data: core.ToolCallEvent{ID: id, Name: name, Args: args},
		})
	}
}

func (a *Agent) appendToolStubs(calls []opencode.ToolCall, text string) {
	for idx, call := range calls {
		id := call.ID
		if id == "" {
			id = fmt.Sprintf("call_%d", idx)
		}
		toolMsg := opencode.Message{
			Role:       "tool",
			ToolCallID: id,
			Name:       call.Function.Name,
			Content:    opencode.StringContent(text),
		}
		a.mu.Lock()
		a.messages = append(a.messages, toolMsg)
		a.mu.Unlock()
		a.persist(toolMsg)
	}
}

// toolDelta streams a chunk of incremental tool output (e.g. live bash stdout)
// to the frontend so long-running tools show progress instead of only a final
// result.
func (a *Agent) toolDelta(id, chunk string) {
	if a.emit != nil && chunk != "" {
		a.emit(core.Event{
			Kind: core.EventToolDelta,
			Data: core.ToolCallEvent{ID: id, Result: chunk},
		})
	}
}

func (a *Agent) toolResult(id, result string, isErr bool) {
	if a.emit != nil {
		a.emit(core.Event{
			Kind: core.EventToolResult,
			Data: core.ToolCallEvent{ID: id, Result: result, Error: isErr},
		})
	}
}

func (a *Agent) interrupted() {
	if a.emit != nil {
		a.emit(core.Event{Kind: core.EventSystem, Data: "interrupted"})
	}
}

func (a *Agent) err(text string) {
	if a.emit != nil {
		a.emit(core.Event{Kind: core.EventError, Data: text})
	}
}

// systemPrompt returns the session-cached system prompt, building and
// persisting it on first use. Callers may hold a.mu — no locking here.
func (a *Agent) systemPrompt() string {
	if a.cachedSystemPrompt == "" {
		a.cachedSystemPrompt = a.buildSessionPrompt()
		a.persistSystemPrompt()
	}
	return a.cachedSystemPrompt
}

func (a *Agent) buildSessionPrompt() string {
	tools := a.toolMenu()
	var toolNames []string
	for _, t := range tools {
		toolNames = append(toolNames, t.Function.Name)
	}
	sessionID := ""
	if a.session != nil {
		sessionID = a.session.SessionID()
	}
	return BuildSessionSystemPrompt(SystemPromptInputs{
		WorkDir:               a.workDir,
		Cfg:                   a.cfg,
		ToolNames:             toolNames,
		Store:                 a.memStore,
		SessionID:             sessionID,
		PreloadedSkillsPrompt: a.cfg.PreloadedSkillsPrompt,
	})
}

func (a *Agent) persistSystemPrompt() {
	if a.session != nil && a.cachedSystemPrompt != "" {
		_ = a.session.SetSystemPrompt(a.cachedSystemPrompt)
	}
}

// invalidateSystemPrompt forces a rebuild on next use and reloads memory from
// disk so the rebuilt prompt captures this session's writes (fresh frozen
// snapshot). Called at session boundaries only: /new, session switch,
// compaction. Caller may hold a.mu.
func (a *Agent) invalidateSystemPrompt() {
	a.cachedSystemPrompt = ""
	if a.memStore != nil {
		a.memStore.LoadFromDisk()
	}
}

// InvalidateSystemPrompt is the exported, self-locking variant.
func (a *Agent) InvalidateSystemPrompt() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.invalidateSystemPrompt()
}

func (a *Agent) Cfg() config.Runtime {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfg
}

func (a *Agent) WorkDir() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.workDir
}
