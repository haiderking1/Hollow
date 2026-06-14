package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/enough/enough/backend/core"
	"github.com/enough/enough/backend/session"
	"github.com/enough/enough/frontend/tui/highlight"
	"github.com/enough/enough/frontend/tui/markdown"
	"github.com/enough/enough/frontend/tui/term"
)

type chatImage struct {
	Path     string
	MIMEType string
	Width    int
	Height   int
	URL      string
}

type chatMsg struct {
	role         string // user, assistant, tool, error, system, compactionSummary, branchSummary
	text         string
	thinking     string
	toolID       string
	toolName     string
	toolArgs     string
	toolResult   string
	toolDetails  string
	toolError    bool
	toolPending bool
	toolAdded    int
	toolRemoved  int
	toolDiffSnapshotted bool
	toolBeforeContent   string
	tokensBefore int
	images       []chatImage
}

// chatMsgFromSessionLine maps a persisted session line to a TUI chat message.
// When skipRuntimeNotice is true, internal user-role runtime notices are dropped.
func chatMsgFromSessionLine(line session.ChatLine, skipRuntimeNotice bool) (chatMsg, bool) {
	if skipRuntimeNotice && line.Role == "user" && strings.HasPrefix(line.Text, core.RuntimeNoticePrefix) {
		return chatMsg{}, false
	}
	var chatImages []chatImage
	for _, img := range line.Images {
		chatImages = append(chatImages, chatImage{URL: img.URL})
	}
	cleanResult, legacyMetadata := extractAndStripBrowserMetadata(line.ToolName, line.ToolResult)
	toolDetails := line.ToolDetails
	if toolDetails == "" {
		toolDetails = legacyMetadata
	}
	return chatMsg{
		role:         line.Role,
		text:         line.Text,
		thinking:     line.Thinking,
		toolName:     line.ToolName,
		toolArgs:     line.ToolArgs,
		toolResult:   cleanResult,
		toolDetails:  toolDetails,
		toolError:    line.ToolError,
		tokensBefore: line.TokensBefore,
		images:       chatImages,
	}, true
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

func renderSkillSummary(styles Styles, msg chatMsg, width int, expanded bool) string {
	label := styles.LogAccent.Render("[skill]")

	if expanded {
		header := fmt.Sprintf("**Skill: %s**", msg.toolName)
		if strings.TrimSpace(msg.toolArgs) != "" {
			header += fmt.Sprintf("\n\n**Args:** %s", strings.TrimSpace(msg.toolArgs))
		}
		content := header + "\n\n" + msg.text
		return label + "\n" + wrapText(content, width)
	}

	text := fmt.Sprintf("loaded skill '%s'", msg.toolName)
	if strings.TrimSpace(msg.toolArgs) != "" {
		text += fmt.Sprintf(" — %s", term.TruncateWidth(strings.TrimSpace(msg.toolArgs), 40))
	}
	text += " (ctrl+o to expand)"
	return label + " " + styles.LogDim.Render(text)
}

// chatBlockSpec describes one rendered chat block. fp is a fingerprint of the
// block's display-relevant content; render produces the block lazily so callers
// can skip rendering blocks whose fp matches a cached entry.
type chatBlockSpec struct {
	role   string
	fp     uint64
	render func() string
}

// Alloc-free FNV-1a helpers. Fingerprinting must stay cheap: it runs over every
// block on every frame, so reflection-based hashing (fmt "%v") would reintroduce
// the O(transcript) cost the per-block cache exists to remove.
const (
	fnvOffset uint64 = 14695981039346656037
	fnvPrime  uint64 = 1099511628211
)

func fnvStr(h uint64, s string) uint64 {
	for i := 0; i < len(s); i++ {
		h = (h ^ uint64(s[i])) * fnvPrime
	}
	return (h ^ 0) * fnvPrime // field separator so "ab"+"c" != "a"+"bc"
}

func fnvByte(h uint64, b byte) uint64 { return (h ^ uint64(b)) * fnvPrime }

func fnvInt(h uint64, n int) uint64 {
	u := uint64(n)
	for i := 0; i < 8; i++ {
		h = fnvByte(h, byte(u))
		u >>= 8
	}
	return h
}

func fnvBool(h uint64, b bool) uint64 {
	if b {
		return fnvByte(h, 1)
	}
	return fnvByte(h, 0)
}

// hashMsg folds every display-relevant field of a message into h so any change
// to a rendered block flips its fingerprint.
func hashMsg(h uint64, m chatMsg) uint64 {
	h = fnvStr(h, m.role)
	h = fnvStr(h, m.text)
	h = fnvStr(h, m.thinking)
	h = fnvStr(h, m.toolID)
	h = fnvStr(h, m.toolName)
	h = fnvStr(h, m.toolArgs)
	h = fnvStr(h, m.toolResult)
	h = fnvBool(h, m.toolError)
	h = fnvBool(h, m.toolPending)
	h = fnvInt(h, m.toolAdded)
	h = fnvInt(h, m.toolRemoved)
	h = fnvInt(h, m.tokensBefore)
	for _, img := range m.images {
		h = fnvStr(h, img.URL)
		h = fnvStr(h, img.Path)
		url := img.URL
		if url == "" && img.Path != "" {
			url = "file://" + img.Path
		}
		if url != "" && markdown.ImageReady(url) {
			h = fnvByte(h, 2)
		}
	}
	return h
}

// chatBlockSpecs groups messages into blocks (matching renderChat's grouping)
// without rendering them. width/hideThinking/expandTools are folded into each
// fp so a change in those invalidates every block.
func chatBlockSpecs(styles Styles, messages []chatMsg, width int, hideThinking, expandTools bool, toolSpinnerFrame int, mdOpts markdown.RenderOptions) []chatBlockSpec {
	contentW := width - 2
	var specs []chatBlockSpec

	// Seed every fingerprint with the width and global render flags so a change
	// to either invalidates the affected blocks.
	seed := fnvInt(fnvOffset, contentW)
	seed = fnvBool(seed, hideThinking)
	seed = fnvBool(seed, expandTools)

	add := func(role string, fp uint64, render func() string) {
		specs = append(specs, chatBlockSpec{role: role, fp: fp, render: render})
	}

	for i := 0; i < len(messages); i++ {
		msg := messages[i]
		switch msg.role {
		case "user":
			m := msg
			add("user", hashMsg(seed, m), func() string {
				return renderUser(styles, m, contentW, mdOpts)
			})
		case "assistant":
			m := msg
			add("assistant", hashMsg(seed, m), func() string {
				return renderAssistant(styles, m, contentW, hideThinking, mdOpts)
			})
		case "tool":
			var group []chatMsg
			for i < len(messages) && messages[i].role == "tool" {
				group = append(group, messages[i])
				i++
			}
			i--
			g := group
			fp := seed
			for _, t := range g {
				fp = hashMsg(fp, t)
			}
			if toolGroupAnimates(g) {
				fp = fnvInt(fp, toolSpinnerFrame)
			}
			frame := toolSpinnerFrame
			add("tool", fp, func() string {
				return renderToolGroup(styles, g, contentW, expandTools, frame)
			})
		case "compactionSummary":
			m := msg
			add("compactionSummary", hashMsg(seed, m), func() string {
				return renderCompactionSummary(styles, m, contentW, expandTools)
			})
		case "branchSummary":
			m := msg
			add("branchSummary", hashMsg(seed, m), func() string {
				return renderBranchSummary(styles, m, contentW, expandTools)
			})
		case "skillSummary":
			m := msg
			add("skillSummary", hashMsg(seed, m), func() string {
				return renderSkillSummary(styles, m, contentW, expandTools)
			})
		case "error":
			m := msg
			add("error", hashMsg(seed, m), func() string {
				return styles.AssistError.Render("● " + wrapText(m.text, contentW-4))
			})
		case "system":
			m := msg
			add("system", hashMsg(seed, m), func() string {
				return styles.LogDim.Render(wrapText(m.text, contentW-4))
			})
		}
	}

	return specs
}

// renderChat formats the messages list.
// Note: Ctrl+O toggles expandTools, which in Enough's scrolling-only chat layout
// globally expands both tool call outputs and compaction/branch summary details.
// This is intentional to keep navigation simple and consistent.
func renderChat(styles Styles, messages []chatMsg, width int, hideThinking, expandTools bool) string {
	if width <= 0 {
		width = 80
	}

	specs := chatBlockSpecs(styles, messages, width, hideThinking, expandTools, 0, markdown.RenderOptions{})
	var blocks []string
	var roles []string
	for _, spec := range specs {
		block := spec.render()
		if block == "" {
			continue
		}
		blocks = append(blocks, block)
		roles = append(roles, spec.role)
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

func renderUser(styles Styles, msg chatMsg, width int, mdOpts markdown.RenderOptions) string {
	wrapped := wrapText(msg.text, width-4)
	lines := strings.Split(wrapped, "\n")

	var out strings.Builder
	hasText := len(lines) > 0 && lines[0] != ""
	if hasText {
		out.WriteString(styles.InputPrompt.Render("❯ "))
		out.WriteString(styles.Text.Render(lines[0]))

		for _, line := range lines[1:] {
			out.WriteString("\n")
			out.WriteString(styles.Text.Render("  " + line))
		}
	}

	for _, img := range msg.images {
		var imgURL string
		if img.Path != "" {
			imgURL = "file://" + filepath.ToSlash(img.Path)
		} else {
			imgURL = img.URL
		}
		renderedImg := markdown.RenderAttachmentImage(imgURL, width-4, userMarkdownTheme(styles), mdOpts)

		// Image protocol lines must be raw — margins corrupt sixel/kitty sequences
		// and break direct-placement reserved rows during incremental redraw.
		imgLines := strings.Split(strings.TrimRight(renderedImg, "\n"), "\n")
		for _, imgLine := range imgLines {
			if out.Len() > 0 {
				out.WriteString("\n")
			}
			out.WriteString(imgLine)
		}
	}

	return out.String()
}

func userMarkdownTheme(styles Styles) markdown.Theme {
	p := highlight.GruvboxDark()
	return markdown.Theme{
		Plain: func(s string) string {
			return styles.Text.Render(s)
		},
		Bold: func(s string) string {
			return styles.Text.Copy().Bold(true).Render(s)
		},
		Italic: func(s string) string {
			return styles.Text.Copy().Italic(true).Render(s)
		},
		Code: func(s string) string {
			return p.InlineCode(s)
		},
		Link: func(s string) string {
			return styles.LogAccent.Render(s)
		},
		LinkURL: func(s string) string {
			return styles.LogDim.Render(s)
		},
		Heading: func(s string) string {
			return styles.LogAccent.Copy().Bold(true).Render(s)
		},
	}
}

func renderAssistant(styles Styles, msg chatMsg, width int, hideThinking bool, mdOpts markdown.RenderOptions) string {
	var parts []string

	if strings.TrimSpace(msg.thinking) != "" {
		if hideThinking {
			parts = append(parts, renderThinkingLabel(styles, contentWidth(width)))
		} else {
			parts = append(parts, renderThinkingBody(styles, msg.thinking, contentWidth(width)))
		}
	}

	if strings.TrimSpace(msg.text) != "" {
		parts = append(parts, renderAssistantText(styles, msg.text, contentWidth(width), mdOpts))
	}

	if len(parts) == 0 {
		return renderAssistantText(styles, msg.text, contentWidth(width), mdOpts)
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

func renderAssistantText(styles Styles, text string, width int, mdOpts markdown.RenderOptions) string {
	body := markdown.Render(text, width, assistantMarkdownTheme(styles), mdOpts)
	if body == "" {
		return ""
	}

	lines := strings.Split(body, "\n")
	var out strings.Builder
	for i, line := range lines {
		if i > 0 {
			out.WriteString("\n")
		}
		if markdown.IsImageLine(line) {
			out.WriteString(line)
			continue
		}
		if i == 0 {
			out.WriteString(styles.AssistBullet.Render("● "))
		} else {
			out.WriteString("  ")
		}
		out.WriteString(line)
	}
	return out.String()
}

func assistantMarkdownTheme(styles Styles) markdown.Theme {
	p := highlight.GruvboxDark()
	return markdown.Theme{
		Plain: func(s string) string {
			return styles.AssistText.Render(s)
		},
		Bold: func(s string) string {
			return styles.AssistText.Copy().Bold(true).Render(s)
		},
		Italic: func(s string) string {
			return styles.AssistText.Copy().Italic(true).Render(s)
		},
		Code: func(s string) string {
			return p.InlineCode(s)
		},
		Link: func(s string) string {
			return styles.LogAccent.Render(s)
		},
		LinkURL: func(s string) string {
			return styles.LogDim.Render(s)
		},
		Heading: func(s string) string {
			return styles.AssistText.Copy().Bold(true).Render(s)
		},
		Quote: func(s string) string {
			return styles.AssistText.Copy().Italic(true).Render(s)
		},
		QuoteBorder: func(s string) string {
			return styles.LogDim.Render(s)
		},
		HR: func(s string) string {
			return styles.LogDim.Render(s)
		},
		ListBullet: func(s string) string {
			return styles.LogAccent.Render(s)
		},
		Image: func(s string) string {
			return styles.LogDim.Render(s)
		},
		CodeBlockBorder: func(s string) string {
			return p.Paint(p.Special, s)
		},
		CodeBlockIndent: "  ",
		HighlightCode: func(lang, code string) []string {
			out := highlight.HighlightCode(lang, code, p)
			if out == "" {
				return nil
			}
			return strings.Split(out, "\n")
		},
	}
}
