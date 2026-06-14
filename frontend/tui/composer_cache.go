package tui

import (
	"strings"

	"github.com/enough/enough/backend/auth"
)

type composerRenderCache struct {
	value            string
	cursor           int
	width            int
	mode             composerMode
	running          bool
	compacting       bool
	thinkingLevel    string
	connected        bool
	attachmentsCount int
	lines            []string
}

func (a *App) composerLines(width int) []string {
	c := &a.composerCache
	value := a.editor.Value()
	cursor := a.editor.Cursor()
	connected := auth.Connected()
	thinking := string(a.thinkingLevel)

	if c.width == width &&
		c.value == value &&
		c.cursor == cursor &&
		c.mode == a.mode &&
		c.running == a.running &&
		c.compacting == a.compacting &&
		c.thinkingLevel == thinking &&
		c.connected == connected &&
		c.attachmentsCount == len(a.pendingAttachments) &&
		c.lines != nil {
		return c.lines
	}

	composer := a.composerStyle().
		Width(width - 2).
		Render(a.renderTaskInput())
	if composer == "" {
		c.lines = nil
	} else {
		c.lines = clampSplitLines(strings.Split(composer, "\n"), width)
	}
	c.value = value
	c.cursor = cursor
	c.width = width
	c.mode = a.mode
	c.running = a.running
	c.compacting = a.compacting
	c.thinkingLevel = thinking
	c.connected = connected
	c.attachmentsCount = len(a.pendingAttachments)
	return c.lines
}
