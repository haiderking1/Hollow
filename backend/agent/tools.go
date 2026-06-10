package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type toolResult struct {
	output string
	isErr  bool
}

func (a *Agent) executeTool(name, argsJSON string) toolResult {
	switch name {
	case "read_file":
		return a.toolReadFile(argsJSON)
	case "write_file":
		return a.toolWriteFile(argsJSON)
	case "list_dir":
		return a.toolListDir(argsJSON)
	case "bash":
		return a.toolBash(argsJSON)
	default:
		return toolResult{output: fmt.Sprintf("unknown tool: %s", name), isErr: true}
	}
}

func (a *Agent) toolReadFile(argsJSON string) toolResult {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return toolResult{output: err.Error(), isErr: true}
	}

	path, err := a.resolvePath(args.Path)
	if err != nil {
		return toolResult{output: err.Error(), isErr: true}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return toolResult{output: err.Error(), isErr: true}
	}

	const max = 64_000
	out := string(data)
	if len(out) > max {
		out = out[:max] + "\n... truncated ..."
	}
	return toolResult{output: out}
}

func (a *Agent) toolWriteFile(argsJSON string) toolResult {
	var args struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return toolResult{output: err.Error(), isErr: true}
	}

	path, err := a.resolvePath(args.Path)
	if err != nil {
		return toolResult{output: err.Error(), isErr: true}
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return toolResult{output: err.Error(), isErr: true}
	}

	if err := os.WriteFile(path, []byte(args.Content), 0o644); err != nil {
		return toolResult{output: err.Error(), isErr: true}
	}

	return toolResult{output: fmt.Sprintf("wrote %d bytes to %s", len(args.Content), path)}
}

func (a *Agent) toolListDir(argsJSON string) toolResult {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return toolResult{output: err.Error(), isErr: true}
	}

	path := args.Path
	if path == "" {
		path = "."
	}

	path, err := a.resolvePath(path)
	if err != nil {
		return toolResult{output: err.Error(), isErr: true}
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return toolResult{output: err.Error(), isErr: true}
	}

	var b strings.Builder
	for _, e := range entries {
		if e.IsDir() {
			b.WriteString(e.Name())
			b.WriteString("/\n")
			continue
		}
		b.WriteString(e.Name())
		b.WriteByte('\n')
	}
	return toolResult{output: b.String()}
}

func (a *Agent) toolBash(argsJSON string) toolResult {
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return toolResult{output: err.Error(), isErr: true}
	}

	cmd := exec.Command("bash", "-lc", args.Command)
	cmd.Dir = a.workDir
	out, err := cmd.CombinedOutput()

	const max = 32_000
	text := string(out)
	if len(text) > max {
		text = text[:max] + "\n... truncated ..."
	}

	if err != nil {
		return toolResult{output: fmt.Sprintf("%v\n%s", err, text), isErr: true}
	}
	return toolResult{output: text}
}

func (a *Agent) resolvePath(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("path is required")
	}

	var abs string
	if filepath.IsAbs(p) {
		abs = filepath.Clean(p)
	} else {
		abs = filepath.Clean(filepath.Join(a.workDir, p))
	}

	workAbs, err := filepath.Abs(a.workDir)
	if err != nil {
		return "", err
	}

	if abs != workAbs && !strings.HasPrefix(abs, workAbs+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes workspace: %s", p)
	}
	return abs, nil
}
