package tui

import (
	"strings"
)

type Editor struct {
	runes  []rune
	cursor int // rune index
	limit  int // 0 = unlimited
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
	e.runes = []rune(v)
	e.clampCursor()
}

func (e *Editor) Cursor() int {
	return e.cursor
}

func (e *Editor) Insert(r rune) {
	if e.limit > 0 && len(e.runes) >= e.limit {
		return
	}
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
	pos := e.cursor - 1
	e.runes = append(e.runes[:pos], e.runes[pos+1:]...)
	e.cursor = pos
}

func (e *Editor) Delete() {
	if e.cursor >= len(e.runes) {
		return
	}
	e.runes = append(e.runes[:e.cursor], e.runes[e.cursor+1:]...)
}

func (e *Editor) MoveLeft() {
	if e.cursor > 0 {
		e.cursor--
	}
}

func (e *Editor) MoveRight() {
	if e.cursor < len(e.runes) {
		e.cursor++
	}
}

func (e *Editor) Home() {
	e.cursor = 0
}

func (e *Editor) End() {
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
	for _, r := range text {
		e.Insert(r)
	}
}
