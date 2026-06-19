package tui

import (
	"strings"
	"unicode"
)

type editorSnapshot struct {
	runes  []rune
	cursor int
}

type Editor struct {
	runes      []rune
	cursor     int // rune index
	limit      int // 0 = unlimited
	undoStack  []editorSnapshot
	lastAction string
}

func NewEditor(limit int) Editor {
	return Editor{limit: limit}
}

// NewTaskEditor is the main composer input with no character limit.
func NewTaskEditor() Editor {
	return NewEditor(0)
}

func (e *Editor) Value() string {
	return string(e.runes)
}

func (e *Editor) Runes() []rune {
	return e.runes
}

func (e *Editor) SetValue(v string) {
	newRunes := []rune(v)
	if len(newRunes) == 0 {
		e.ClearUndo()
	} else if string(e.runes) != v {
		e.PushUndo()
	}
	e.runes = newRunes
	e.clampCursor()
	e.lastAction = "set"
}

func (e *Editor) Cursor() int {
	return e.cursor
}

func (e *Editor) Insert(r rune) {
	if e.limit > 0 && len(e.runes) >= e.limit {
		return
	}
	// Undo coalescing: consecutive non-space typing coalesces into one undo snapshot
	if unicode.IsSpace(r) || e.lastAction != "insert" {
		e.PushUndo()
	}
	e.lastAction = "insert"

	pos := e.cursor
	if pos > len(e.runes) {
		pos = len(e.runes)
	}
	e.runes = append(e.runes[:pos], append([]rune{r}, e.runes[pos:]...)...)
	e.cursor = pos + 1
}

func (e *Editor) Backspace() {
	if e.cursor == 0 {
		return
	}
	e.PushUndo()
	e.lastAction = "delete"

	pos := e.cursor - 1
	e.runes = append(e.runes[:pos], e.runes[pos+1:]...)
	e.cursor = pos
}

func (e *Editor) Delete() {
	if e.cursor >= len(e.runes) {
		return
	}
	e.PushUndo()
	e.lastAction = "delete"

	e.runes = append(e.runes[:e.cursor], e.runes[e.cursor+1:]...)
}

func (e *Editor) MoveLeft() {
	e.lastAction = "move"
	if e.cursor > 0 {
		e.cursor--
	}
}

func (e *Editor) MoveRight() {
	e.lastAction = "move"
	if e.cursor < len(e.runes) {
		e.cursor++
	}
}

func (e *Editor) Home() {
	e.lastAction = "move"
	e.cursor = 0
}

func (e *Editor) End() {
	e.lastAction = "move"
	e.cursor = len(e.runes)
}

func (e *Editor) clampCursor() {
	n := len(e.runes)
	if e.cursor > n {
		e.cursor = n
	}
	if e.cursor < 0 {
		e.cursor = 0
	}
}

func (e *Editor) InsertPaste(text string) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.ReplaceAll(text, "\n", " ")
	
	// Paste is considered atomic for undo
	if len(text) > 0 {
		e.PushUndo()
	}
	e.lastAction = "paste"

	for _, r := range text {
		if e.limit > 0 && len(e.runes) >= e.limit {
			break
		}
		pos := e.cursor
		if pos > len(e.runes) {
			pos = len(e.runes)
		}
		e.runes = append(e.runes[:pos], append([]rune{r}, e.runes[pos:]...)...)
		e.cursor = pos + 1
	}
}

// PushUndo saves a copy of current runes and cursor for undo.
func (e *Editor) PushUndo() {
	snapRunes := make([]rune, len(e.runes))
	copy(snapRunes, e.runes)
	e.undoStack = append(e.undoStack, editorSnapshot{
		runes:  snapRunes,
		cursor: e.cursor,
	})
	if len(e.undoStack) > 100 {
		e.undoStack = e.undoStack[1:]
	}
}

// ClearUndo clears the undo history.
func (e *Editor) ClearUndo() {
	e.undoStack = nil
	e.lastAction = ""
}

// Undo restores the previous editor state from the undo stack.
func (e *Editor) Undo() {
	if len(e.undoStack) == 0 {
		return
	}
	lastIdx := len(e.undoStack) - 1
	snap := e.undoStack[lastIdx]
	e.undoStack = e.undoStack[:lastIdx]

	e.runes = snap.runes
	e.cursor = snap.cursor
	e.lastAction = "undo"
}

func isWhitespace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r'
}

func isPunctuation(r rune) bool {
	switch r {
	case '(', ')', '{', '}', '[', ']', '<', '>', '.', ',', ';', ':', '\'', '"', '!', '?', '+', '-', '=', '*', '/', '\\', '|', '&', '%', '^', '$', '#', '@', '~', '`':
		return true
	default:
		return false
	}
}

// MoveWordLeft moves the cursor to the start of the previous word.
func (e *Editor) MoveWordLeft() {
	e.lastAction = "move"
	if e.cursor <= 0 {
		return
	}
	pos := e.cursor

	// 1. Skip trailing spaces
	for pos > 0 && isWhitespace(e.runes[pos-1]) {
		pos--
	}

	if pos > 0 {
		if isPunctuation(e.runes[pos-1]) {
			// 2. Skip punctuation run
			for pos > 0 && isPunctuation(e.runes[pos-1]) {
				pos--
			}
		} else {
			// 3. Skip word run (not space, not punctuation)
			for pos > 0 && !isWhitespace(e.runes[pos-1]) && !isPunctuation(e.runes[pos-1]) {
				pos--
			}
		}
	}

	e.cursor = pos
}

// MoveWordRight moves the cursor to the start of the next word.
func (e *Editor) MoveWordRight() {
	e.lastAction = "move"
	n := len(e.runes)
	if e.cursor >= n {
		return
	}
	pos := e.cursor

	// 1. Skip leading spaces
	for pos < n && isWhitespace(e.runes[pos]) {
		pos++
	}

	if pos < n {
		if isPunctuation(e.runes[pos]) {
			// 2. Skip punctuation run
			for pos < n && isPunctuation(e.runes[pos]) {
				pos++
			}
		} else {
			// 3. Skip word run (not space, not punctuation)
			for pos < n && !isWhitespace(e.runes[pos]) && !isPunctuation(e.runes[pos]) {
				pos++
			}
		}
	}

	e.cursor = pos
}

// DeleteWordBackward deletes the word behind the cursor.
func (e *Editor) DeleteWordBackward() {
	if e.cursor <= 0 {
		return
	}
	e.PushUndo()
	e.lastAction = "delete"

	pos := e.cursor
	// 1. Skip trailing spaces
	for pos > 0 && isWhitespace(e.runes[pos-1]) {
		pos--
	}

	if pos > 0 {
		if isPunctuation(e.runes[pos-1]) {
			// 2. Skip punctuation run
			for pos > 0 && isPunctuation(e.runes[pos-1]) {
				pos--
			}
		} else {
			// 3. Skip word run (not space, not punctuation)
			for pos > 0 && !isWhitespace(e.runes[pos-1]) && !isPunctuation(e.runes[pos-1]) {
				pos--
			}
		}
	}

	e.runes = append(e.runes[:pos], e.runes[e.cursor:]...)
	e.cursor = pos
}

// DeleteWordForward deletes the word in front of the cursor.
func (e *Editor) DeleteWordForward() {
	n := len(e.runes)
	if e.cursor >= n {
		return
	}
	e.PushUndo()
	e.lastAction = "delete"

	pos := e.cursor
	// 1. Skip leading spaces
	for pos < n && isWhitespace(e.runes[pos]) {
		pos++
	}

	if pos < n {
		if isPunctuation(e.runes[pos]) {
			// 2. Skip punctuation run
			for pos < n && isPunctuation(e.runes[pos]) {
				pos++
			}
		} else {
			// 3. Skip word run (not space, not punctuation)
			for pos < n && !isWhitespace(e.runes[pos]) && !isPunctuation(e.runes[pos]) {
				pos++
			}
		}
	}

	e.runes = append(e.runes[:e.cursor], e.runes[pos:]...)
}

// DeleteToLineStart deletes from the cursor position to the start of the editor.
func (e *Editor) DeleteToLineStart() {
	if e.cursor <= 0 {
		return
	}
	e.PushUndo()
	e.lastAction = "delete"

	e.runes = e.runes[e.cursor:]
	e.cursor = 0
}

// DeleteToLineEnd deletes from the cursor position to the end of the editor.
func (e *Editor) DeleteToLineEnd() {
	if e.cursor >= len(e.runes) {
		return
	}
	e.PushUndo()
	e.lastAction = "delete"

	e.runes = e.runes[:e.cursor]
}
