package tui

import (
	"strings"
	"unicode/utf8"
)

type Editor struct {
	value  string
	cursor int // rune index
	limit  int
}

func NewEditor(limit int) Editor {
	return Editor{limit: limit}
}

func (e *Editor) Value() string {
	return e.value
}

func (e *Editor) SetValue(v string) {
	e.value = v
	e.clampCursor()
}

func (e *Editor) Cursor() int {
	return e.cursor
}

func (e *Editor) Insert(r rune) {
	if e.limit > 0 && utf8.RuneCountInString(e.value) >= e.limit {
		return
	}
	runes := []rune(e.value)
	pos := e.cursor
	if pos > len(runes) {
		pos = len(runes)
	}
	runes = append(runes[:pos], append([]rune{r}, runes[pos:]...)...)
	e.value = string(runes)
	e.cursor = pos + 1
}

func (e *Editor) Backspace() {
	if e.cursor == 0 {
		return
	}
	runes := []rune(e.value)
	pos := e.cursor - 1
	e.value = string(append(runes[:pos], runes[pos+1:]...))
	e.cursor = pos
}

func (e *Editor) Delete() {
	runes := []rune(e.value)
	if e.cursor >= len(runes) {
		return
	}
	e.value = string(append(runes[:e.cursor], runes[e.cursor+1:]...))
}

func (e *Editor) MoveLeft() {
	if e.cursor > 0 {
		e.cursor--
	}
}

func (e *Editor) MoveRight() {
	if e.cursor < utf8.RuneCountInString(e.value) {
		e.cursor++
	}
}

func (e *Editor) Home() {
	e.cursor = 0
}

func (e *Editor) End() {
	e.cursor = utf8.RuneCountInString(e.value)
}

func (e *Editor) clampCursor() {
	n := utf8.RuneCountInString(e.value)
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
