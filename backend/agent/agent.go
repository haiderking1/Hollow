package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/enough/enough/backend/config"
	"github.com/enough/enough/backend/core"
	"github.com/enough/enough/backend/opencode"
	"github.com/enough/enough/backend/session"
)

const maxRounds = 32

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
		a.messages = append(a.messages, sm.Messages()...)
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
	a.messages = append(a.messages, sm.Messages()...)
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
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// Prompt appends a user message and runs the agent loop until the model stops
// calling tools or maxRounds is hit.
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

	userMsg := opencode.Message{
		Role:    "user",
		Content: opencode.StringContent(userText),
	}
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

	for round := 0; round < maxRounds; round++ {
		if err := ctx.Err(); err != nil {
			if errors.Is(err, context.Canceled) {
				a.interrupted()
				return nil
			}
			return err
		}

		a.mu.Lock()
		messages := append([]opencode.Message(nil), a.messages...)
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
			return nil
		}

		a.mu.Lock()
		a.messages = append(a.messages, msg)
		a.mu.Unlock()
		a.persist(msg)

		for _, call := range msg.ToolCalls {
			a.toolActivity(call.Function.Name, call.Function.Arguments)

			result := a.executeTool(call.Function.Name, call.Function.Arguments)
			if result.isErr {
				a.toolActivity(call.Function.Name, truncate(result.output, 200))
			}

			toolMsg := opencode.Message{
				Role:       "tool",
				ToolCallID: call.ID,
				Name:       call.Function.Name,
				Content:    opencode.StringContent(result.output),
			}

			a.mu.Lock()
			a.messages = append(a.messages, toolMsg)
			a.mu.Unlock()
			a.persist(toolMsg)
		}
	}

	err := fmt.Errorf("agent stopped after %d tool rounds", maxRounds)
	a.err(err.Error())
	return err
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

func (a *Agent) toolActivity(name, args string) {
	if a.emit != nil {
		a.emit(core.Event{
			Kind: core.EventToolActivity,
			Data: fmt.Sprintf("%s(%s)", name, truncate(args, 80)),
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
