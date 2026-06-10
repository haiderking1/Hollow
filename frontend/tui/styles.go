package tui

import "github.com/charmbracelet/lipgloss"

type Styles struct {
	Text        lipgloss.Style
	LogDim      lipgloss.Style
	LogAccent   lipgloss.Style
	LogWarn     lipgloss.Style
	LogErr      lipgloss.Style
	LogOk       lipgloss.Style
	InputBox    lipgloss.Style
	InputPrompt lipgloss.Style
	InputHint   lipgloss.Style
	InputCaret  lipgloss.Style
	AssistBullet lipgloss.Style
	AssistText  lipgloss.Style
	ThinkingText lipgloss.Style
	AssistError lipgloss.Style
	ToolActivity lipgloss.Style
	ToolAction   lipgloss.Style
	ToolPath     lipgloss.Style
	ToolBullet   lipgloss.Style
	ToolTarget   lipgloss.Style
	ToolSub      lipgloss.Style
	ToolRunBox   lipgloss.Style
	ToolRunText  lipgloss.Style
	ToolDelta    lipgloss.Style
	ToolDeltaRemoved lipgloss.Style
	ToolMuted    lipgloss.Style
	ToolOutput   lipgloss.Style
	ToolPending  lipgloss.Style
	SlashMenu   lipgloss.Style
	SlashSelected lipgloss.Style
	SlashName   lipgloss.Style
	SlashDesc   lipgloss.Style
	SlashDim    lipgloss.Style
	CompactionSpinner lipgloss.Style
	CompactionText    lipgloss.Style
	FooterWarn        lipgloss.Style
	FooterErr         lipgloss.Style
}

func NewStyles() Styles {
	base := lipgloss.NewStyle()

	border := lipgloss.Color("#2a2a34")
	text := lipgloss.Color("#e8e8ed")
	textDim := lipgloss.Color("#6b6b78")
	accent := lipgloss.Color("#7c8cff")
	amber := lipgloss.Color("#f0b429")
	green := lipgloss.Color("#3dd68c")
	red := lipgloss.Color("#f25c5c")

	return Styles{
		Text: base.Copy().
			Foreground(text),

		LogDim: base.Copy().
			Foreground(textDim),

		LogAccent: base.Copy().
			Foreground(accent).
			Bold(true),

		LogWarn: base.Copy().
			Foreground(amber),

		LogErr: base.Copy().
			Foreground(red).
			Bold(true),

		LogOk: base.Copy().
			Foreground(green),

		InputBox: base.Copy().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(border).
			Padding(0, 1),

		InputPrompt: base.Copy().
			Foreground(textDim),

		InputHint: base.Copy().
			Foreground(textDim).
			Italic(true),

		InputCaret: base.Copy().
			Foreground(lipgloss.Color("#0d0d0f")).
			Background(lipgloss.Color("#e8e8ed")),

		AssistBullet: base.Copy().
			Foreground(text).
			Bold(true),

		AssistText: base.Copy().
			Foreground(text),

		ThinkingText: base.Copy().
			Foreground(lipgloss.Color("#8b8b9a")).
			Italic(true),

		AssistError: base.Copy().
			Foreground(red).
			Bold(true),

		ToolActivity: base.Copy().
			Foreground(textDim).
			Italic(true),

		ToolAction: base.Copy().
			Foreground(text).
			Bold(true),

		ToolPath: base.Copy().
			Foreground(lipgloss.Color("#d4d4d4")).
			Background(lipgloss.Color("#262626")).
			Padding(0, 1),

		ToolBullet: base.Copy().
			Foreground(green).
			Bold(true),

		ToolTarget: base.Copy().
			Foreground(lipgloss.Color("#737373")),

		ToolSub: base.Copy().
			Foreground(lipgloss.Color("#737373")),

		ToolRunBox: base.Copy().
			Foreground(lipgloss.Color("#a3a3a3")).
			Background(lipgloss.Color("#262626")).
			Padding(0, 1),

		ToolRunText: base.Copy().
			Foreground(lipgloss.Color("#a3a3a3")),

		ToolDelta: base.Copy().
			Foreground(lipgloss.Color("#4ade80")),

		ToolDeltaRemoved: base.Copy().
			Foreground(lipgloss.Color("#f87171")),

		ToolMuted: base.Copy().
			Foreground(lipgloss.Color("#737373")),

		ToolOutput: base.Copy().
			Foreground(textDim),

		ToolPending: base.Copy().
			Foreground(lipgloss.Color("#737373")).
			Italic(true),

		SlashMenu: base.Copy().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(border).
			Padding(0, 1),

		SlashSelected: base.Copy().
			Foreground(accent).
			Bold(true),

		SlashName: base.Copy().
			Foreground(text).
			Bold(true),

		SlashDesc: base.Copy().
			Foreground(textDim),

		SlashDim: base.Copy().
			Foreground(textDim).
			Italic(true),

		CompactionSpinner: base.Copy().
			Foreground(lipgloss.Color("#66D9EF")),

		CompactionText: base.Copy().
			Foreground(textDim),

		FooterWarn: base.Copy().
			Foreground(amber),

		FooterErr: base.Copy().
			Foreground(red),
	}
}

func thinkingBorderColor(level string) lipgloss.Color {
	switch level {
	case "high":
		return lipgloss.Color("#c084fc")
	case "xhigh":
		return lipgloss.Color("#f472b6")
	default:
		return lipgloss.Color("#2a2a34")
	}
}
