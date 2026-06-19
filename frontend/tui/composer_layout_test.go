package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/enough/enough/frontend/tui/term"
)

func TestComposerFlameRules(t *testing.T) {
	app := &App{
		styles: NewStyles(),
		width:  80,
		editor: NewTaskEditor(),
	}
	app.mode = modeTask
	app.editor.SetValue(strings.Repeat("d", 200))

	lines := app.composerLines(80)
	if len(lines) < 4 {
		t.Fatalf("expected top rule + content + bottom rule, got %d lines", len(lines))
	}

	top := ansi.Strip(lines[0])
	bottom := ansi.Strip(lines[len(lines)-1])
	if strings.Contains(top, "╭") || strings.Contains(top, "│") {
		t.Fatalf("expected flat top rule, got %q", top)
	}
	if !strings.HasPrefix(top, strings.Repeat("─", 10)) {
		t.Fatalf("expected horizontal rule, got %q", top)
	}
	if len([]rune(top)) != 80 || len([]rune(bottom)) != 80 {
		t.Fatalf("rules should span terminal width")
	}

	for i, line := range lines {
		if term.VisibleWidth(line) != 80 {
			t.Fatalf("line %d width %d, want 80", i, term.VisibleWidth(line))
		}
	}
}

func TestComposerFlameWrapWidth(t *testing.T) {
	app := &App{styles: NewStyles(), editor: NewTaskEditor(), mode: modeTask}
	app.editor.SetValue(strings.Repeat("d", 160))

	lines := app.composerLines(80)
	// layout width is 79; 160 chars -> 3 content lines + 2 rules = 5
	if len(lines) != 5 {
		t.Fatalf("expected 5 lines, got %d", len(lines))
	}
}

func TestComposerFlameCursor(t *testing.T) {
	app := &App{styles: NewStyles(), editor: NewTaskEditor(), mode: modeTask}
	app.editor.SetValue("hi")

	lines := app.composerLines(80)
	if !strings.Contains(lines[1], "\x1b[7m") {
		t.Fatalf("expected reverse-video cursor, got %q", lines[1])
	}
}
