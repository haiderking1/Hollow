package tui

import (
	"strings"
	"testing"

	"github.com/enough/enough/frontend/tui/term"
)

func TestComposerLinesCache(t *testing.T) {
	app := &App{
		styles: NewStyles(),
		width:  80,
		editor: NewTaskEditor(),
	}
	app.editor.Insert('h')
	app.editor.Insert('i')

	first := app.composerLines(80)
	if len(first) == 0 {
		t.Fatal("expected composer lines")
	}

	second := app.composerLines(80)
	if len(second) != len(first) {
		t.Fatalf("cache miss changed line count: %d vs %d", len(second), len(first))
	}

	app.editor.Insert('!')
	third := app.composerLines(80)
	if app.composerCache.value != app.editor.Value() {
		t.Fatal("composer cache should track editor value")
	}
	if len(third) == 0 {
		t.Fatal("expected composer lines after edit")
	}
}

func TestComposerLinesFullWidth(t *testing.T) {
	app := &App{
		styles: NewStyles(),
		width:  40,
		editor: NewTaskEditor(),
	}
	app.editor.SetValue(strings.Repeat("d", 200))

	lines := app.composerLines(40)
	if len(lines) < 2 {
		t.Fatalf("expected wrapped composer lines, got %d", len(lines))
	}

	for i, line := range lines {
		w := term.VisibleWidth(line)
		if w != 40 {
			t.Fatalf("line %d visible width %d, want 40 (symmetric borders)", i, w)
		}
	}
}
