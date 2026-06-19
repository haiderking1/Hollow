//go:build windows

package term

import (
	"errors"
	"os"
	"time"

	"golang.org/x/term"
)

// CellPixels returns (0, 0) on Windows.
func (t *Terminal) CellPixels() (w, h int) {
	return 0, 0
}

func (t *Terminal) startReadAndResize() {
	t.stopResize = make(chan struct{})

	// Start resize ticker (150ms)
	go func() {
		ticker := time.NewTicker(150 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if w, h, err := term.GetSize(t.fd); err == nil {
					t.mu.Lock()
					changed := (t.width != w || t.height != h)
					if changed {
						t.width = w
						t.height = h
					}
					t.mu.Unlock()
					if changed && t.onResize != nil {
						t.onResize()
					}
				}
			case <-t.stopResize:
				return
			}
		}
	}()

	go t.readLoop()
}

func (t *Terminal) stopReadAndResize() {
	if t.stopResize != nil {
		close(t.stopResize)
		t.stopResize = nil
	}
}

func (t *Terminal) readLoop() {
	buf := make([]byte, 256)
	stdin := os.NewFile(uintptr(t.fd), "stdin")
	if stdin == nil {
		return
	}
	defer stdin.Close()

	const pollInterval = 50 * time.Millisecond

	for {
		t.mu.Lock()
		paused := t.pauseRead
		ack := t.pauseAck
		if paused && ack != nil {
			close(ack)
			t.pauseAck = nil
		}
		started := t.started
		t.mu.Unlock()

		if paused || !started {
			time.Sleep(20 * time.Millisecond)
			continue
		}

		if err := stdin.SetReadDeadline(time.Now().Add(pollInterval)); err != nil {
			return
		}
		n, err := stdin.Read(buf)
		_ = stdin.SetReadDeadline(time.Time{})

		if n > 0 {
			t.mu.Lock()
			active := t.started && !t.pauseRead
			t.mu.Unlock()
			if active && t.onInput != nil {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				t.onInput(chunk)
			}
			continue
		}

		if err != nil {
			var ne interface{ Timeout() bool }
			if errors.As(err, &ne) && ne.Timeout() {
				continue // idle — re-check pauseRead
			}
			return // EOF or fatal
		}
	}
}
