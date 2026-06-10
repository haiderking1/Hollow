package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/enough/enough/backend/config"
	"github.com/enough/enough/backend/core"
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

	messages []opencode.Message
	busy     bool
	cancel   context.CancelFunc

	compactionCancel          context.CancelFunc
	overflowRecoveryAttempted bool
}

func New(cfg config.Runtime, workDir string, sm *session.Manager) *Agent {
	if workDir == "" {
		workDir, _ = os.Getwd()
	}

	a := &Agent{
		cfg:     cfg,
		client:  opencode.NewClient(cfg.Endpoint, cfg.APIKey, cfg.Model),
		workDir: workDir,
		session: sm,
		messages: []opencode.Message{
			{Role: "system", Content: opencode.StringContent(systemPrompt)},
		},
	}

	if sm != nil {
		a.messages = append(a.messages, opencode.RepairToolMessages(sm.Messages())...)
	}

	return a
}

func (a *Agent) Session() *session.Manager {
	return a.session
}

// LoadSession switches the agent transcript to a different persisted session.
func (a *Agent) LoadSession(sm *session.Manager) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.session = sm
	a.messages = []opencode.Message{
		{Role: "system", Content: opencode.StringContent(systemPrompt)},
	}
	a.messages = append(a.messages, opencode.RepairToolMessages(sm.Messages())...)
}

func (a *Agent) Reset() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.messages = []opencode.Message{
		{Role: "system", Content: opencode.StringContent(systemPrompt)},
	}

	if a.session != nil {
		return a.session.NewSession()
	}
	return nil
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
	a.mu.Unlock()
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
	a.mu.Unlock()
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
	a.cfg = cfg
	a.client = opencode.NewClient(cfg.Endpoint, cfg.APIKey, cfg.Model)
}

func (a *Agent) SetEmit(emit func(core.Event)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.emit = emit
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
	a.cancel = cancel
	a.cfg = cfg
	a.client = opencode.NewClient(cfg.Endpoint, cfg.APIKey, cfg.Model)
	if emit != nil {
		a.emit = emit
	}
	a.mu.Unlock()

	a.overflowRecoveryAttempted = false

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
			contextWindow := ModelContextWindow(a.cfg.Model, a.cfg.Compaction.ContextWindow)
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

	defer func() {
		cancel()
		a.mu.Lock()
		a.busy = false
		a.cancel = nil
		a.mu.Unlock()
	}()

	return a.runLoop(ctx)
}

func (a *Agent) runLoop(ctx context.Context) error {
	tools := nativeTools()

	for {
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
			messages = append([]opencode.Message{{Role: "system", Content: opencode.StringContent(systemPrompt)}}, repaired...)
		} else {
			messages = append([]opencode.Message(nil), a.messages...)
		}
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
			Model:    a.cfg.Model,
			Messages: messages,
			Tools:    tools,
		}
		opencode.ApplyThinkingToRequest(&req, opencode.ParseThinkingLevel(a.cfg.ThinkingLevel), a.cfg.Model)

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
					contextWindow := ModelContextWindow(a.cfg.Model, a.cfg.Compaction.ContextWindow)
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

			result := a.executeTool(call.Function.Name, call.Function.Arguments)
			a.toolResult(id, result.output, result.isErr)

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

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
