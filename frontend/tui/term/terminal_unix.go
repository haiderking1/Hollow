//go:build unix

package term

import (
	"os"
	"os/signal"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// CellPixels reports the terminal cell size in pixels via TIOCGWINSZ.
func (t *Terminal) CellPixels() (w, h int) {
	ws, err := unix.IoctlGetWinsize(t.fd, unix.TIOCGWINSZ)
	if err != nil || ws.Col == 0 || ws.Row == 0 || ws.Xpixel == 0 || ws.Ypixel == 0 {
		return 0, 0
	}
	return int(ws.Xpixel) / int(ws.Col), int(ws.Ypixel) / int(ws.Row)
}

func (t *Terminal) startReadAndResize() {
	go t.readLoop()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGWINCH)
	t.stopResize = make(chan struct{})

	go func() {
		for {
			select {
			case <-sig:
				if w, h, err := term.GetSize(t.fd); err == nil {
					t.mu.Lock()
					t.width = w
					t.height = h
					t.mu.Unlock()
					if t.onResize != nil {
						t.onResize()
					}
				}
			case <-t.stopResize:
				signal.Stop(sig)
				return
			}
		}
	}()
}

func (t *Terminal) stopReadAndResize() {
	if t.stopResize != nil {
		close(t.stopResize)
		t.stopResize = nil
	}
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
