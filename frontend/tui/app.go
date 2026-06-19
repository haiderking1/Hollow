package tui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/enough/enough/backend/agent"
	"github.com/enough/enough/backend/approval"
	"github.com/enough/enough/backend/auth"
	"github.com/enough/enough/backend/config"
	"github.com/enough/enough/backend/core"
	"github.com/enough/enough/backend/enoughhome"
	"github.com/enough/enough/backend/imageutil"
	"github.com/enough/enough/backend/opencode"
	"github.com/enough/enough/backend/session"
	"github.com/enough/enough/frontend/tui/flame"
	"github.com/enough/enough/frontend/tui/markdown"
	"github.com/enough/enough/frontend/tui/term"
)

type pendingAttachment struct {
	id     string
	path   string // stored on disk under ~/.enough/attachments/{sessionID}/{uuid}.{ext}
	mime   string
	width  int
	height int
}

type queuedMessage struct {
	text        string
	attachments []agent.UserAttachment
}

type App struct {
	term     *term.Terminal
	renderer *flame.Renderer
	styles   Styles

	width  int
	height int

	mode composerMode

	editor      Editor
	messages    []chatMsg
	slashCursor int

	sessionPickerItems         []session.Info
	sessionPickerCursor        int
	sessionPickerAll           bool
	sessionPickerConfirmDelete string
	sessionPickerStatus        string

	treePickerNodes   []FlatTreeNode
	treePickerCursor  int
	treePickerConfirm int
	treePickerChoice  int
	treePickerTarget  string

	modelRegistry             *opencode.Registry
	modelPickerFilter         string
	modelPickerProviderCursor int
	modelPickerCursor         int
	modelPickerFocus          modelPickerFocus
	modelPickerStatus         string
	modelPickerThinking       opencode.ThinkingLevel

	connectPickerCursor   int
	connectPickerStatus   string
	connectTargetProvider string
	codexOAuthCancel      context.CancelFunc

	pluginsPickerTab      int
	pluginsPickerCursor   int
	pluginsPickerFocus    pluginsPickerFocus
	pluginsPickerFilter   string
	pluginsPickerStatus   string
	pluginsPendingEntryID string

	running                  bool
	compacting               bool
	loop                     loopState
	workflow                 workflowState
	forceAssistantBubble     bool
	compactionLabel          string
	compactionFrame          int
	activityPhase            activityPhase
	activityFrame            int
	activityWordIndex        int
	nextActivityWordIndex    int
	lastActivityWordIndex    int
	activityStreamSegments   int
	activityStartedAt        time.Time
	toolSpinnerFrame         int
	compactionQueuedMessages []queuedMessage
	pendingAttachments       []pendingAttachment
	agentCh                  <-chan core.Event
	workflowCh               <-chan core.Event
	agent                    *agent.Agent
	session                  *session.Manager

	thinkingLevel opencode.ThinkingLevel
	hideThinking  bool
	toolsExpanded bool

	chatRevision  uint64
	chatCache     chatRenderCache
	chatBlocks    chatBlockCache
	footerCache   footerRenderCache
	composerCache composerRenderCache

	greeted bool
	quit    bool
	lastSigintTime time.Time

	renderTimer    *time.Timer
	renderPending  bool
	lastRenderAt   time.Time

	mu       sync.Mutex
	renderCh chan struct{}

	// notifyCh carries out-of-band system lines (background review and
	// curator summaries) from goroutines that outlive a turn's event
	// channel. Drained by the main loop.
	notifyCh chan string

	// lastActiveAt is the end of the last agent activity, feeding the
	// curator's min-idle gate.
	lastActiveAt time.Time

	preloadedSkills []string

	writeApprovalSubsystem string
	writeApprovalID        string
	writeApprovalRecord    *approval.PendingRecord
	writeApprovalShowDiff  bool
	writeApprovalStatus    string
	writeApprovalQueue     []writeApprovalItem

	approvalPromptCh chan writeApprovalItem

	stdin *stdinBuffer
}

func newApp(t *term.Terminal) *App {
	return &App{
		term:                  t,
		renderer:              flame.NewRenderer(t),
		styles:                NewStyles(),
		editor:                NewTaskEditor(),
		lastActivityWordIndex: -1,
		renderCh:              make(chan struct{}, 1),
		notifyCh:              make(chan string, 16),
		approvalPromptCh:      make(chan writeApprovalItem, 16),
		modelRegistry:         opencode.DefaultRegistry(),
		lastActiveAt:          time.Now(),
	}
}

// notifyAsync delivers a system line from any goroutine. Non-blocking; the
// main loop drains notifyCh and renders.
func (a *App) notifyAsync(text string) {
	select {
	case a.notifyCh <- text:
	default:
	}
	a.requestRender()
}

// refreshCellDimensions sets the terminal cell pixel size used for image
// scaling via TIOCGWINSZ. When the terminal does not report pixel geometry,
// the default in image_protocol.go is used (no CSI queries — those leak).
func (a *App) refreshCellDimensions() {
	if w, h := a.term.CellPixels(); w > 0 && h > 0 {
		markdown.SetCellDimensions(markdown.CellDimensions{WidthPx: w, HeightPx: h})
	}
}

func (a *App) run() error {
	a.width = a.term.Columns()
	a.height = a.term.Rows()

	inputCh := make(chan []byte, 32)
	a.stdin = newStdinBuffer(func(seq []byte) {
		if len(seq) > 0 && markdown.HandleTerminalResponse(seq) {
			return
		}
		if len(seq) > 0 && kittyResponseRegex.Match(seq) {
			SetKittyProtocolActive(true)
			_, _ = os.Stdout.Write([]byte("\x1b[>7u"))
			return
		}
		a.dispatchKeyInput(seq)
	}, func(pasteContent string) {
		a.dispatchKeyInput([]byte(bracketedPasteStart + pasteContent + bracketedPasteEnd))
	})
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
		a.refreshCellDimensions()
		a.requestRenderNow()
	}); err != nil {
		return err
	}
	a.refreshCellDimensions()

	// Query Kitty keyboard protocol support
	_, _ = os.Stdout.Write([]byte("\x1b[?u"))
	go func() {
		time.Sleep(150 * time.Millisecond)
		if !IsKittyProtocolActive() {
			_, _ = os.Stdout.Write([]byte("\x1b[>4;2m"))
		}
	}()
	defer func() {
		a.shutdown()
		a.stopRenderTimer()
		a.renderer.Stop()
		a.term.Stop()
	}()

	if err := a.initSession(); err != nil {
		a.appendMessage("error", "session: "+err.Error())
	}

	a.loadThinkingSettings()
	if cfg, err := config.Load(); err == nil && cfg.Workflows != nil {
		a.workflow.ultracode = cfg.Workflows.Ultracode
		if cfg.Workflows.AltScreen && os.Getenv("TMUX") != "" {
			a.appendMessage("system", "alt-screen + tmux: use tmux copy mode (Ctrl+b [); see docs/terminal.md")
		}
	}
	a.startModelFetch()

	if !auth.Connected() {
		a.appendMessage("system", "not connected — type / to connect")
	}
	a.greeted = true
	if lines, prefix := a.buildLines(); len(lines) > 0 {
		a.renderer.Render(lines, prefix)
	}



	compactionTick := time.NewTicker(80 * time.Millisecond)
	defer compactionTick.Stop()

	// Curator: session-start check (static gates only) plus a periodic idle
	// tick. Runs in the background; summaries arrive via notifyCh.
	if cfg, err := config.LoadRuntime(); err == nil && auth.Connected() {
		go agent.MaybeRunCurator(cfg, -1, a.notifyAsync)
	}
	curatorTick := time.NewTicker(10 * time.Minute)
	defer curatorTick.Stop()

	for !a.quit {
		a.mu.Lock()
		agentCh := a.agentCh
		workflowCh := a.workflowCh
		a.mu.Unlock()

		select {
		case data := <-inputCh:
			a.handleInput(data)

		case <-a.stdin.flushCh:
			flushed := a.stdin.Flush()
			for _, seq := range flushed {
				if len(seq) > 0 && markdown.HandleTerminalResponse(seq) {
					continue
				}
				if len(seq) > 0 && kittyResponseRegex.Match(seq) {
					SetKittyProtocolActive(true)
					_, _ = os.Stdout.Write([]byte("\x1b[>7u"))
					continue
				}
				a.dispatchKeyInput(seq)
			}
			a.requestRender()

		case e, ok := <-agentCh:
			if !ok {
				a.finishAgentRun()
			} else {
				a.handleAgentEvent(e)
				for {
					select {
					case e, ok = <-agentCh:
						if !ok {
							a.finishAgentRun()
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

		case e, ok := <-workflowCh:
			if !ok {
				a.finishWorkflowRun()
			} else {
				a.handleWorkflowEvent(e)
				for {
					select {
					case e, ok = <-workflowCh:
						if !ok {
							a.finishWorkflowRun()
							goto workflowEventsDone
						}
						a.handleWorkflowEvent(e)
					default:
						goto workflowEventsDone
					}
				}
			}
		workflowEventsDone:
			a.requestRender()

		case text := <-a.notifyCh:
			a.appendMessage("system", text)
			a.bumpChat()
			a.requestRender()

		case item := <-a.approvalPromptCh:
			a.promptWriteApproval(item)
			a.requestRender()

		case <-curatorTick.C:
			a.mu.Lock()
			running := a.running
			idleFor := time.Since(a.lastActiveAt)
			a.mu.Unlock()
			if !running && auth.Connected() {
				if cfg, err := config.LoadRuntime(); err == nil {
					go agent.MaybeRunCurator(cfg, idleFor, a.notifyAsync)
				}
			}

		case <-a.renderCh:
			a.mu.Lock()
			lines, prefix := a.buildLines()
			a.mu.Unlock()
			a.renderer.Render(lines, prefix)

		case <-compactionTick.C:
			a.mu.Lock()
			compacting := a.compacting
			if compacting {
				a.compactionFrame++
			}
			animatingActivity := a.agentActivityVisible()
			if animatingActivity {
				a.tickAgentActivity()
			}
			animatingTools := a.hasAnimatingTools()
			if animatingTools {
				a.toolSpinnerFrame++
			}
			a.mu.Unlock()
			if compacting || animatingActivity || animatingTools {
				a.requestRender()
			}
		}
	}

	return nil
}

func (a *App) initSession() error {
	sm, err := session.StartNew("")
	if err != nil {
		return err
	}
	a.session = sm
	a.bumpChat()
	return nil
}

func (a *App) ensureAgent(cfg config.Runtime) *agent.Agent {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.agent == nil {
		a.agent = agent.New(cfg, "", a.session)
		a.agent.SetNotify(a.notifyAsync)
		a.agent.SetApprovalPrompt(a.approvalPromptAsync)
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
		if msg, ok := chatMsgFromSessionLine(line, true); ok {
			a.messages = append(a.messages, msg)
		}
	}
	a.bumpChat()
}

func (a *App) finishAgentRun() {
	a.mu.Lock()
	a.running = false
	a.lastActiveAt = time.Now()
	a.agentCh = nil
	a.stopAgentActivity()
	a.mu.Unlock()
	if a.finishWorkflowDraft() {
		return
	}
	if a.tryContinueLoop() {
		return
	}
	a.tryDrainCompactionQueue()
}

// tryDrainCompactionQueue starts the next queued user turn after compaction or
// agent idle. Processes one message at a time so attachments stay with their turn.
func (a *App) tryDrainCompactionQueue() {
	a.mu.Lock()
	running := a.running
	compacting := a.compacting
	if running || compacting || len(a.compactionQueuedMessages) == 0 {
		a.mu.Unlock()
		return
	}
	next := a.compactionQueuedMessages[0]
	a.compactionQueuedMessages = a.compactionQueuedMessages[1:]
	a.mu.Unlock()
	a.startAgent(next.text, next.attachments)
}

func (a *App) handleInput(data []byte) {
	a.stdin.Process(data)
}

var kittyResponseRegex = regexp.MustCompile(`^\x1b\[\?(\d+)u$`)

func (a *App) dispatchKeyInput(data []byte) {
	k := SeqToParsedKey(string(data))
	if k.action != keyNone {
		if a.handleKey(k) {
			a.requestRender()
			return
		}
	}
	a.requestRender()
}

func (a *App) handleKey(k parsedKey) bool {
	a.mu.Lock()
	running := a.running
	mode := a.mode
	a.mu.Unlock()

	switch k.action {
	case keyCtrlC:
		return a.handleCtrlC()
	case keyCtrlD:
		if a.handleCtrlD() {
			return true
		}
		if a.mode != modeSessionPicker && a.mode != modeModelPicker && a.mode != modeConnectPicker && a.mode != modeConnectCodex && a.mode != modePluginsPicker && a.mode != modeWorkflowPanel && a.mode != modeWorkflowApproval && a.mode != modeWorkflowSave {
			a.editor.Delete()
			a.requestRender()
			return false
		}
	}

	if mode == modeWriteApproval {
		if a.handleWriteApprovalKey(k) {
			return false
		}
		return false
	}
	if mode == modeWorkflowApproval {
		a.handleWorkflowApprovalKey(k)
		return false
	}
	if mode == modeWorkflowPanel || mode == modeWorkflowSave {
		if a.handleWorkflowPanelKey(k) {
			a.requestRender()
			return false
		}
	}

	if !running && mode == modeSessionPicker {
		if a.sessionPickerConfirmDelete != "" {
			switch k.action {
			case keyEnter:
				a.confirmSessionDelete()
				a.requestRender()
			case keyEscape:
				a.cancelSessionDeleteConfirm()
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

	if !running && mode == modeModelPicker {
		if a.handleModelPickerKey(k) {
			return false
		}
	}

	if !running && mode == modeConnectPicker {
		if a.handleConnectPickerKey(k) {
			return false
		}
	}

	if !running && mode == modePluginsPicker {
		if a.handlePluginsPickerKey(k) {
			return false
		}
	}

	if !running && mode == modeTask && k.action == keyDown && strings.TrimSpace(a.editor.Value()) == "" &&
		(a.workflow.active || a.workflow.paused) {
		a.openWorkflowPanel()
		a.workflow.panelLevel = 0
		a.requestRender()
		return false
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
		case keyEscape:
			a.dismissSlashMenu()
			a.requestRender()
			return false
		}
	}

	if !running && a.mode != modeSessionPicker && a.mode != modeModelPicker && a.mode != modeConnectPicker && a.mode != modeConnectCodex && a.mode != modePluginsPicker && a.mode != modePluginsSecret && a.mode != modeWriteApproval && a.mode != modeWorkflowApproval && a.mode != modeWorkflowPanel {
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

	loopCancel := running && isLoopCancelCommand(a.editor.Value())
	workflowControl := running && isWorkflowControlCommand(a.editor.Value())
	if k.action == keyEnter && (!running || a.compacting || loopCancel || workflowControl) && a.mode != modeSessionPicker && a.mode != modeModelPicker && a.mode != modeConnectPicker && a.mode != modeConnectCodex && a.mode != modeWriteApproval && a.mode != modeWorkflowApproval && a.mode != modeWorkflowPanel && a.mode != modeWorkflowSave {
		a.handleSubmit()
		a.requestRender()
		return false
	}

	if k.action == keyEscape {
		a.handleInterrupt()
		a.requestRender()
		return false
	}

	if a.mode == modeSessionPicker || a.mode == modeModelPicker || a.mode == modeConnectPicker || a.mode == modeConnectCodex || a.mode == modePluginsPicker || a.mode == modeWorkflowPanel || a.mode == modeWorkflowApproval {
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
		if a.editor.Value() == "" && len(a.pendingAttachments) > 0 {
			a.pendingAttachments = a.pendingAttachments[:len(a.pendingAttachments)-1]
		} else {
			a.editor.Backspace()
		}
	case keyDelete:
		a.editor.Delete()
	case keyLeft:
		a.editor.MoveLeft()
	case keyRight:
		a.editor.MoveRight()
	case keyHome, keyLineStart:
		a.editor.Home()
	case keyEnd, keyLineEnd:
		a.editor.End()
	case keyWordLeft:
		a.editor.MoveWordLeft()
	case keyWordRight:
		a.editor.MoveWordRight()
	case keyDeleteWordBackward:
		a.editor.DeleteWordBackward()
	case keyDeleteWordForward:
		a.editor.DeleteWordForward()
	case keyDeleteToLineStart:
		a.editor.DeleteToLineStart()
	case keyDeleteToLineEnd:
		a.editor.DeleteToLineEnd()
	case keyUndo:
		a.editor.Undo()
	case keyCtrlV:
		a.tryAttachClipboardImage()
	case keyCtrlShiftV, keyPaste:
		a.pasteComposerText(k.paste)
	}
}

func (a *App) pasteComposerText(bracketed string) {
	if bracketed != "" {
		a.editor.InsertPaste(bracketed)
		return
	}
	if text, err := readClipboardText(); err == nil && text != "" {
		a.editor.InsertPaste(text)
	}
}

func (a *App) buildLines() (out []string, stablePrefix int) {
	w := a.width
	if w <= 0 {
		w = 80
	}

	if chatLines := a.chatLines(w); len(chatLines) > 0 {
		out = append(out, chatLines...)
	}
	stablePrefix = len(out)

	if picker := a.renderSessionPicker(w); picker != "" {
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, clampSplitLines(strings.Split(picker, "\n"), w)...)
	}

	if picker := a.renderModelPicker(w); picker != "" {
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, clampSplitLines(strings.Split(picker, "\n"), w)...)
	}

	if picker := a.renderConnectPicker(w); picker != "" {
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, clampSplitLines(strings.Split(picker, "\n"), w)...)
	}

	if picker := a.renderPluginsPicker(w); picker != "" {
		if len(out) > 0 {
			out = append(out, "")
		}
		// Do not clamp — truncating picker lines breaks search box corners (╭╮╰╯).
		out = append(out, strings.Split(picker, "\n")...)
	}

	if picker := a.renderWriteApprovalPicker(w); picker != "" {
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, clampSplitLines(strings.Split(picker, "\n"), w)...)
	}

	if picker := a.renderWorkflowApproval(w); picker != "" {
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, clampSplitLines(strings.Split(picker, "\n"), w)...)
	}

	if panel := a.renderWorkflowPanel(w); panel != "" {
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, clampSplitLines(strings.Split(panel, "\n"), w)...)
	}

	if a.mode == modeTreePicker {
		if picker := a.renderTreePicker(w); picker != "" {
			if len(out) > 0 {
				out = append(out, "")
			}
			out = append(out, clampSplitLines(strings.Split(picker, "\n"), w)...)
		}
	}

	if loader := a.renderCompactionLoader(); loader != "" {
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, clampSplitLines([]string{loader}, w)...)
	}

	if loader := a.renderAgentActivityLoader(); loader != "" {
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, clampSplitLines([]string{loader}, w)...)
	}

	if taskLine := a.renderWorkflowTaskLine(w); taskLine != "" {
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, taskLine)
	}

	composer := a.composerLines(w)
	if len(composer) > 0 {
		out = append(out, composer...)
	}

	if menu := a.renderSlashMenu(w); menu != "" {
		out = append(out, clampSplitLines(strings.Split(menu, "\n"), w)...)
	}

	if footer := a.footerLines(w); len(footer) > 0 {
		out = append(out, clampSplitLines(footer, w)...)
	}

	h := a.height
	if h <= 0 {
		h = 24
	}
	for len(out) < h {
		out = append([]string{""}, out...)
		stablePrefix++
	}

	return out, stablePrefix
}

func (a *App) renderTaskInput() string {
	res := a.renderTaskInputRaw()
	if len(a.pendingAttachments) > 0 {
		var chips []string
		for _, att := range a.pendingAttachments {
			chips = append(chips, a.styles.InputHint.Render(fmt.Sprintf("[🖼 image (%dx%d)]", att.width, att.height)))
		}
		res += "\n" + "  " + strings.Join(chips, " ")
	}
	return res
}

func (a *App) renderTaskInputRaw() string {
	if a.mode == modePluginsPicker {
		prompt := a.styles.InputPrompt.Render("… ")
		hint := "↓/enter/tab list · esc close · type to filter"
		if a.pluginsPickerFocus == pluginsPickerFocusList {
			hint = "↑ search · esc close"
		}
		return prompt + a.styles.InputCaret.Render("▎") + "  " + a.styles.InputHint.Render(hint)
	}

	if a.mode == modeConnectCodex {
		prompt := a.styles.InputPrompt.Render("… ")
		hint := "waiting for browser sign-in · esc cancel"
		if a.connectPickerStatus != "" {
			hint = a.connectPickerStatus + " · esc cancel"
		}
		return prompt + a.styles.InputCaret.Render("▎") + "  " + a.styles.InputHint.Render(hint)
	}

	if a.mode == modeWriteApproval {
		prompt := a.styles.InputPrompt.Render("❯ ")
		hint := "y approve · n reject · d diff · esc later"
		return prompt + a.styles.InputCaret.Render("▎") + "  " + a.styles.InputHint.Render(hint)
	}
	if a.mode == modeWorkflowApproval {
		prompt := a.styles.InputPrompt.Render("❯ ")
		hint := "enter/y run · a always · v view · e edit · n/esc deny"
		return prompt + a.styles.InputCaret.Render("▎") + "  " + a.styles.InputHint.Render(hint)
	}
	if a.mode == modeWorkflowPanel {
		prompt := a.styles.InputPrompt.Render("… ")
		return prompt + a.styles.InputCaret.Render("▎") + "  " + a.styles.InputHint.Render("workflow controls · ? help · esc back")
	}

	return ""
}

func (a *App) appendMessage(role, text string) {
	text = strings.TrimSpace(text)
	if text == "" && role != "assistant" {
		return
	}
	a.messages = append(a.messages, chatMsg{role: role, text: text})
	a.bumpChat()
	a.requestRender()
}

func (a *App) ensureAssistantBubble() *chatMsg {
	if len(a.messages) == 0 || a.messages[len(a.messages)-1].role != "assistant" {
		a.messages = append(a.messages, chatMsg{role: "assistant"})
		a.bumpChat()
	}
	return &a.messages[len(a.messages)-1]
}

func (a *App) appendAssistantDelta(delta string) {
	if delta == "" {
		return
	}
	last := a.ensureAssistantBubble()
	last.text += delta
	a.bumpChat()
}

func (a *App) appendAssistantThinkingDelta(delta string) {
	if delta == "" {
		return
	}
	last := a.ensureAssistantBubble()
	last.thinking += delta
	a.bumpChat()
}

func (a *App) setLastAssistant(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if len(a.messages) > 0 && a.messages[len(a.messages)-1].role == "assistant" {
		a.messages[len(a.messages)-1].text = text
		a.bumpChat()
		return
	}
	a.messages = append(a.messages, chatMsg{role: "assistant", text: text})
	a.bumpChat()
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

	if a.mode == modePluginsSecret {
		a.savePluginsSecret(raw)
		return
	}

	if a.loop.active {
		if isLoopCancelCommand(raw) {
			a.handleSlash(raw)
		} else {
			a.appendMessage("error", "loop active — use /loop-cancel")
		}
		return
	}

	if strings.HasPrefix(raw, "/") {
		a.handleSlash(raw)
		return
	}

	if task, ok := isUltracodePrompt(raw, a.workflow.ultracode); ok {
		a.startWorkflow(task)
		return
	}

	if !auth.Connected() {
		a.appendMessage("error", "not connected — type / and pick connect")
		return
	}

	attachments, chatImages := a.takePendingAttachments()
	if raw == "" && len(attachments) == 0 {
		return
	}

	if a.compacting {
		a.compactionQueuedMessages = append(a.compactionQueuedMessages, queuedMessage{text: raw, attachments: attachments})
		a.appendUserMessage(raw, chatImages)
		a.requestRender()
		return
	}

	a.appendUserMessage(raw, chatImages)
	a.startAgent(raw, attachments)
	a.requestRender()
}

func (a *App) takePendingAttachments() ([]agent.UserAttachment, []chatImage) {
	var out []agent.UserAttachment
	var imgs []chatImage
	for _, att := range a.pendingAttachments {
		data, err := os.ReadFile(att.path)
		if err != nil {
			continue
		}
		out = append(out, agent.UserAttachment{
			MIMEType: att.mime,
			Data:     data,
		})
		imgs = append(imgs, chatImage{
			Path:     att.path,
			MIMEType: att.mime,
			Width:    att.width,
			Height:   att.height,
		})
	}
	a.pendingAttachments = nil
	return out, imgs
}

func (a *App) appendUserMessage(text string, images []chatImage) {
	text = strings.TrimSpace(text)
	if text == "" && len(images) == 0 {
		return
	}
	a.messages = append(a.messages, chatMsg{
		role:   "user",
		text:   text,
		images: images,
	})
	a.bumpChat()
	a.requestRender()
}

func (a *App) tryAttachClipboardImage() bool {
	data, mime, err := readClipboardImage()
	if err != nil || len(data) == 0 {
		return false
	}
	a.attachImage(data, mime)
	return true
}

func (a *App) attachImage(data []byte, mime string) {
	sessionID := "temp"
	if a.session != nil {
		sessionID = a.session.SessionID()
	}

	resizedData, w, h, _, _, _, err := imageutil.ResizeImage(data, mime)
	if err != nil {
		a.appendMessage("error", "Failed to resize image: "+err.Error())
		return
	}

	ext := "png"
	switch mime {
	case "image/jpeg":
		ext = "jpg"
	case "image/gif":
		ext = "gif"
	case "image/webp":
		ext = "webp"
	}

	uuidBytes := make([]byte, 16)
	_, _ = rand.Read(uuidBytes)
	uuidStr := hex.EncodeToString(uuidBytes)
	fileName := uuidStr + "." + ext

	dir := filepath.Join(enoughhome.HomeDir(), "attachments", sessionID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		a.appendMessage("error", "Failed to create attachments directory: "+err.Error())
		return
	}

	filePath := filepath.Join(dir, fileName)
	if err := os.WriteFile(filePath, resizedData, 0644); err != nil {
		a.appendMessage("error", "Failed to save attachment: "+err.Error())
		return
	}

	a.pendingAttachments = append(a.pendingAttachments, pendingAttachment{
		id:     uuidStr,
		path:   filePath,
		mime:   mime,
		width:  w,
		height: h,
	})
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
