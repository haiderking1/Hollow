package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/enough/enough/backend/opencode"
)

const maxBashOutput = 32_000

func bashTool() opencode.Tool {
	return opencode.Tool{
		Type: "function",
		Function: opencode.ToolFunction{
			Name:        "bash",
			Description: "Run a shell command in the project workspace. Do NOT run mpv, sixel, blessed, or full-screen TUI apps — they break the Enough terminal. Use curl, tests, and plain-text commands only.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"command": {"type": "string"}
				},
				"required": ["command"]
			}`),
		},
	}
}

func (a *Agent) toolBash(ctx context.Context, id, argsJSON string) toolResult {
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return toolResult{output: err.Error(), isErr: true}
	}

	if blocked := bashCommandBlocked(args.Command); blocked != "" {
		return toolResult{output: blocked, isErr: true}
	}

	// CommandContext + the platform Cancel hook means an aborted context (ESC)
	// actually kills the running process (and its group on unix), instead of
	// the command running to completion after cancellation.
	cmd := exec.CommandContext(ctx, "bash", "-lc", args.Command)
	cmd.Dir = a.workDir
	cmd.Env = append(os.Environ(),
		"TERM=dumb",
		"NO_COLOR=1",
		"CLICOLOR=0",
		"FORCE_COLOR=0",
	)
	configureProcGroup(cmd)

	sw := &bashStreamWriter{max: maxBashOutput, onChunk: func(chunk string) {
		clean, _ := SanitizeBashOutput(chunk)
		if clean != "" {
			a.toolDelta(id, clean)
		}
	}}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return toolResult{output: err.Error(), isErr: true}
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return toolResult{output: err.Error(), isErr: true}
	}

	if err := cmd.Start(); err != nil {
		return toolResult{output: err.Error(), isErr: true}
	}

	var wg sync.WaitGroup
	wg.Add(2)
	copyOut := func(r io.Reader) {
		defer wg.Done()
		_, _ = io.Copy(sw, r)
	}
	go copyOut(stdout)
	go copyOut(stderr)

	started := time.Now()
	err = cmd.Wait()
	wg.Wait()
	duration := time.Since(started)
	text, _ := SanitizeBashOutput(sw.String())

	// Interrupted: report whatever was captured plus a clear marker rather than
	// a raw "signal: killed" error. No evidence — an interrupted run proves nothing.
	if ctx.Err() != nil {
		if text != "" && !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
		return toolResult{output: text + "[interrupted]", isErr: true}
	}

	exitCode := 0
	if err != nil {
		exitCode = -1
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exitCode = ee.ExitCode()
		}
	}
	a.recordCommandRun(args.Command, exitCode, text, duration)

	if err != nil {
		return toolResult{output: fmt.Sprintf("%v\n%s", err, text), isErr: true}
	}
	return toolResult{output: text}
}

// bashStreamWriter accumulates command output up to a cap while streaming each
// appended chunk to onChunk. The streamed chunks concatenate exactly to the
// final String(), so the live view and the persisted result stay consistent.
type bashStreamWriter struct {
	mu        sync.Mutex
	buf       strings.Builder
	max       int
	truncated bool
	onChunk   func(string)
}

const truncMarker = "\n... truncated ..."

func (w *bashStreamWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	var emit string
	if !w.truncated {
		room := w.max - w.buf.Len()
		switch {
		case room <= 0:
			w.truncated = true
			w.buf.WriteString(truncMarker)
			emit = truncMarker
		case len(p) <= room:
			clean, _ := SanitizeBashOutput(string(p))
			w.buf.WriteString(clean)
			emit = clean
		default:
			clean, _ := SanitizeBashOutput(string(p[:room]))
			w.buf.WriteString(clean)
			w.buf.WriteString(truncMarker)
			w.truncated = true
			emit = clean + truncMarker
		}
	}
	w.mu.Unlock()

	if emit != "" && w.onChunk != nil {
		w.onChunk(emit)
	}
	return len(p), nil
}

func (w *bashStreamWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}
