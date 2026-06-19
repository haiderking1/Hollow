package shell

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// CommandContext creates an *exec.Cmd to run Git Bash on Windows or system bash on Unix.
// It prepends the Git paths to the child env PATH on Windows, and hides the console window.
func CommandContext(ctx context.Context, command string, login bool) (*exec.Cmd, error) {
	bashExe, err := ResolveBash()
	if err != nil {
		return nil, err
	}

	var args []string
	if login {
		args = []string{"-l", "-c", command}
	} else {
		args = []string{"-c", command}
	}

	cmd := exec.CommandContext(ctx, bashExe, args...)

	// Setup base environment
	cmd.Env = append(os.Environ(),
		"TERM=dumb",
		"NO_COLOR=1",
		"CLICOLOR=0",
		"FORCE_COLOR=0",
	)

	if runtime.GOOS == "windows" {
		// Prepend Git directories to PATH on Windows (B3)
		bashAbs, err := filepath.Abs(bashExe)
		if err == nil {
			gitRoot := gitRootFromBashExe(bashAbs)
			newEntries := []string{
				filepath.Join(gitRoot, "cmd"),
				filepath.Join(gitRoot, "bin"),
				filepath.Join(gitRoot, "usr", "bin"),
			}

			// Find PATH in cmd.Env
			pathKey := "PATH"
			var pathVal string
			for i, env := range cmd.Env {
				parts := strings.SplitN(env, "=", 2)
				if len(parts) == 2 && strings.EqualFold(parts[0], "PATH") {
					pathKey = parts[0]
					pathVal = parts[1]
					// Remove existing PATH from env slice
					cmd.Env = append(cmd.Env[:i], cmd.Env[i+1:]...)
					break
				}
			}

			joinedNew := strings.Join(newEntries, ";")
			if pathVal != "" {
				joinedNew = joinedNew + ";" + pathVal
			}
			cmd.Env = append(cmd.Env, pathKey+"="+joinedNew)
		}

		// Set SysProcAttr to hide window by default (B5)
		setHideWindow(cmd)
	}

	return cmd, nil
}

// gitRootFromBashExe returns the Git for Windows install root containing cmd/, bin/, usr/.
func gitRootFromBashExe(bashAbs string) string {
	parent := filepath.Dir(bashAbs)       // .../bin or .../usr/bin
	grandparent := filepath.Dir(parent)   // .../git or .../usr
	great := filepath.Dir(grandparent)    // .../git or drive root

	switch {
	case strings.EqualFold(filepath.Base(parent), "bin") &&
		strings.EqualFold(filepath.Base(grandparent), "usr"):
		// MinGit: <root>/usr/bin/bash.exe
		return great
	case strings.EqualFold(filepath.Base(parent), "bin"):
		// PortableGit: <root>/bin/bash.exe
		return grandparent
	default:
		return great
	}
}
