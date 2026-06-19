package term

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
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

// CellPixels reports the terminal cell size in pixels via TIOCGWINSZ. Foot and
// most modern terminals populate the pixel fields; returns (0, 0) when they do
// not (e.g. some multiplexers), letting the caller fall back to a query/default.
func (t *Terminal) CellPixels() (w, h int) {
	ws, err := unix.IoctlGetWinsize(t.fd, unix.TIOCGWINSZ)
	if err != nil || ws.Col == 0 || ws.Row == 0 || ws.Xpixel == 0 || ws.Ypixel == 0 {
		return 0, 0
	}
	return int(ws.Xpixel) / int(ws.Col), int(ws.Ypixel) / int(ws.Row)
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

	go t.readLoop()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGWINCH)
	go func() {
		for range sig {
			if w, h, err := term.GetSize(t.fd); err == nil {
				t.mu.Lock()
				t.width = w
				t.height = h
				t.mu.Unlock()
				if t.onResize != nil {
					t.onResize()
				}
			}
		}
	}()

	return nil
}

func (t *Terminal) readLoop() {
	buf := make([]byte, 256)
	for {
		t.mu.Lock()
		paused := t.pauseRead
		if paused && t.pauseAck != nil {
			close(t.pauseAck)
			t.pauseAck = nil
		}
		t.mu.Unlock()
		if paused {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		poll := []unix.PollFd{{Fd: int32(t.fd), Events: unix.POLLIN}}
		ready, err := unix.Poll(poll, 50)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			return
		}
		if ready == 0 || poll[0].Revents&unix.POLLIN == 0 {
			continue
		}
		n, err := os.Stdin.Read(buf)
		if err != nil || n == 0 {
			return
		}
		if t.onInput != nil {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			t.onInput(chunk)
		}
	}
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
	_, _ = fmt.Fprint(os.Stdout, "\x1b[?2004l")
	t.mu.Lock()
	alt := t.altScreen
	t.started = false
	t.mu.Unlock()
	if alt {
		_, _ = fmt.Fprint(os.Stdout, "\x1b[?1049l")
	}
	if t.oldState != nil {
		_ = term.Restore(t.fd, t.oldState)
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
	if err != nil {
		return err
	}
	return rawErr
}
