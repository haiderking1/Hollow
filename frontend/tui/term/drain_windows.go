//go:build windows

package term

import (
	"errors"
	"os"
	"time"
)

// DrainInput discards pending tty input on Windows using SetReadDeadline on Stdin.
func DrainInput(fd int, totalTimeout time.Duration) {
	if fd <= 0 {
		return
	}
	_, _ = os.Stdout.Write([]byte("\x1b[<u\x1b[>4;0m"))

	deadline := time.Now().Add(totalTimeout)
	idleUntil := time.Now().Add(60 * time.Millisecond)
	buf := make([]byte, 256)

	f := os.Stdin

	for time.Now().Before(deadline) {
		if time.Now().After(idleUntil) {
			return
		}

		readDeadline := time.Now().Add(50 * time.Millisecond)
		if readDeadline.After(idleUntil) {
			readDeadline = idleUntil
		}

		err := f.SetReadDeadline(readDeadline)
		if err != nil {
			// If SetReadDeadline is not supported, we shouldn't block indefinitely.
			// Just return to avoid hanging on drain.
			return
		}

		n, err := f.Read(buf)

		// Reset the deadline
		_ = f.SetReadDeadline(time.Time{})

		if n > 0 {
			idleUntil = time.Now().Add(60 * time.Millisecond)
			continue
		}

		if err != nil {
			var netErr interface{ Timeout() bool }
			if errors.As(err, &netErr) && netErr.Timeout() {
				if time.Now().After(idleUntil) {
					return
				}
				continue
			}
			return
		}
	}
}
