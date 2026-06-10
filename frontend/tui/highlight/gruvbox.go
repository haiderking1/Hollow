package highlight

import "fmt"

const resetAttrs = "\033[0m"

// Palette holds Gruvbox-dark syntax colors for terminal output.
type Palette struct {
	Fg          string
	Comment     string
	Keyword     string
	String      string
	Number      string
	Function    string
	Type        string
	Punctuation string
	Special     string
	Inline      string
}

// GruvboxDark returns the classic Gruvbox dark palette.
func GruvboxDark() Palette {
	return Palette{
		Fg:          fg(235, 219, 178),
		Comment:     fg(146, 131, 116),
		Keyword:     fg(251, 73, 52),
		String:      fg(184, 187, 38),
		Number:      fg(211, 134, 155),
		Function:    fg(250, 189, 47),
		Type:        fg(250, 189, 47),
		Punctuation: fg(168, 153, 132),
		Special:     fg(142, 192, 124),
		Inline:      fg(250, 189, 47),
	}
}

func fg(r, g, b byte) string {
	return fmt.Sprintf("\033[38;2;%d;%d;%dm", r, g, b)
}

func (p Palette) paint(color, text string) string {
	if text == "" {
		return ""
	}
	return color + text + resetAttrs
}

func (p Palette) bold(text string) string {
	return "\033[1m" + p.Fg + text + resetAttrs
}

func (p Palette) italic(text string) string {
	return "\033[3m" + p.Fg + text + resetAttrs
}

func (p Palette) inlineCode(text string) string {
	return p.paint(p.Inline, text)
}
