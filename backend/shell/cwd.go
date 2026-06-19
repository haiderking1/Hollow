package shell

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

var msysDriveRegex1 = regexp.MustCompile(`^/([a-zA-Z])(/.*)?$`)
var msysDriveRegex2 = regexp.MustCompile(`^/(?:cygdrive|mnt)/([a-zA-Z])(/.*)?$`)

// MsysToWindowsPath translates a Git Bash / MSYS-style POSIX path (/c/Users/x)
// to the native Windows form (C:\Users\x).
func MsysToWindowsPath(cwd string) string {
	return msysToWindowsPathInternal(cwd, runtime.GOOS == "windows")
}

func msysToWindowsPathInternal(cwd string, isWindows bool) string {
	if !isWindows || cwd == "" {
		return cwd
	}
	// Match /c/Users/... or /c
	if m := msysDriveRegex1.FindStringSubmatch(cwd); m != nil {
		drive := strings.ToUpper(m[1])
		tail := m[2]
		if tail != "" {
			tail = strings.ReplaceAll(tail, "/", "\\")
		} else {
			tail = "\\"
		}
		return drive + ":" + tail
	}
	// Match /cygdrive/c/... or /mnt/c/...
	if m := msysDriveRegex2.FindStringSubmatch(cwd); m != nil {
		drive := strings.ToUpper(m[1])
		tail := m[2]
		if tail != "" {
			tail = strings.ReplaceAll(tail, "/", "\\")
		} else {
			tail = "\\"
		}
		return drive + ":" + tail
	}
	return cwd
}

// ResolveSafeCwd returns the cwd if it exists as a directory, else walks up to
// the nearest existing ancestor. Normalizes MSYS paths on Windows.
func ResolveSafeCwd(cwd string) string {
	if runtime.GOOS == "windows" {
		cwd = MsysToWindowsPath(cwd)
	}
	if cwd != "" {
		if fi, err := os.Stat(cwd); err == nil && fi.IsDir() {
			return cwd
		}
	}
	parent := filepath.Dir(cwd)
	for parent != "" && parent != cwd {
		if fi, err := os.Stat(parent); err == nil && fi.IsDir() {
			return parent
		}
		nextParent := filepath.Dir(parent)
		if nextParent == parent {
			break
		}
		parent = nextParent
	}
	return os.TempDir()
}
