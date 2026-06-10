package tui

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/enough/enough/backend/agent"
	"github.com/enough/enough/backend/auth"
	"github.com/enough/enough/backend/config"
	"github.com/enough/enough/backend/core"
	"github.com/enough/enough/backend/opencode"
	"github.com/enough/enough/backend/session"
	"github.com/enough/enough/frontend/tui/flame"
	"github.com/enough/enough/frontend/tui/term"
)

type App struct {
	term     *term.Terminal
	renderer *flame.Renderer
	keys     *keyReader
	styles   Styles

	width  int
	height int

	mode composerMode

	editor      Editor
	messages    []chatMsg
	slashCursor int

	sessionPickerItems        []session.Info
	sessionPickerCursor       int
	sessionPickerAll          bool
	sessionPickerConfirmDelete string
	sessionPickerStatus        string

	treePickerNodes            []FlatTreeNode
	treePickerCursor           int
	treePickerConfirm          int
	treePickerChoice           int
	treePickerTarget           string

	running                    bool
	compacting                 bool
	compactionLabel            string
	compactionFrame            int
	compactionQueuedMessages   []string
	agentCh                    <-chan core.Event
	agent                      *agent.Agent
	session                    *session.Manager

	thinkingLevel opencode.ThinkingLevel
	hideThinking  bool
	toolsExpanded bool

	greeted bool
	quit    bool

	lastSigintTime time.Time
	escFlush       *time.Timer

	mu       sync.Mutex
	renderCh chan struct{}
}

func newApp(t *term.Terminal) *App {
	return &App{
		term:     t,
		renderer: flame.NewRenderer(t),
		keys:     newKeyReader(),
		styles:   NewStyles(),
		editor:   NewEditor(512),
		renderCh: make(chan struct{}, 1),
	}
}

func (a *App) requestRender() {
	select {
	case a.renderCh <- struct{}{}:
	default:
	}
}

func (a *App) run() error {
	a.width = a.term.Columns()
	a.height = a.term.Rows()

	inputCh := make(chan []byte, 32)
	if err := a.term.Start(func(b []byte) {
		select {
		case inputCh <- b:
		default:
		}
	}, func() {
		a.mu.Lock()
		a.width = a.term.Columns()
		a.height = a.term.Rows()
		a.mu.Unlock()
		a.requestRender()
	}); err != nil {
		return err
	}
	defer func() {
		a.renderer.Stop()
		a.term.Stop()
	}()

	if err := a.initSession(); err != nil {
		a.appendMessage("error", "session: "+err.Error())
	}

	a.loadThinkingSettings()

	if !auth.Connected() {
		a.appendMessage("system", "not connected — type / to connect")
	}
	a.greeted = true
	a.renderer.Render(a.buildLines())

	escFlush := time.NewTimer(0)
	if !escFlush.Stop() {
		<-escFlush.C
	}
	a.escFlush = escFlush

	compactionTick := time.NewTicker(80 * time.Millisecond)
	defer compactionTick.Stop()

	for !a.quit {
		a.mu.Lock()
		agentCh := a.agentCh
		a.mu.Unlock()

		select {
		case data := <-inputCh:
			a.handleInput(data)

		case <-escFlush.C:
			for _, k := range a.keys.flushPending() {
				if a.handleKey(k) {
					break
				}
			}
			a.requestRender()

		case e, ok := <-agentCh:
			if !ok {
				a.mu.Lock()
				a.running = false
				a.agentCh = nil
				a.mu.Unlock()
			} else {
				a.handleAgentEvent(e)
				for {
					select {
					case e, ok = <-agentCh:
						if !ok {
							a.mu.Lock()
							a.running = false
							a.agentCh = nil
							a.mu.Unlock()
							goto agentEventsDone
						}
						a.handleAgentEvent(e)
					default:
						goto agentEventsDone
					}
				}
			}
		agentEventsDone:
			a.requestRender()

		case <-a.renderCh:
			a.mu.Lock()
			lines := a.buildLines()
			a.mu.Unlock()
			a.renderer.Render(lines)

		case <-compactionTick.C:
			a.mu.Lock()
			compacting := a.compacting
			if compacting {
				a.compactionFrame++
			}
			a.mu.Unlock()
			if compacting {
				a.requestRender()
			}
		}
	}

	return nil
}

func (a *App) initSession() error {
	sm, err := session.ContinueRecent("")
	if err != nil {
		return err
	}
	a.session = sm

	for _, line := range sm.ChatLines() {
		a.messages = append(a.messages, chatMsg{
			role:         line.Role,
			text:         line.Text,
			thinking:     line.Thinking,
			toolName:     line.ToolName,
			toolArgs:     line.ToolArgs,
			toolResult:   line.ToolResult,
			toolError:    line.ToolError,
			tokensBefore: line.TokensBefore,
		})
	}

	return nil
}

func (a *App) ensureAgent(cfg config.Runtime) *agent.Agent {
	if a.agent == nil {
		a.agent = agent.New(cfg, "", a.session)
		return a.agent
	}
	a.agent.UpdateConfig(cfg)
	if a.session != nil {
		a.agent.LoadSession(a.session)
	}
	return a.agent
}

func (a *App) reloadChatFromSession() {
	a.messages = nil
	if a.session == nil {
		return
	}
	for _, line := range a.session.ChatLines() {
		a.messages = append(a.messages, chatMsg{
			role:         line.Role,
			text:         line.Text,
			thinking:     line.Thinking,
			toolName:     line.ToolName,
			toolArgs:     line.ToolArgs,
			toolResult:   line.ToolResult,
			toolError:    line.ToolError,
			tokensBefore: line.TokensBefore,
		})
	}
}

func (a *App) handleInput(data []byte) {
	a.stopEscFlush()

	keys, needsFlush := a.keys.feed(data)
	for _, k := range keys {
		if a.handleKey(k) {
			return
		}
	}
	if needsFlush {
		a.escFlush.Reset(escapeFlushDelay)
	}
}

func (a *App) stopEscFlush() {
	if a.escFlush == nil {
		return
	}
	if !a.escFlush.Stop() {
		select {
		case <-a.escFlush.C:
		default:
		}
	}
}

func (a *App) handleKey(k parsedKey) bool {
	a.mu.Lock()
	running := a.running
	mode := a.mode
	a.mu.Unlock()

	if k.action == keyEscape {
		a.handleInterrupt()
		a.requestRender()
		return false
	}

	if !running && mode == modeSessionPicker {
		if a.sessionPickerConfirmDelete != "" {
			if k.action == keyEnter {
				a.confirmSessionDelete()
				a.requestRender()
			}
			return false
		}
		if a.handleSessionPickerKey(k) {
			return false
		}
	}

	if !running && mode == modeTreePicker {
		if a.handleTreePickerKey(k) {
			return false
		}
	}

	switch k.action {
	case keyCtrlC:
		return a.handleCtrlC()

	case keyCtrlD:
		return a.handleCtrlD()
	}

	if !running && a.slashActive() {
		switch k.action {
		case keyUp:
			if a.slashCursor > 0 {
				a.slashCursor--
			}
			a.requestRender()
			return false
		case keyDown:
			a.slashCursor++
			a.clampSlashCursor()
			a.requestRender()
			return false
		case keyRune:
			if k.r == 'k' || k.r == 'K' {
				if a.slashCursor > 0 {
					a.slashCursor--
				}
				a.requestRender()
				return false
			}
			if k.r == 'j' || k.r == 'J' {
				a.slashCursor++
				a.clampSlashCursor()
				a.requestRender()
				return false
			}
		case keyTab:
			a.autocompleteSlash()
			a.requestRender()
			return false
		case keyEnter:
			cmds := a.filteredSlashCommands()
			if len(cmds) > 0 {
				a.clampSlashCursor()
				name := cmds[a.slashCursor].name
				a.editor.SetValue("")
				a.slashCursor = 0
				a.runSlashCommand(name)
				a.requestRender()
			}
			return false
		}
	}

	if !running && a.mode != modeSessionPicker {
		switch k.action {
		case keyShiftTab:
			a.cycleThinkingLevel()
			a.requestRender()
			return false
		case keyCtrlT:
			a.toggleThinkingVisibility()
			a.requestRender()
			return false
		case keyCtrlO:
			a.toggleToolsExpanded()
			a.requestRender()
			return false
		}
	}

	if k.action == keyEnter && (!running || a.compacting) && a.mode != modeSessionPicker {
		a.handleSubmit()
		a.requestRender()
		return false
	}

	if a.mode == modeSessionPicker {
		return false
	}

	prevFilter := a.slashFilter()
	a.applyEditorKey(k)
	if a.slashFilter() != prevFilter {
		a.slashCursor = 0
	}
	a.clampSlashCursor()
	a.requestRender()
	return false
}

func (a *App) applyEditorKey(k parsedKey) {
	switch k.action {
	case keyRune:
		a.editor.Insert(k.r)
	case keyBackspace:
		a.editor.Backspace()
	case keyDelete:
		a.editor.Delete()
	case keyLeft:
		a.editor.MoveLeft()
	case keyRight:
		a.editor.MoveRight()
	case keyHome:
		a.editor.Home()
	case keyEnd:
		a.editor.End()
	case keyPaste:
		a.editor.InsertPaste(k.paste)
	}
}

func (a *App) buildLines() []string {
	w := a.width
	if w <= 0 {
		w = 80
	}

	var lines []string

	chat := renderChat(a.styles, a.messages, w, a.hideThinking, a.toolsExpanded)
	if chat != "" {
		lines = append(lines, strings.Split(chat, "\n")...)
	}

	if menu := a.renderSlashMenu(w); menu != "" {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, strings.Split(menu, "\n")...)
	}

	if picker := a.renderSessionPicker(w); picker != "" {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, strings.Split(picker, "\n")...)
	}

	if a.mode == modeTreePicker {
		if picker := a.renderTreePicker(w); picker != "" {
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, strings.Split(picker, "\n")...)
		}
	}

	if loader := a.renderCompactionLoader(); loader != "" {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, loader)
	}

	composer := a.composerStyle().
		Width(w - 2).
		Render(a.renderTaskInput())
	lines = append(lines, strings.Split(composer, "\n")...)

	if footer := a.renderFooter(w); len(footer) > 0 {
		lines = append(lines, footer...)
	}

	// Pad top so composer + footer stay at bottom when content is short (Flame-style).
	h := a.height
	if h <= 0 {
		h = 24
	}
	for len(lines) < h {
		lines = append([]string{""}, lines...)
	}

	return lines
}

func (a *App) renderTaskInput() string {
	value := a.editor.Value()

	if a.mode == modeConnect {
		prompt := a.styles.InputPrompt.Render("key ")
		if value == "" {
			return prompt + a.styles.InputCaret.Render("▎") + a.styles.InputHint.Render(connectPlaceholder)
		}
		return a.renderTypedLine(prompt, value)
	}

	prompt := a.styles.InputPrompt.Render("❯ ")

	if a.running {
		if value == "" {
			hint := "esc interrupt · ctrl+c abort"
			if a.compacting {
				hint = "compacting... esc cancel · ctrl+c abort"
			}
			return prompt + a.styles.InputCaret.Render("▎") + "  " + a.styles.InputHint.Render(hint)
		}
		return a.renderTypedLine(prompt, value)
	}

	if value == "" {
		hint := taskPlaceholder
		if !auth.Connected() {
			hint = "type / for commands..."
		}
		return prompt + a.styles.InputCaret.Render("▎") + a.styles.InputHint.Render(hint)
	}

	return a.renderTypedLine(prompt, value)
}

func (a *App) renderTypedLine(prompt, value string) string {
	pos := a.editor.Cursor()
	runes := []rune(value)
	if pos < 0 {
		pos = 0
	}
	if pos > len(runes) {
		pos = len(runes)
	}

	before := a.styles.Text.Render(string(runes[:pos]))

	if pos == len(runes) {
		return prompt + before + a.styles.InputCaret.Render("▎")
	}

	cur := a.styles.InputCaret.Render(string(runes[pos]))
	after := a.styles.Text.Render(string(runes[pos+1:]))

	return prompt + before + cur + after
}

func (a *App) appendMessage(role, text string) {
	text = strings.TrimSpace(text)
	if text == "" && role != "assistant" {
		return
	}
	a.messages = append(a.messages, chatMsg{role: role, text: text})
	a.requestRender()
}

func (a *App) ensureAssistantBubble() *chatMsg {
	if len(a.messages) == 0 || a.messages[len(a.messages)-1].role != "assistant" {
		a.messages = append(a.messages, chatMsg{role: "assistant"})
	}
	return &a.messages[len(a.messages)-1]
}

func (a *App) appendAssistantDelta(delta string) {
	if delta == "" {
		return
	}
	last := a.ensureAssistantBubble()
	last.text += delta
}

func (a *App) appendAssistantThinkingDelta(delta string) {
	if delta == "" {
		return
	}
	last := a.ensureAssistantBubble()
	last.thinking += delta
}

func (a *App) setLastAssistant(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if len(a.messages) > 0 && a.messages[len(a.messages)-1].role == "assistant" {
		a.messages[len(a.messages)-1].text = text
		return
	}
	a.messages = append(a.messages, chatMsg{role: "assistant", text: text})
}

func (a *App) runSlashCommand(name string) {
	a.handleSlash("/" + name)
}

func (a *App) handleSubmit() {
	raw := strings.TrimSpace(a.editor.Value())
	a.editor.SetValue("")

	if a.mode == modeConnect {
		a.saveAPIKey(raw)
		return
	}

	if strings.HasPrefix(raw, "/") {
		a.handleSlash(raw)
		return
	}

	if !auth.Connected() {
		a.appendMessage("error", "not connected — type / and pick connect")
		return
	}

	if raw == "" {
		return
	}

	if a.compacting {
		a.compactionQueuedMessages = append(a.compactionQueuedMessages, raw)
		a.appendMessage("user", raw)
		a.requestRender()
		return
	}

	a.appendMessage("user", raw)
	a.startAgent(raw)
	a.requestRender()
}

func (a *App) renderTreePicker(width int) string {
	var lines []string
	if a.treePickerConfirm == 0 {
		lines = append(lines, a.styles.SlashSelected.Render(" Select branch node to navigate to: "))
		for i, node := range a.treePickerNodes {
			marker := "  "
			if i == a.treePickerCursor {
				marker = "› "
			}
			indentStr := strings.Repeat("  ", node.Indent)
			text := node.DisplayText
			if node.IsActive {
				text += " (current position)"
			}
			line := fmt.Sprintf("%s%s%s", marker, indentStr, text)
			if i == a.treePickerCursor {
				lines = append(lines, a.styles.SlashSelected.Render(line))
			} else {
				lines = append(lines, a.styles.SlashDim.Render(line))
			}
		}
		lines = append(lines, "")
		lines = append(lines, a.styles.SlashDim.Render("  ↑↓ pick   enter select   esc cancel"))
	} else if a.treePickerConfirm == 1 {
		lines = append(lines, a.styles.SlashSelected.Render(" Summarize abandoned branch? "))
		choices := []string{"No summary", "Summarize", "Summarize with custom prompt"}
		for i, choice := range choices {
			marker := "  "
			if i == a.treePickerChoice {
				marker = "› "
			}
			line := marker + choice
			if i == a.treePickerChoice {
				lines = append(lines, a.styles.SlashSelected.Render(line))
			} else {
				lines = append(lines, a.styles.SlashDim.Render(line))
			}
		}
		lines = append(lines, "")
		lines = append(lines, a.styles.SlashDim.Render("  ↑↓ pick   enter confirm   esc back"))
	}
	body := strings.Join(lines, "\n")
	return a.styles.SlashMenu.Width(width - 2).Render(body)
}
