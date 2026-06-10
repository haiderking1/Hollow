package tui

import (
	"fmt"
	"strings"

	"github.com/enough/enough/frontend/tui/highlight"
)

type chatMsg struct {
	role         string // user, assistant, tool, error, system, compactionSummary, branchSummary
	text         string
	thinking     string
	toolID       string
	toolName     string
	toolArgs     string
	toolResult   string
	toolError    bool
	toolPending bool
	toolAdded    int
	toolRemoved  int
	tokensBefore int
}

func wrapText(text string, width int) string {
	if width < 10 {
		width = 10
	}

	parts := strings.Split(text, "\n")
	var out []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			out = append(out, "")
			continue
		}
		out = append(out, wrapWords(part, width))
	}
	return strings.Join(out, "\n")
}

func wrapWords(text string, width int) string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return text
	}

	var lines []string
	var line strings.Builder

	flush := func() {
		if line.Len() > 0 {
			lines = append(lines, line.String())
			line.Reset()
		}
	}

	for _, word := range words {
		if line.Len() == 0 {
			line.WriteString(word)
			continue
		}
		if line.Len()+1+len(word) > width {
			flush()
			line.WriteString(word)
			continue
		}
		line.WriteString(" ")
		line.WriteString(word)
	}
	flush()
	return strings.Join(lines, "\n")
}

func renderCompactionSummary(styles Styles, msg chatMsg, width int, expanded bool) string {
	label := styles.LogAccent.Render("[compaction]")

	if expanded {
		header := fmt.Sprintf("**Compacted from %d tokens**\n\n", msg.tokensBefore)
		content := header + msg.text
		return label + "\n" + wrapText(content, width)
	}

	text := fmt.Sprintf("Compacted from %d tokens (ctrl+o to expand)", msg.tokensBefore)
	return label + " " + styles.LogDim.Render(text)
}

func renderBranchSummary(styles Styles, msg chatMsg, width int, expanded bool) string {
	label := styles.LogAccent.Render("[branch]")

	if expanded {
		header := "**Branch Summary**\n\n"
		content := header + msg.text
		return label + "\n" + wrapText(content, width)
	}

	text := "Branch summary (ctrl+o to expand)"
	return label + " " + styles.LogDim.Render(text)
}

// renderChat formats the messages list.
// Note: Ctrl+O toggles expandTools, which in Enough's scrolling-only chat layout
// globally expands both tool call outputs and compaction/branch summary details.
// This is intentional to keep navigation simple and consistent.
func renderChat(styles Styles, messages []chatMsg, width int, hideThinking, expandTools bool) string {
	if width <= 0 {
		width = 80
	}

	contentW := width - 2
	var blocks []string
	var roles []string

	for i := 0; i < len(messages); i++ {
		msg := messages[i]
		switch msg.role {
		case "user":
			block := renderUser(styles, msg.text, contentW)
			if block != "" {
				blocks = append(blocks, block)
				roles = append(roles, "user")
			}
		case "assistant":
			block := renderAssistant(styles, msg, contentW, hideThinking)
			if block != "" {
				blocks = append(blocks, block)
				roles = append(roles, "assistant")
			}
		case "tool":
			var group []chatMsg
			for i < len(messages) && messages[i].role == "tool" {
				group = append(group, messages[i])
				i++
			}
			i--
			block := renderToolGroup(styles, group, contentW, expandTools)
			if block != "" {
				blocks = append(blocks, block)
				roles = append(roles, "tool")
			}
		case "compactionSummary":
			block := renderCompactionSummary(styles, msg, contentW, expandTools)
			if block != "" {
				blocks = append(blocks, block)
				roles = append(roles, "compactionSummary")
			}
		case "branchSummary":
			block := renderBranchSummary(styles, msg, contentW, expandTools)
			if block != "" {
				blocks = append(blocks, block)
				roles = append(roles, "branchSummary")
			}
		case "error":
			blocks = append(blocks, styles.AssistError.Render("● "+wrapText(msg.text, contentW-4)))
			roles = append(roles, "error")
		case "system":
			blocks = append(blocks, styles.LogDim.Render(wrapText(msg.text, contentW-4)))
			roles = append(roles, "system")
		}
	}

	return joinChatBlocks(blocks, roles)
}

func joinChatBlocks(blocks, roles []string) string {
	if len(blocks) == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString(blocks[0])
	for i := 1; i < len(blocks); i++ {
		sep := "\n\n"
		if roles[i] == "tool" || roles[i-1] == "tool" {
			sep = "\n"
		}
		out.WriteString(sep)
		out.WriteString(blocks[i])
	}
	return out.String()
}

func renderUser(styles Styles, text string, width int) string {
	wrapped := wrapText(text, width-4)
	lines := strings.Split(wrapped, "\n")
	if len(lines) == 0 {
		return ""
	}

	var out strings.Builder
	out.WriteString(styles.InputPrompt.Render("❯ "))
	out.WriteString(styles.Text.Render(lines[0]))

	for _, line := range lines[1:] {
		out.WriteString("\n")
		out.WriteString(styles.Text.Render("  " + line))
	}
	return out.String()
}

func renderAssistant(styles Styles, msg chatMsg, width int, hideThinking bool) string {
	var parts []string

	if strings.TrimSpace(msg.thinking) != "" {
		if hideThinking {
			parts = append(parts, renderThinkingLabel(styles, contentWidth(width)))
		} else {
			parts = append(parts, renderThinkingBody(styles, msg.thinking, contentWidth(width)))
		}
	}

	if strings.TrimSpace(msg.text) != "" {
		parts = append(parts, renderAssistantText(styles, msg.text, contentWidth(width)))
	}

	if len(parts) == 0 {
		return renderAssistantText(styles, msg.text, contentWidth(width))
	}
	return strings.Join(parts, "\n\n")
}

func contentWidth(width int) int {
	return width - 4
}

func renderThinkingLabel(styles Styles, width int) string {
	return styles.ThinkingText.Render(wrapText("Thinking...", width))
}

func renderThinkingBody(styles Styles, thinking string, width int) string {
	wrapped := wrapText(strings.TrimSpace(thinking), width)
	lines := strings.Split(wrapped, "\n")
	if len(lines) == 0 {
		return ""
	}

	var out strings.Builder
	out.WriteString(styles.ThinkingText.Render(lines[0]))
	for _, line := range lines[1:] {
		out.WriteString("\n")
		out.WriteString(styles.ThinkingText.Render("  " + line))
	}
	return out.String()
}

func renderAssistantText(styles Styles, text string, width int) string {
	body := highlight.Render(text, width, highlight.TextStyle{
		Plain: func(line string) string {
			return styles.AssistText.Render(line)
		},
		Bold: func(line string) string {
			return styles.AssistText.Copy().Bold(true).Render(line)
		},
		Italic: func(line string) string {
			return styles.AssistText.Copy().Italic(true).Render(line)
		},
	})
	if body == "" {
		return ""
	}

	lines := strings.Split(body, "\n")
	var out strings.Builder
	out.WriteString(styles.AssistBullet.Render("● "))
	out.WriteString(lines[0])

	for _, line := range lines[1:] {
		out.WriteString("\n")
		out.WriteString("  ")
		out.WriteString(line)
	}
	return out.String()
}
