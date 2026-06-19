package term

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

type Terminal struct {
	mu sync.Mutex

	fd       int
	oldState *term.State
	width    int
	height   int

	onInput  func([]byte)
	onResize func()

	started   bool
	altScreen bool
	pauseRead bool
	pauseAck  chan struct{}

	stopResize chan struct{}
}

func New() (*Terminal, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return nil, fmt.Errorf("stdin is not a terminal")
	}

	w, h, err := term.GetSize(fd)
	if err != nil {
		return nil, err
	}

	return &Terminal{
		fd:        fd,
		width:     w,
		height:    h,
		altScreen: AltScreenRequested(),
	}, nil
}

func AltScreenRequested() bool {
	return envEnabled("ENOUGH_ALT_SCREEN") || envEnabled("ENOUGH_NO_FLICKER")
}

func envEnabled(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func (t *Terminal) Columns() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.width
}

func (t *Terminal) Rows() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.height
}

func (t *Terminal) Start(onInput func([]byte), onResize func()) error {
	t.onInput = onInput
	t.onResize = onResize

	old, err := term.MakeRaw(t.fd)
	if err != nil {
		return err
	}
	t.oldState = old
	t.started = true

	if w, h, err := term.GetSize(t.fd); err == nil {
		t.width = w
		t.height = h
	}

	if t.altScreen {
		_, _ = fmt.Fprint(os.Stdout, "\x1b[?1049h\x1b[2J\x1b[H")
	}
	_, _ = fmt.Fprint(os.Stdout, "\x1b[?2004h")
	t.hideCursor()

	t.startReadAndResize()

	DrainInput(t.fd, 200*time.Millisecond)

	return nil
}

func (t *Terminal) Write(data string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, _ = os.Stdout.WriteString(data)
}

func (t *Terminal) hideCursor() {
	t.Write("\x1b[?25l")
}

func (t *Terminal) ShowCursor() {
	t.Write("\x1b[?25h")
}

func (t *Terminal) Stop() {
	t.ShowCursor()
	_, _ = fmt.Fprint(os.Stdout, "\x1b[?2004l\x1b[<u\x1b[>4;0m")
	
	t.mu.Lock()
	t.pauseRead = true
	ack := make(chan struct{})
	t.pauseAck = ack
	alt := t.altScreen
	t.started = false
	fd := t.fd
	t.mu.Unlock()

	t.stopReadAndResize()

	select {
	case <-ack:
	case <-time.After(time.Second):
	}
	DrainInput(fd, 1000*time.Millisecond)
	if alt {
		_, _ = fmt.Fprint(os.Stdout, "\x1b[?1049l")
	}
	if t.oldState != nil {
		_ = term.Restore(fd, t.oldState)
	}
}

func (t *Terminal) SetAltScreen(enabled bool) {
	t.mu.Lock()
	if t.altScreen == enabled {
		t.mu.Unlock()
		return
	}
	t.altScreen = enabled
	started := t.started
	t.mu.Unlock()
	if !started {
		return
	}
	if enabled {
		t.Write("\x1b[?1049h\x1b[2J\x1b[H")
	} else {
		t.Write("\x1b[?1049l")
	}
}

func (t *Terminal) AltScreen() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.altScreen
}

func (t *Terminal) RunExternal(name string, args ...string) error {
	t.mu.Lock()
	t.pauseRead = true
	ack := make(chan struct{})
	t.pauseAck = ack
	alt := t.altScreen
	t.mu.Unlock()

	t.stopReadAndResize()

	select {
	case <-ack:
	case <-time.After(time.Second):
	}

	t.ShowCursor()
	_, _ = fmt.Fprint(os.Stdout, "\x1b[?2004l")
	if alt {
		_, _ = fmt.Fprint(os.Stdout, "\x1b[?1049l")
	}
	if t.oldState != nil {
		_ = term.Restore(t.fd, t.oldState)
	}

	cmd := exec.Command(name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()

	old, rawErr := term.MakeRaw(t.fd)
	if rawErr == nil {
		t.oldState = old
	}
	if alt {
		_, _ = fmt.Fprint(os.Stdout, "\x1b[?1049h\x1b[2J\x1b[H")
	}
	_, _ = fmt.Fprint(os.Stdout, "\x1b[?2004h")
	t.hideCursor()

	t.mu.Lock()
	t.pauseRead = false
	t.mu.Unlock()

	t.startReadAndResize()

	if err != nil {
		return err
	}
	return rawErr
}
