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

func TestEditorMovements(t *testing.T) {
	e := NewTaskEditor()
	e.SetValue("hello world, go-lang developer")

	// Set cursor at the end
	e.End()
	if e.Cursor() != 30 {
		t.Fatalf("expected cursor at 30, got %d", e.Cursor())
	}

	// MoveWordLeft: from end to start of "developer"
	e.MoveWordLeft()
	if e.Cursor() != 21 {
		t.Fatalf("expected cursor at 21, got %d (word: %s)", e.Cursor(), string(e.Runes()[e.Cursor():]))
	}

	// MoveWordLeft: from "developer" to "developer" start (should skip space, then move to start of "lang")
	// wait, text is "go-lang developer". runes: "go" (0..2), "-" (2), "lang" (3..7), " " (7), "developer" (8..17).
	// Let's verify the exact positions:
	// "hello world, go-lang developer"
	// index of 'd' in developer is 21.
	// index of '-' in "go-lang" is 15. index of 'l' in "lang" is 16.
	// when moving left from 21: skips space ' ', finds 'g' at 20 (part of "go-lang").
	// Wait, is '-' a punctuation? Yes, so "lang" is a word, "-" is punctuation, "go" is a word.
	// Let's check MoveWordLeft:
	// From 21 (start of "developer"):
	// 1. Skips trailing spaces: ' ' at 20 is skipped, pos becomes 20.
	// 2. e.runes[19] is 'g' (word char). Skip word run "lang" -> 'g' is at 20. Wait, string is "go-lang developer":
	// Index:
	// h e l l o _ w o r l d  ,  _  g  o  -  l  a  n  g  _  d  e  v  e  l  o  p  e  r
	// 0 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20 21 22 23 24 25 26 27 28 29
	// developer starts at 21.
	// from 21, previous char is ' ' at 20. We skip space -> pos = 20.
	// previous char at pos-1 (19) is 'g'. It's a word char. So skip word run (not whitespace, not punctuation).
	// runes from 19 backwards: 'g' (19), 'n' (18), 'a' (17), 'l' (16).
	// '-' at 15 is punctuation, so it stops there. pos becomes 16 (start of "lang").
	e.MoveWordLeft()
	if e.Cursor() != 16 {
		t.Fatalf("expected cursor at 16 ('lang'), got %d (rune %q)", e.Cursor(), string(e.Runes()[e.Cursor():]))
	}

	// MoveWordLeft: from 16. previous char is '-' at 15 (punctuation).
	// 1. Skips space: none.
	// 2. previous is '-' (punctuation). Skip punctuation run: "-" (15) -> pos becomes 15.
	e.MoveWordLeft()
	if e.Cursor() != 15 {
		t.Fatalf("expected cursor at 15 ('-'), got %d", e.Cursor())
	}

	// MoveWordLeft: from 15. previous char is 'o' (word char). Skip word run "go" -> pos becomes 13.
	e.MoveWordLeft()
	if e.Cursor() != 13 {
		t.Fatalf("expected cursor at 13 ('go'), got %d", e.Cursor())
	}

	// MoveWordRight: from 13 ("go-lang developer").
	// 1. Skip spaces: none.
	// 2. e.runes[13] is 'g' (word char). Skip word run -> pos becomes 15 (at '-').
	e.MoveWordRight()
	if e.Cursor() != 15 {
		t.Fatalf("expected cursor at 15 ('-'), got %d", e.Cursor())
	}

	// MoveWordRight: from 15.
	// 1. Skip spaces: none.
	// 2. e.runes[15] is '-' (punctuation). Skip punctuation run -> pos becomes 16.
	e.MoveWordRight()
	if e.Cursor() != 16 {
		t.Fatalf("expected cursor at 16 ('lang'), got %d", e.Cursor())
	}
}

func TestEditorDeletions(t *testing.T) {
	// Test DeleteWordBackward
	e := NewTaskEditor()
	e.SetValue("hello world, go-lang developer")
	e.End()
	e.DeleteWordBackward() // deletes "developer"
	if e.Value() != "hello world, go-lang " {
		t.Fatalf("expected 'hello world, go-lang ', got %q", e.Value())
	}

	// Test DeleteWordForward
	e.Home()
	e.MoveWordRight()      // cursor after "hello"
	e.DeleteWordForward()  // deletes " world"
	if e.Value() != "hello, go-lang " {
		t.Fatalf("expected 'hello, go-lang ', got %q", e.Value())
	}

	// Test DeleteToLineEnd
	e.SetValue("first second third")
	e.Home()
	e.MoveWordRight() // cursor at 5 (after "first")
	e.DeleteToLineEnd()
	if e.Value() != "first" {
		t.Fatalf("expected 'first', got %q", e.Value())
	}

	// Test DeleteToLineStart
	e.SetValue("first second third")
	e.End()
	e.MoveWordLeft() // cursor at 13 (before "third")
	e.DeleteToLineStart()
	if e.Value() != "third" {
		t.Fatalf("expected 'third', got %q", e.Value())
	}
}

func TestEditorUndo(t *testing.T) {
	e := NewTaskEditor()

	// Type word: coalesced
	e.Insert('a')
	e.Insert('b')
	e.Insert('c')
	if e.Value() != "abc" {
		t.Fatalf("expected 'abc', got %q", e.Value())
	}

	// Undo should restore empty string because typing "abc" was coalesced
	e.Undo()
	if e.Value() != "" {
		t.Fatalf("expected '', got %q", e.Value())
	}

	// Backspace and Delete should be undoable
	e.SetValue("hello")
	e.End()
	e.Backspace()
	if e.Value() != "hell" {
		t.Fatalf("expected 'hell', got %q", e.Value())
	}
	e.Undo()
	if e.Value() != "hello" {
		t.Fatalf("expected 'hello', got %q", e.Value())
	}
}
