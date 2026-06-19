//go:build unix

package term

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// DrainInput discards pending tty input (terminal query replies, stray escapes)
// so they are not delivered to the shell after Enough exits.
func DrainInput(fd int, totalTimeout time.Duration) {
	if fd <= 0 {
		return
	}
	_, _ = os.Stdout.Write([]byte("\x1b[<u\x1b[>4;0m"))
	deadline := time.Now().Add(totalTimeout)
	idleUntil := time.Now().Add(60 * time.Millisecond)
	buf := make([]byte, 256)
	for time.Now().Before(deadline) {
		wait := time.Until(idleUntil)
		if wait <= 0 {
			return
		}
		if wait > 50*time.Millisecond {
			wait = 50 * time.Millisecond
		}
		poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		n, err := unix.Poll(poll, int(wait.Milliseconds()))
		if err != nil {
			return
		}
		if n == 0 || poll[0].Revents&unix.POLLIN == 0 {
			if time.Now().After(idleUntil) {
				return
			}
			continue
		}
		f := os.NewFile(uintptr(fd), "tty")
		if f == nil {
			return
		}
		_ = f.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
		got, readErr := f.Read(buf)
		_ = f.Close()
		if got > 0 {
			idleUntil = time.Now().Add(60 * time.Millisecond)
			continue
		}
		var netErr interface{ Timeout() bool }
		if errors.As(readErr, &netErr) && netErr.Timeout() {
			if time.Now().After(idleUntil) {
				return
			}
			continue
		}
		return
	}
}
