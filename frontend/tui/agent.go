package tui

import (
	"context"

	"github.com/enough/enough/backend/config"
	"github.com/enough/enough/backend/core"
)

func (a *App) startAgent(task string) {
	a.running = true
	ch := make(chan core.Event, 64)
	a.agentCh = ch

	go func() {
		defer close(ch)

		emit := func(e core.Event) {
			ch <- e
		}

		cfg, err := config.LoadRuntime()
		if err != nil {
			emit(core.Event{Kind: core.EventError, Data: err.Error()})
			return
		}

		a.mu.Lock()
		ag := a.ensureAgent(cfg)
		a.mu.Unlock()

		_ = ag.Prompt(context.Background(), cfg, task, emit)
	}()
}

func (a *App) handleAgentEvent(e core.Event) {
	switch e.Kind {
	case core.EventAssistantStart:
		a.ensureAssistantBubble()

	case core.EventAssistantThinkingDelta:
		if delta, ok := e.Data.(string); ok {
			a.appendAssistantThinkingDelta(delta)
		}

	case core.EventAssistantDelta:
		if delta, ok := e.Data.(string); ok {
			a.appendAssistantDelta(delta)
		}

	case core.EventAssistantMessage:
		if text, ok := e.Data.(string); ok {
			a.setLastAssistant(text)
		}

	case core.EventToolActivity:
		if text, ok := e.Data.(string); ok {
			a.appendMessage("tool", text)
		}

	case core.EventError:
		if text, ok := e.Data.(string); ok {
			a.appendMessage("error", text)
		}

	case core.EventSystem:
		if text, ok := e.Data.(string); ok {
			a.appendMessage("system", text)
		}

	case core.EventLog:
		if chat, ok := eventToChatMsg(e); ok {
			a.appendMessage(chat.role, chat.text)
		}
	}
}

func eventToChatMsg(e core.Event) (chatMsg, bool) {
	switch e.Kind {
	case core.EventAssistantMessage:
		text, ok := e.Data.(string)
		return chatMsg{role: "assistant", text: text}, ok

	case core.EventToolActivity:
		text, ok := e.Data.(string)
		return chatMsg{role: "tool", text: text}, ok

	case core.EventError:
		text, ok := e.Data.(string)
		return chatMsg{role: "error", text: text}, ok

	case core.EventSystem:
		text, ok := e.Data.(string)
		return chatMsg{role: "system", text: text}, ok

	case core.EventLog:
		entry, ok := e.Data.(core.LogEntry)
		if !ok {
			return chatMsg{}, false
		}
		switch entry.Level {
		case "err":
			return chatMsg{role: "error", text: entry.Message}, true
		default:
			return chatMsg{role: "system", text: entry.Message}, true
		}

	default:
		return chatMsg{}, false
	}
}
