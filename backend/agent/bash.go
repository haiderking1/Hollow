package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/enough/enough/backend/opencode"
	"github.com/enough/enough/backend/shell"
)

const (
	maxBashOutput        = 32_000
	exitStdioGrace       = 100 * time.Millisecond // Flame: don't hang on inherited stdio after shell exit
	bashUpdateThrottle   = 100 * time.Millisecond
)

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
	cmd, err := shell.CommandContext(ctx, args.Command, true)
	if err != nil {
		return toolResult{output: err.Error(), isErr: true}
	}
	cmd.Dir = shell.ResolveSafeCwd(a.workDir)
	configureProcGroup(cmd)

	delta := newBashDeltaEmitter(func(chunk string) {
		clean, _ := SanitizeBashOutput(chunk)
		if clean != "" {
			a.toolDelta(id, clean)
		}
	})
	defer delta.flush()

	sw := &bashStreamWriter{max: maxBashOutput, onChunk: delta.add}

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
	a.registerBashCmd(cmd)
	defer a.unregisterBashCmd(cmd)

	var wg sync.WaitGroup
	wg.Add(2)
	copyOut := func(r io.ReadCloser) {
		defer wg.Done()
		_, _ = io.Copy(sw, r)
	}
	go copyOut(stdout)
	go copyOut(stderr)

	started := time.Now()
	copyDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(copyDone)
	}()

	exitDone := make(chan error, 1)
	go func() {
		exitDone <- cmd.Wait()
	}()

	closeReadersAfterGrace := func() {
		select {
		case <-copyDone:
		case <-time.After(exitStdioGrace):
			_ = stdout.Close()
			_ = stderr.Close()
			<-copyDone
		}
	}

	var waitErr error
	select {
	case err := <-exitDone:
		waitErr = err
		closeReadersAfterGrace()
	case <-copyDone:
		waitErr = <-exitDone
	case <-ctx.Done():
		_ = killProcessGroup(cmd)
		closeReadersAfterGrace()
		waitErr = <-exitDone
	}

	sw.Finalize()
	delta.flush()
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
	if waitErr != nil {
		exitCode = -1
		var ee *exec.ExitError
		if errors.As(waitErr, &ee) {
			exitCode = ee.ExitCode()
		}
	}
	a.recordCommandRun(args.Command, exitCode, text, duration)

	if waitErr != nil {
		return toolResult{output: fmt.Sprintf("%v\n%s", waitErr, text), isErr: true}
	}
	return toolResult{output: text}
}

func (a *Agent) registerBashCmd(cmd *exec.Cmd) {
	a.activeBashMu.Lock()
	a.activeBashCmd = cmd
	a.activeBashMu.Unlock()
}

func (a *Agent) unregisterBashCmd(cmd *exec.Cmd) {
	a.activeBashMu.Lock()
	if a.activeBashCmd == cmd {
		a.activeBashCmd = nil
	}
	a.activeBashMu.Unlock()
}

func (a *Agent) killActiveBash() {
	a.activeBashMu.Lock()
	cmd := a.activeBashCmd
	a.activeBashMu.Unlock()
	if cmd != nil {
		_ = killProcessGroup(cmd)
	}
}

// bashStreamWriter accumulates command output up to a cap while streaming each
// appended chunk to onChunk. The streamed chunks concatenate exactly to the
// final String(), so the live view and the persisted result stay consistent.
type bashStreamWriter struct {
	mu        sync.Mutex
	buf       strings.Builder
	max       int
	total     int
	truncated bool
	onChunk   func(string)
}

const truncMarker = "\n... truncated ..."

func (w *bashStreamWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	w.total += len(p)
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

func (w *bashStreamWriter) Finalize() {
	w.mu.Lock()
	var emit string
	if w.total > w.max && !w.truncated {
		w.truncated = true
		w.buf.WriteString(truncMarker)
		emit = truncMarker
	}
	w.mu.Unlock()
	if emit != "" && w.onChunk != nil {
		w.onChunk(emit)
	}
}

func (w *bashStreamWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// bashDeltaEmitter throttles live bash output updates (Flame uses 100ms) so the
// TUI event channel is not flooded and copy goroutines cannot block on emit.
type bashDeltaEmitter struct {
	mu      sync.Mutex
	emit    func(string)
	pending strings.Builder
	lastAt  time.Time
	timer   *time.Timer
}

func newBashDeltaEmitter(emit func(string)) *bashDeltaEmitter {
	return &bashDeltaEmitter{emit: emit}
}

func (e *bashDeltaEmitter) add(chunk string) {
	if chunk == "" || e.emit == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pending.WriteString(chunk)
	delay := bashUpdateThrottle - time.Since(e.lastAt)
	if delay <= 0 {
		e.flushLocked()
		return
	}
	if e.timer == nil {
		e.timer = time.AfterFunc(delay, func() {
			e.mu.Lock()
			defer e.mu.Unlock()
			e.timer = nil
			e.flushLocked()
		})
	}
}

func (e *bashDeltaEmitter) flush() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.timer != nil {
		e.timer.Stop()
		e.timer = nil
	}
	e.flushLocked()
}

func (e *bashDeltaEmitter) flushLocked() {
	if e.pending.Len() == 0 {
		return
	}
	e.emit(e.pending.String())
	e.pending.Reset()
	e.lastAt = time.Now()
}
