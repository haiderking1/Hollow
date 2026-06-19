package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestComposerTaskInput(t *testing.T) {
	app := &App{
		styles: NewStyles(),
		editor: NewTaskEditor(),
		mode:   modeTask,
	}

	// Case 1: Idle, empty input
	app.running = false
	lines := app.composerLines(80)
	if len(lines) < 3 {
		t.Fatalf("expected composer lines, got %d", len(lines))
	}
	plain := ansi.Strip(lines[1])
	if strings.Contains(plain, "❯") {
		t.Fatalf("expected idle prompt without '❯', got: %q", plain)
	}
	if strings.Contains(plain, "describe what you want done") {
		t.Fatalf("expected idle prompt without placeholder hint, got: %q", plain)
	}
	if !strings.Contains(lines[1], "\x1b[7m") {
		t.Fatalf("expected reverse-video cursor, got: %q", lines[1])
	}

	// Case 2: Running, empty input
	app.running = true
	lines = app.composerLines(80)
	plain = ansi.Strip(lines[1])
	if strings.Contains(plain, "esc interrupt") {
		t.Fatalf("expected running prompt without hint, got: %q", plain)
	}
	if !strings.Contains(lines[1], "\x1b[7m") {
		t.Fatalf("expected reverse-video cursor when empty, got: %q", lines[1])
	}

	// Case 3: Running, typed input with cursor at the end
	app.editor.SetValue("hello")
	app.editor.End()
	lines = app.composerLines(80)
	plain = ansi.Strip(lines[1])
	if !strings.Contains(plain, "hello") {
		t.Fatalf("expected running prompt to contain typed text, got: %q", plain)
	}
	if strings.Contains(plain, "esc interrupt") {
		t.Fatalf("expected running prompt NOT to contain hint when typing, got: %q", plain)
	}
	if !strings.Contains(lines[1], "\x1b[7m") {
		t.Fatalf("expected reverse-video cursor, got: %q", lines[1])
	}
}
