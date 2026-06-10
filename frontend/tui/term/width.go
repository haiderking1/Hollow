package term

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

func VisibleWidth(s string) int {
	return runewidth.StringWidth(ansi.Strip(s))
}

func TruncateWidth(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if VisibleWidth(s) <= max {
		return s
	}
	plain := ansi.Strip(s)
	var w int
	for _, r := range plain {
		rw := runewidth.RuneWidth(r)
		if w+rw > max {
			break
		}
		w += rw
	}
	// Re-truncate by rune width on stripped string
	var out strings.Builder
	w = 0
	for _, r := range plain {
		rw := runewidth.RuneWidth(r)
		if w+rw > max {
			break
		}
		out.WriteRune(r)
		w += rw
	}
	return out.String()
}
