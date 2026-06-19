//go:build !windows

package fslock

import (
	"os"
	"syscall"
)

// Lock acquires an exclusive lock on the file descriptor using syscall.Flock.
func Lock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
}

// Unlock releases the lock on the file descriptor using syscall.Flock.
func Unlock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
