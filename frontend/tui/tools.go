package tui

import (
	"github.com/enough/enough/backend/agent"
	"github.com/enough/enough/backend/core"
)

func (a *App) handleToolStart(ev core.ToolCallEvent) {
	msg := chatMsg{
		role:        "tool",
		toolID:      ev.ID,
		toolName:    ev.Name,
		toolArgs:    ev.Args,
		toolPending: true,
	}
	switch ev.Name {
	case "write_file", "edit_file":
		if before, ok := readFileForDiff(pathFromToolArgs(ev.Args)); ok {
			msg.toolDiffSnapshotted = true
			msg.toolBeforeContent = before
		}
	}
	a.messages = append(a.messages, msg)
	a.bumpChat()
}

// handleToolDelta appends a chunk of live tool output to the matching pending
// tool message so long-running tools (bash) show progress as it streams.
func (a *App) handleToolDelta(ev core.ToolCallEvent) {
	clean, _ := sanitizeToolOutput(ev.Name, ev.Result)
	if clean == "" {
		return
	}
	for i := len(a.messages) - 1; i >= 0; i-- {
		msg := &a.messages[i]
		if msg.role != "tool" || !msg.toolPending {
			continue
		}
		if ev.ID != "" && msg.toolID != ev.ID {
			continue
		}
		msg.toolResult += clean
		a.bumpChat()
		return
	}
}

func (a *App) handleToolResult(ev core.ToolCallEvent) {
	for i := len(a.messages) - 1; i >= 0; i-- {
		msg := &a.messages[i]
		if msg.role != "tool" {
			continue
		}
		if ev.ID != "" && msg.toolID != ev.ID {
			continue
		}
		if !msg.toolPending && msg.toolResult != "" {
			continue
		}
		msg.toolPending = false
		clean, _ := sanitizeToolOutput(msg.toolName, ev.Result)
		msg.toolResult = clean
		msg.toolError = ev.Error
		switch msg.toolName {
		case "write_file", "edit_file":
			if msg.toolDiffSnapshotted {
				msg.toolAdded, msg.toolRemoved = finalizeFileToolDiff(
					msg.toolName, msg.toolArgs, msg.toolBeforeContent, ev.Error,
				)
			}
		}
		a.bumpChat()
		return
	}

	clean, _ := sanitizeToolOutput(ev.Name, ev.Result)
	a.messages = append(a.messages, chatMsg{
		role:       "tool",
		toolID:     ev.ID,
		toolName:   ev.Name,
		toolResult: clean,
		toolError:  ev.Error,
	})
	a.bumpChat()
}

func sanitizeToolOutput(toolName, text string) (string, bool) {
	if text == "" {
		return "", false
	}
	if toolName == "bash" || toolName == "" {
		return agent.SanitizeBashOutput(text)
	}
	return text, false
}

func sanitizeLoadedToolResult(toolName, text string) string {
	clean, _ := sanitizeToolOutput(toolName, text)
	return clean
}

func (a *App) toggleToolsExpanded() {
	a.toolsExpanded = !a.toolsExpanded
	state := "collapsed"
	if a.toolsExpanded {
		state = "expanded"
	}
	a.appendMessage("system", "Tool output: "+state)
}
