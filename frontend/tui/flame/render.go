package flame

import (
	"os"
	"strings"

	"github.com/enough/enough/frontend/tui/term"
)

func isTermuxSession() bool {
	return os.Getenv("TERMUX_VERSION") != ""
}

// Renderer implements Flame packages/tui doRender() with native scrollback.
type Renderer struct {
	Terminal *term.Terminal

	previousLines       []string
	previousWidth       int
	previousHeight      int
	previousViewportTop int
	hardwareCursorRow   int
	maxLinesRendered    int
	stopped             bool
}

func NewRenderer(t *term.Terminal) *Renderer {
	return &Renderer{Terminal: t}
}

func (r *Renderer) Stop() {
	r.stopped = true
	if len(r.previousLines) > 0 {
		targetRow := len(r.previousLines)
		lineDiff := targetRow - r.hardwareCursorRow
		if lineDiff > 0 {
			r.Terminal.Write("\x1b[" + itoa(lineDiff) + "B")
		} else if lineDiff < 0 {
			r.Terminal.Write("\x1b[" + itoa(-lineDiff) + "A")
		}
		r.Terminal.Write("\r\n")
	}
}

func (r *Renderer) Render(lines []string, stablePrefix int) {
	if r.stopped {
		return
	}

	width := r.Terminal.Columns()
	height := r.Terminal.Rows()
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}

	newLines := lines
	widthChanged := r.previousWidth != 0 && r.previousWidth != width
	heightChanged := r.previousHeight != 0 && r.previousHeight != height
	previousBufferLength := height
	if r.previousHeight > 0 {
		previousBufferLength = r.previousViewportTop + r.previousHeight
	}
	prevViewportTop := r.previousViewportTop
	if heightChanged {
		prevViewportTop = max(0, previousBufferLength-height)
	}
	viewportTop := prevViewportTop
	hardwareCursorRow := r.hardwareCursorRow

	computeLineDiff := func(targetRow int) int {
		currentScreenRow := hardwareCursorRow - prevViewportTop
		targetScreenRow := targetRow - viewportTop
		return targetScreenRow - currentScreenRow
	}

	diffStart := 0
	if stablePrefix > 0 && prefixEqual(r.previousLines, newLines, stablePrefix) {
		diffStart = stablePrefix
	}

	fullRender := func(clear bool) {
		var buf strings.Builder
		buf.WriteString("\x1b[?2026h")
		if clear {
			buf.WriteString("\x1b[2J\x1b[H\x1b[3J")
		}
		for i, line := range newLines {
			if i > 0 {
				buf.WriteString("\r\n")
			}
			buf.WriteString(line)
		}
		buf.WriteString("\x1b[?2026l")
		r.Terminal.Write(buf.String())

		r.hardwareCursorRow = max(0, len(newLines)-1)
		if clear {
			r.maxLinesRendered = len(newLines)
		} else {
			r.maxLinesRendered = max(r.maxLinesRendered, len(newLines))
		}
		bufferLength := max(height, len(newLines))
		r.previousViewportTop = max(0, bufferLength-height)
		r.previousLines = append([]string(nil), newLines...)
		r.previousWidth = width
		r.previousHeight = height
	}

	if len(r.previousLines) == 0 && !widthChanged && !heightChanged {
		fullRender(false)
		return
	}

	if widthChanged {
		fullRender(true)
		return
	}

	// Flame: skip full redraw on height change in Termux (software keyboard).
	if heightChanged && !isTermuxSession() {
		fullRender(true)
		return
	}

	firstChanged := -1
	lastChanged := -1
	maxLines := max(len(newLines), len(r.previousLines))
	for i := diffStart; i < maxLines; i++ {
		oldLine := ""
		if i < len(r.previousLines) {
			oldLine = r.previousLines[i]
		}
		newLine := ""
		if i < len(newLines) {
			newLine = newLines[i]
		}
		if oldLine != newLine {
			if firstChanged == -1 {
				firstChanged = i
			}
			lastChanged = i
		}
	}

	appendedLines := len(newLines) > len(r.previousLines)
	if appendedLines {
		if firstChanged == -1 {
			firstChanged = len(r.previousLines)
		}
		lastChanged = len(newLines) - 1
	}
	appendStart := appendedLines && firstChanged == len(r.previousLines) && firstChanged > 0

	if firstChanged == -1 {
		r.previousViewportTop = prevViewportTop
		r.previousHeight = height
		return
	}

	if firstChanged >= len(newLines) {
		if len(r.previousLines) > len(newLines) {
			var buf strings.Builder
			buf.WriteString("\x1b[?2026h")
			targetRow := max(0, len(newLines)-1)
			if targetRow < prevViewportTop {
				fullRender(true)
				return
			}
			lineDiff := computeLineDiff(targetRow)
			if lineDiff > 0 {
				buf.WriteString("\x1b[" + itoa(lineDiff) + "B")
			} else if lineDiff < 0 {
				buf.WriteString("\x1b[" + itoa(-lineDiff) + "A")
			}
			buf.WriteString("\r")
			extraLines := len(r.previousLines) - len(newLines)
			if extraLines > height {
				fullRender(true)
				return
			}
			if extraLines > 0 {
				buf.WriteString("\x1b[1B")
			}
			for i := 0; i < extraLines; i++ {
				buf.WriteString("\r\x1b[2K")
				if i < extraLines-1 {
					buf.WriteString("\x1b[1B")
				}
			}
			if extraLines > 0 {
				buf.WriteString("\x1b[" + itoa(extraLines) + "A")
			}
			buf.WriteString("\x1b[?2026l")
			r.Terminal.Write(buf.String())
			r.hardwareCursorRow = targetRow
		}
		r.previousLines = append([]string(nil), newLines...)
		r.previousWidth = width
		r.previousHeight = height
		r.previousViewportTop = prevViewportTop
		return
	}

	if firstChanged < prevViewportTop {
		fullRender(true)
		return
	}

	var buf strings.Builder
	buf.WriteString("\x1b[?2026h")

	prevViewportBottom := prevViewportTop + height - 1
	moveTargetRow := firstChanged
	if appendStart {
		moveTargetRow = firstChanged - 1
	}

	if moveTargetRow > prevViewportBottom {
		currentScreenRow := hardwareCursorRow - prevViewportTop
		if currentScreenRow < 0 {
			currentScreenRow = 0
		}
		if currentScreenRow > height-1 {
			currentScreenRow = height - 1
		}
		moveToBottom := height - 1 - currentScreenRow
		if moveToBottom > 0 {
			buf.WriteString("\x1b[" + itoa(moveToBottom) + "B")
		}
		scroll := moveTargetRow - prevViewportBottom
		for i := 0; i < scroll; i++ {
			buf.WriteString("\r\n")
		}
		prevViewportTop += scroll
		viewportTop += scroll
		hardwareCursorRow = moveTargetRow
	}

	lineDiff := computeLineDiff(moveTargetRow)
	if lineDiff > 0 {
		buf.WriteString("\x1b[" + itoa(lineDiff) + "B")
	} else if lineDiff < 0 {
		buf.WriteString("\x1b[" + itoa(-lineDiff) + "A")
	}

	if appendStart {
		buf.WriteString("\r\n")
	} else {
		buf.WriteString("\r")
	}

	renderEnd := min(lastChanged, len(newLines)-1)
	for i := firstChanged; i <= renderEnd; i++ {
		if i > firstChanged {
			buf.WriteString("\r\n")
		}
		buf.WriteString("\x1b[2K")
		buf.WriteString(newLines[i])
	}

	finalCursorRow := renderEnd

	// Flame shrink path: \r\n\x1b[2K per removed line, then cursor up.
	if len(r.previousLines) > len(newLines) {
		if renderEnd < len(newLines)-1 {
			moveDown := len(newLines) - 1 - renderEnd
			buf.WriteString("\x1b[" + itoa(moveDown) + "B")
			finalCursorRow = len(newLines) - 1
		}
		extraLines := len(r.previousLines) - len(newLines)
		for i := len(newLines); i < len(r.previousLines); i++ {
			buf.WriteString("\r\n\x1b[2K")
		}
		if extraLines > 0 {
			buf.WriteString("\x1b[" + itoa(extraLines) + "A")
		}
	}

	buf.WriteString("\x1b[?2026l")
	r.Terminal.Write(buf.String())

	r.hardwareCursorRow = finalCursorRow
	r.maxLinesRendered = max(r.maxLinesRendered, len(newLines))
	bufferLength := max(height, len(newLines))
	r.previousViewportTop = max(0, bufferLength-height)
	r.previousLines = append([]string(nil), newLines...)
	r.previousWidth = width
	r.previousHeight = height
}

func prefixEqual(a, b []string, n int) bool {
	if n <= 0 {
		return false
	}
	if len(a) < n || len(b) < n {
		return false
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func clampLines(lines []string, width int) []string {
	out := make([]string, len(lines))
	for i, line := range lines {
		if term.VisibleWidth(line) > width {
			out[i] = term.TruncateWidth(line, width)
		} else {
			out[i] = line
		}
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var digits [16]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	s := string(digits[i:])
	if neg {
		return "-" + s
	}
	return s
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
