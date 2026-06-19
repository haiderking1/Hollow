package workflow

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/enough/enough/backend/shell"
)

const maxWorkflowBashOutput = 64 * 1024

func (r *Runtime) runBash(ctx context.Context, command string) (BashResult, error) {
	if command == "" {
		return BashResult{}, errors.New("runBash command is empty")
	}
	cmd, err := shell.CommandContext(ctx, command, true)
	if err != nil {
		return BashResult{}, err
	}
	cmd.Dir = shell.ResolveSafeCwd(r.workDir)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	exitCode := 0
	if err != nil {
		exitCode = -1
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exitCode = ee.ExitCode()
		} else if ctx.Err() != nil {
			return BashResult{}, ctx.Err()
		}
	}
	result := BashResult{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: exitCode}
	combined := append(append([]byte(nil), stdout.Bytes()...), stderr.Bytes()...)
	if len(combined) > maxWorkflowBashOutput {
		hash := sha256.Sum256(combined)
		result.SHA256 = hex.EncodeToString(hash[:])
		result.Truncated = true
		dir := filepath.Join(filepath.Dir(r.snapshot.ScriptPath), "artifacts")
		_ = os.MkdirAll(dir, 0o755)
		path := filepath.Join(dir, "bash-"+result.SHA256[:16]+".log")
		_ = os.WriteFile(path, combined, 0o600)
		result.FullOutputPath = path
		if len(result.Stdout) > maxWorkflowBashOutput/2 {
			result.Stdout = result.Stdout[:maxWorkflowBashOutput/2] + "\n... truncated; full output: " + path
		}
		if len(result.Stderr) > maxWorkflowBashOutput/2 {
			result.Stderr = result.Stderr[:maxWorkflowBashOutput/2] + "\n... truncated; full output: " + path
		}
	}
	return result, nil
}
