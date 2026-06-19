package tui

import (
	"testing"
	"time"
)

func TestHandleCtrlCClearsComposerDraft(t *testing.T) {
	app := &App{
		editor:  NewTaskEditor(),
		running: true,
	}
	app.editor.SetValue("draft message")

	if quit := app.handleCtrlC(); quit {
		t.Fatal("expected clear, not quit")
	}
	if app.editor.Value() != "" {
		t.Fatalf("expected composer cleared, got %q", app.editor.Value())
	}
}

func TestHandleCtrlCQuitsOnDoublePress(t *testing.T) {
	app := &App{
		editor: NewTaskEditor(),
	}

	// First press
	if quit := app.handleCtrlC(); quit {
		t.Fatal("expected first press not to quit")
	}
	if app.quit {
		t.Fatal("expected quit flag not set on first press")
	}

	// Wait 10ms (well within 500ms)
	time.Sleep(10 * time.Millisecond)

	// Second press
	if quit := app.handleCtrlC(); !quit {
		t.Fatal("expected second press to quit")
	}
	if !app.quit {
		t.Fatal("expected quit flag set on second press")
	}
}

func TestHandleCtrlCDoesNotQuitOnSlowDoublePress(t *testing.T) {
	app := &App{
		editor: NewTaskEditor(),
	}

	// First press
	if quit := app.handleCtrlC(); quit {
		t.Fatal("expected first press not to quit")
	}

	// Wait 600ms (more than 500ms)
	time.Sleep(600 * time.Millisecond)

	// Second press
	if quit := app.handleCtrlC(); quit {
		t.Fatal("expected second slow press not to quit")
	}
	if app.quit {
		t.Fatal("expected quit flag not set on slow press")
	}
}
