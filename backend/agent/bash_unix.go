//go:build unix

package agent

import (
	"os/exec"
	"syscall"
)

// configureProcGroup puts the command in its own process group and, on context
// cancellation, kills the whole group so child processes spawned by the shell
// (pipelines, subshells) are torn down too — not just the bash parent.
//
// Pdeathsig ensures the group is SIGKILL'd if the Enough process exits without
// a clean Abort (terminal close, kill -9 on parent, etc.) so bash jobs cannot
// outlive the app and poison the next session.
func configureProcGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid:   true,
		Pdeathsig: syscall.SIGKILL,
	}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// Negative pid targets the process group.
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
