package highlight

import "strings"

// TextStyle applies lipgloss or ANSI styling to prose segments.
type TextStyle struct {
	Plain  func(string) string
	Bold   func(string) string
	Italic func(string) string
}

func (s TextStyle) withDefaults() TextStyle {
	p := GruvboxDark()
	if s.Plain == nil {
		s.Plain = func(t string) string { return p.paint(p.Fg, t) }
	}
	if s.Bold == nil {
		s.Bold = func(t string) string { return p.bold(t) }
	}
	if s.Italic == nil {
		s.Italic = func(t string) string { return p.italic(t) }
	}
	return s
}

// stylizeProse converts inline markdown (`code`, **bold**, *italic*) to styled text.
func stylizeProse(text string, style TextStyle, p Palette) string {
	style = style.withDefaults()
	if text == "" {
		return ""
	}

	var out strings.Builder
	i := 0
	plainStart := 0

	flushPlain := func(until int) {
		if until > plainStart {
			out.WriteString(style.Plain(text[plainStart:until]))
		}
		plainStart = until
	}

	for i < len(text) {
		switch {
		case text[i] == '`':
			end := strings.IndexByte(text[i+1:], '`')
			if end < 0 {
				i++
				continue
			}
			end += i + 1
			flushPlain(i)
			out.WriteString(p.inlineCode(text[i+1 : end]))
			i = end + 1
			plainStart = i
		case i+2 < len(text) && text[i] == '*' && text[i+1] == '*' && text[i+2] == '*':
			end := strings.Index(text[i+3:], "***")
			if end < 0 {
				i++
				continue
			}
			end += i + 3
			flushPlain(i)
			inner := text[i+3 : end]
			out.WriteString(style.Italic(style.Bold(inner)))
			i = end + 3
			plainStart = i
		case i+1 < len(text) && text[i] == '*' && text[i+1] == '*':
			end := strings.Index(text[i+2:], "**")
			if end < 0 {
				i++
				continue
			}
			end += i + 2
			flushPlain(i)
			out.WriteString(style.Bold(text[i+2 : end]))
			i = end + 2
			plainStart = i
		case text[i] == '*':
			end := strings.IndexByte(text[i+1:], '*')
			if end < 0 {
				i++
				continue
			}
			end += i + 1
			flushPlain(i)
			out.WriteString(style.Italic(text[i+1 : end]))
			i = end + 1
			plainStart = i
		default:
			i++
		}
	}
	flushPlain(len(text))
	return out.String()
}
