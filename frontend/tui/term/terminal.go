package term

import (
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"golang.org/x/term"
)

type Terminal struct {
	mu sync.Mutex

	fd     int
	oldState *term.State
	width  int
	height int

	onInput  func([]byte)
	onResize func()
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
		fd:     fd,
		width:  w,
		height: h,
	}, nil
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

	if w, h, err := term.GetSize(t.fd); err == nil {
		t.width = w
		t.height = h
	}

	_, _ = fmt.Fprint(os.Stdout, "\x1b[?2004h") // bracketed paste
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
	if t.oldState != nil {
		_ = term.Restore(t.fd, t.oldState)
	}
}
