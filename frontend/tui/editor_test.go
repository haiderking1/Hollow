package tui

import "testing"

func TestTaskEditorUnlimited(t *testing.T) {
	e := NewTaskEditor()
	for i := 0; i < 600; i++ {
		e.Insert('a')
	}
	if len(e.Runes()) != 600 {
		t.Fatalf("expected 600 runes, got %d", len(e.Runes()))
	}
}

func TestEditorLimitEnforced(t *testing.T) {
	e := NewEditor(512)
	for i := 0; i < 600; i++ {
		e.Insert('a')
	}
	if len(e.Runes()) != 512 {
		t.Fatalf("expected 512 runes, got %d", len(e.Runes()))
	}
}
