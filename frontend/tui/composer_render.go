package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/enough/enough/frontend/tui/term"
	"github.com/mattn/go-runewidth"
)

type composerLayoutLine struct {
	text      string
	cursorPos int // -1 when line has no cursor
}

func (a *App) composerBorderColor() lipgloss.Color {
	switch a.mode {
	case modeConnect, modeConnectPicker, modeConnectCodex, modePluginsSecret:
		return connectBorderColor()
	default:
		return thinkingBorderColor(string(a.thinkingLevel))
	}
}

func (a *App) flameComposerLines(width int) []string {
	if width < 1 {
		width = 1
	}
	borderColor := a.composerBorderColor()
	rule := lipgloss.NewStyle().Foreground(borderColor).Render(strings.Repeat("─", width))

	contentWidth := width
	layoutWidth := width - 1
	if layoutWidth < 1 {
		layoutWidth = 1
	}

	var body []string
	if a.composerUsesEditorLayout() {
		for _, line := range layoutComposerRunes(a.editor.Runes(), a.editor.Cursor(), layoutWidth) {
			body = append(body, renderComposerDisplayLine(line, contentWidth, a.styles.Text))
		}
		body = append(body, a.composerAttachmentLines(contentWidth)...)
	} else {
		for _, styled := range strings.Split(a.renderTaskInput(), "\n") {
			if styled == "" {
				continue
			}
			pad := contentWidth - term.VisibleWidth(styled)
			if pad < 0 {
				pad = 0
			}
			body = append(body, styled+strings.Repeat(" ", pad))
		}
	}

	if len(body) == 0 {
		body = []string{renderComposerDisplayLine(composerLayoutLine{cursorPos: 0}, contentWidth, a.styles.Text)}
	}

	out := make([]string, 0, len(body)+2)
	out = append(out, rule)
	out = append(out, body...)
	out = append(out, rule)
	return out
}

func (a *App) composerAttachmentLines(contentWidth int) []string {
	if len(a.pendingAttachments) == 0 {
		return nil
	}
	var chips []string
	for _, att := range a.pendingAttachments {
		chips = append(chips, a.styles.InputHint.Render(fmt.Sprintf("[🖼 image (%dx%d)]", att.width, att.height)))
	}
	line := "  " + strings.Join(chips, " ")
	pad := contentWidth - term.VisibleWidth(line)
	if pad > 0 {
		line += strings.Repeat(" ", pad)
	}
	return []string{line}
}

func (a *App) composerUsesEditorLayout() bool {
	switch a.mode {
	case modePluginsPicker, modeConnectCodex, modeWriteApproval:
		return false
	default:
		return true
	}
}

func layoutComposerRunes(runes []rune, cursor int, layoutWidth int) []composerLayoutLine {
	if len(runes) == 0 {
		return []composerLayoutLine{{text: "", cursorPos: 0}}
	}

	var lines []composerLayoutLine
	start := 0
	for start < len(runes) {
		end := start
		width := 0
		for end < len(runes) {
			rw := runewidth.RuneWidth(runes[end])
			if width+rw > layoutWidth {
				break
			}
			width += rw
			end++
		}
		if end == start {
			end = start + 1
		}

		text := string(runes[start:end])
		cursorPos := -1
		if cursor >= start && cursor < end {
			cursorPos = cursor - start
		} else if cursor == end && end >= len(runes) {
			cursorPos = end - start
		}

		lines = append(lines, composerLayoutLine{text: text, cursorPos: cursorPos})
		start = end
	}
	return lines
}

func renderComposerDisplayLine(line composerLayoutLine, contentWidth int, textStyle lipgloss.Style) string {
	rs := []rune(line.text)
	var out string

	switch {
	case line.cursorPos < 0:
		out = textStyle.Render(line.text)
	case line.cursorPos >= len(rs):
		out = textStyle.Render(line.text) + "\x1b[7m \x1b[0m"
	default:
		before := string(rs[:line.cursorPos])
		at := string(rs[line.cursorPos])
		after := string(rs[line.cursorPos+1:])
		out = textStyle.Render(before) + "\x1b[7m" + at + "\x1b[0m"
		if after != "" {
			out += textStyle.Render(after)
		}
	}

	pad := contentWidth - term.VisibleWidth(out)
	if pad > 0 {
		out += strings.Repeat(" ", pad)
	}
	return out
}
