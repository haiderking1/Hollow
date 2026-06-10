package highlight

import (
	"strings"

	"github.com/enough/enough/frontend/tui/term"
)

// Render formats assistant message text with Gruvbox code fences and inline markdown.
func Render(text string, width int, style TextStyle) string {
	if text == "" {
		return ""
	}
	if width < 10 {
		width = 10
	}

	p := GruvboxDark()
	style = style.withDefaults()
	blocks := ParseBlocks(text)
	var lines []string

	for _, block := range blocks {
		switch block.Kind {
		case BlockProse:
			lines = append(lines, renderProse(block.Text, width, style, p)...)
		case BlockCode:
			lines = append(lines, renderCodeBlock(block, p)...)
		}
	}
	return strings.Join(lines, "\n")
}

func renderProse(text string, width int, style TextStyle, p Palette) []string {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return nil
	}

	var out []string
	for _, paragraph := range strings.Split(text, "\n") {
		if strings.TrimSpace(paragraph) == "" {
			out = append(out, "")
			continue
		}
		styled := stylizeProse(paragraph, style, p)
		wrapped := wrapWords(styled, width)
		out = append(out, strings.Split(wrapped, "\n")...)
	}
	return out
}

func renderCodeBlock(block Block, p Palette) []string {
	if block.Text == "" && block.Lang == "" {
		return nil
	}

	var lines []string
	if block.Lang != "" {
		lines = append(lines, p.paint(p.Special, block.Lang))
	}

	highlighted := HighlightCode(block.Lang, block.Text, p)
	if highlighted != "" {
		lines = append(lines, strings.Split(highlighted, "\n")...)
	}
	return lines
}

func wrapWords(text string, width int) string {
	words := splitWordsPreserveANSI(text)
	if len(words) == 0 {
		return text
	}

	var lines []string
	var line strings.Builder
	lineWidth := 0

	flush := func() {
		if line.Len() > 0 {
			lines = append(lines, line.String())
			line.Reset()
			lineWidth = 0
		}
	}

	for _, word := range words {
		w := visibleWidth(word)
		if lineWidth == 0 {
			line.WriteString(word)
			lineWidth = w
			continue
		}
		if lineWidth+1+w > width {
			flush()
			line.WriteString(word)
			lineWidth = w
			continue
		}
		line.WriteByte(' ')
		line.WriteString(word)
		lineWidth += 1 + w
	}
	flush()
	return strings.Join(lines, "\n")
}

func splitWordsPreserveANSI(text string) []string {
	var words []string
	var cur strings.Builder
	inEscape := false

	flush := func() {
		if cur.Len() > 0 {
			words = append(words, cur.String())
			cur.Reset()
		}
	}

	for i := 0; i < len(text); i++ {
		b := text[i]
		if inEscape {
			cur.WriteByte(b)
			if b == 'm' {
				inEscape = false
			}
			continue
		}
		if b == '\x1b' {
			inEscape = true
			cur.WriteByte(b)
			continue
		}
		if b == ' ' || b == '\t' {
			flush()
			continue
		}
		cur.WriteByte(b)
	}
	flush()
	return words
}

func visibleWidth(s string) int {
	return term.VisibleWidth(s)
}
