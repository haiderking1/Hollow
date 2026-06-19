//go:build windows

package agent

import (
	"context"
	"os/exec"
	"strconv"
	"syscall"
	"time"
)

// configureProcGroup puts the command in its own process group (CREATE_NEW_PROCESS_GROUP)
// and hides the console window (CREATE_NO_WINDOW = 0x08000000).
func configureProcGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags = syscall.CREATE_NEW_PROCESS_GROUP | 0x08000000
	cmd.Cancel = func() error {
		return killProcessGroup(cmd)
	}
}

// killProcessGroup terminates the process tree using taskkill /T /F.
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	pid := cmd.Process.Pid

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	killCmd := exec.CommandContext(ctx, "taskkill", "/PID", strconv.Itoa(pid), "/T", "/F")
	if killCmd.SysProcAttr == nil {
		killCmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	killCmd.SysProcAttr.CreationFlags = 0x08000000

	err := killCmd.Run()
	if err != nil {
		return cmd.Process.Kill()
	}
	return nil
}
