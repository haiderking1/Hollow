//go:build !windows

package shell

import "os/exec"

func setHideWindow(cmd *exec.Cmd) {
	// No-op on non-Windows
}
