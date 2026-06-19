//go:build !unix && !windows

package agent

import "os/exec"

import "syscall"

// configureProcGroup is a no-op on non-unix platforms; exec.CommandContext's
// default cancellation (killing the process) still applies.
func configureProcGroup(cmd *exec.Cmd) {}

func killProcessGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
