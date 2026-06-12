package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/enough/enough/backend/config"
	"github.com/enough/enough/backend/core"
	"github.com/enough/enough/backend/session"
	"github.com/enough/enough/backend/skills"
)

func (a *App) startAgent(task string) {
	a.running = true
	a.beginAgentActivity()
	a.evidenceCount = 0
	a.obligationState = nil
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

		if len(a.preloadedSkills) > 0 {
			workDir := ""
			if a.session != nil {
				workDir = a.session.CWD()
			}
			sessionId := ""
			if a.session != nil {
				sessionId = a.session.SessionID()
			}
			promptText, loaded, _, _ := skills.BuildPreloadedSkillsPrompt(a.preloadedSkills, workDir, sessionId, cfg)
			cfg.PreloadedSkillsPrompt = promptText
			cfg.PreloadedSkills = loaded
		}

		a.mu.Lock()
		ag := a.ensureAgent(cfg)
		a.mu.Unlock()

		_ = ag.Prompt(context.Background(), cfg, task, emit)
	}()
}

func (a *App) handleAgentEvent(e core.Event) {
	switch e.Kind {
	case core.EventCompactionStart:
		label := "Compacting context..."
		if ev, ok := e.Data.(core.CompactionStartEvent); ok {
			switch ev.Reason {
			case "overflow":
				label = "Context overflow detected, auto-compacting..."
			case "threshold":
				label = "Auto-compacting..."
			}
		}
		a.setCompacting(true, label)

	case core.EventCompactionEnd:
		a.setCompacting(false, "")

		var ev core.CompactionEndEvent
		if data, ok := e.Data.(core.CompactionEndEvent); ok {
			ev = data
		}

		if ev.Aborted {
			if ev.Reason == "manual" {
				a.appendMessage("system", "Compaction cancelled")
			} else {
				a.appendMessage("system", "Auto-compaction cancelled")
			}
		} else if ev.ErrorMessage != "" {
			a.appendMessage("error", ev.ErrorMessage)
		} else if result, ok := ev.Result.(*session.CompactionResult); ok && result != nil {
			after := 0
			if a.session != nil {
				after = session.EstimateContextTokens(a.session.BuildSessionContext().Messages).Tokens
			}
			a.appendMessage("system", fmt.Sprintf("Compacted from %d → %d tokens", result.TokensBefore, after))
			a.reloadChatFromSession()
		} else {
			a.reloadChatFromSession()
		}

		if !ev.WillRetry && len(a.compactionQueuedMessages) > 0 {
			queued := strings.Join(a.compactionQueuedMessages, "\n")
			a.compactionQueuedMessages = nil
			a.startAgent(queued)
		}

	case core.EventBranchSummaryStart:
		a.setCompacting(true, "Summarizing branch...")

	case core.EventBranchSummaryEnd:
		a.setCompacting(false, "")

		var ev core.BranchSummaryEndEvent
		if data, ok := e.Data.(core.BranchSummaryEndEvent); ok {
			ev = data
		}

		if ev.Aborted {
			a.appendMessage("system", "Branch summarization cancelled")
		} else if ev.ErrorMessage != "" {
			a.appendMessage("error", ev.ErrorMessage)
		} else {
			a.reloadChatFromSession()
		}

	case core.EventAssistantStart:
		a.onAssistantStreamStart()
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

	case core.EventToolStart:
		if ev, ok := e.Data.(core.ToolCallEvent); ok {
			a.handleToolStart(ev)
		}

	case core.EventToolDelta:
		if ev, ok := e.Data.(core.ToolCallEvent); ok {
			a.handleToolDelta(ev)
		}

	case core.EventEvidenceAppend:
		if ev, ok := e.Data.(core.EvidenceEvent); ok {
			a.evidenceCount = ev.Count
			a.bumpChat() // footer cache keys off chatRevision
		}

	case core.EventObligationUpdate:
		if ev, ok := e.Data.(core.ObligationEvent); ok {
			a.setObligationState(ev)
			a.bumpChat()
		}

	case core.EventToolResult:
		if ev, ok := e.Data.(core.ToolCallEvent); ok {
			a.handleToolResult(ev)
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
