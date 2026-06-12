package agent

import (
	"regexp"
	"strings"
)

var bashBlockedPatterns = []struct {
	re   *regexp.Regexp
	hint string
}{
	{regexp.MustCompile(`(?i)\bmpv\b`), "mpv draws video/sixel into stdout and breaks the Enough TUI"},
	{regexp.MustCompile(`(?i)--vo=(sixel|tct|caca|kitty)`), "terminal video output cannot run inside Enough"},
	{regexp.MustCompile(`(?i)\bffmpeg\b.*\bpix_fmt=sixel\b`), "sixel ffmpeg output breaks the TUI"},
	{regexp.MustCompile(`(?i)\bchafa\b`), "terminal image output breaks the TUI"},
	{regexp.MustCompile(`(?i)\bimg2sixel\b`), "sixel output breaks the TUI"},
	{regexp.MustCompile(`(?i)\bviu\b`), "terminal image output breaks the TUI"},
	{regexp.MustCompile(`(?i)\bsxiv\b`), "terminal image viewer breaks the TUI"},
}

func bashCommandBlocked(command string) string {
	cmd := strings.TrimSpace(command)
	if cmd == "" {
		return ""
	}
	for _, p := range bashBlockedPatterns {
		if p.re.MatchString(cmd) {
			return "REJECTED: " + p.hint + ". Run it in an external terminal, not via the bash tool. Use plain-text checks (curl, tests, file inspection) here."
		}
	}
	return ""
}

// SanitizeBashOutput strips terminal escape sequences (CSI, OSC, DCS/sixel, etc.)
// so bash tool output cannot corrupt the Enough TUI. Returns cleaned text and
// whether significant binary/escape content was removed.
func SanitizeBashOutput(in string) (string, bool) {
	if in == "" {
		return "", false
	}
	var b strings.Builder
	b.Grow(len(in))
	raw := len(in)
	i := 0
	for i < len(in) {
		c := in[i]
		if c == 0x1b {
			i = skipEscapeSequence(in, i)
			continue
		}
		if c == '\n' || c == '\r' || c == '\t' || c >= 0x20 {
			b.WriteByte(c)
		}
		i++
	}
	out := b.String()
	out = stripOrphanTerminalLeaks(out)
	suppressed := raw > 32 && len(out) < raw/2
	if out == "" && raw > 0 {
		return "[terminal graphics/control output suppressed — do not run mpv/sixel/TUI apps via bash; use an external terminal]", true
	}
	if suppressed {
		out = strings.TrimRight(out, "\n") + "\n[… terminal escape sequences stripped from bash output]"
	}
	return out, suppressed
}

func skipEscapeSequence(s string, i int) int {
	if i >= len(s) || s[i] != 0x1b {
		return i + 1
	}
	i++
	if i >= len(s) {
		return i
	}
	switch s[i] {
	case '[': // CSI
		i++
		for i < len(s) && (s[i] < 0x40 || s[i] > 0x7e) {
			i++
		}
		if i < len(s) {
			i++
		}
	case ']': // OSC
		i++
		for i < len(s) {
			if s[i] == 0x07 {
				return i + 1
			}
			if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
				return i + 2
			}
			i++
		}
	case 'P': // DCS — sixel lives here
		i++
		for i < len(s) {
			if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
				return i + 2
			}
			if s[i] == 0x9c {
				return i + 1
			}
			i++
		}
	case '(', ')', '*', '+', '-', '.', '/': // two-char
		return i + 2
	default:
		return i + 1
	}
	return i
}

// stripOrphanTerminalLeaks removes CSI/mouse bytes left behind when ESC (0x1b)
// was consumed by the terminal emulator before capture — shows up as [MCX0…
func stripOrphanTerminalLeaks(in string) string {
	if in == "" {
		return in
	}
	// Mouse reports without leading ESC: \033[M + 3 bytes → [M + 3 bytes
	if strings.Count(in, "[M") >= 4 {
		var b strings.Builder
		b.Grow(len(in))
		i := 0
		for i < len(in) {
			if i+2 <= len(in) && in[i] == '[' && in[i+1] == 'M' {
				i += 2
				for j := 0; j < 3 && i < len(in); j++ {
					i++
				}
				continue
			}
			// Orphan CSI: [ params final-byte (@ through ~)
			if in[i] == '[' {
				j := i + 1
				for j < len(in) && in[j] >= 0x20 && in[j] <= 0x3f {
					j++
				}
				if j < len(in) && in[j] >= 0x40 && in[j] <= 0x7e {
					i = j + 1
					continue
				}
			}
			b.WriteByte(in[i])
			i++
		}
		clean := b.String()
		if strings.Count(in, "[M") >= 4 && len(strings.TrimSpace(clean)) < len(in)/4 {
			return "[terminal mouse/control output suppressed — do not run mpv/sixel/TUI apps via bash; use an external terminal]"
		}
		return clean
	}
	return in
}
