//go:build windows

package fslock

import (
	"os"

	"golang.org/x/sys/windows"
)

// Lock acquires an exclusive lock on the file handle (blocks until acquired).
func Lock(f *os.File) error {
	ol := new(windows.Overlapped)
	return windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK,
		0,
		1,
		0,
		ol,
	)
}

// Unlock releases the exclusive lock.
func Unlock(f *os.File) error {
	ol := new(windows.Overlapped)
	return windows.UnlockFileEx(
		windows.Handle(f.Fd()),
		0,
		1,
		0,
		ol,
	)
}
