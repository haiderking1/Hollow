package tui

import (
	"github.com/enough/enough/frontend/tui/markdown"
	"github.com/enough/enough/frontend/tui/term"
)

func clampSplitLines(lines []string, width int) []string {
	if width <= 0 || len(lines) == 0 {
		return lines
	}
	sixelMask := markdown.GetSixelLineMask(lines)
	out := make([]string, len(lines))
	for i, line := range lines {
		if sixelMask[i] || markdown.IsImageLayoutRow(line) || line == "" {
			out[i] = line
			continue
		}
		if term.VisibleWidth(line) > width {
			out[i] = term.TruncateWidth(line, width)
		} else {
			out[i] = line
		}
	}
	return out
}
