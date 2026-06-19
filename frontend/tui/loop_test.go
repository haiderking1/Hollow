package tui

import (
	"testing"

	"github.com/enough/enough/backend/core"
)

func TestParseLoopCommand(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		task    string
		max     int
		promise string
	}{
		{name: "trailing max", input: "fix auth --max 10", task: "fix auth", max: 10, promise: "DONE"},
		{name: "unlimited", input: "fix auth", task: "fix auth", max: 0, promise: "DONE"},
		{name: "leading max stays task text", input: "--max 5 add tests", task: "--max 5 add tests", max: 0, promise: "DONE"},
		{name: "zero is unlimited", input: "ship it --max 0", task: "ship it", max: 0, promise: "DONE"},
		{name: "nonnumeric max stays task text", input: "document --max flag behavior", task: "document --max flag behavior", max: 0, promise: "DONE"},
		{name: "embedded numeric max stays task text", input: "document --max 5 behavior", task: "document --max 5 behavior", max: 0, promise: "DONE"},
		{name: "custom promise", input: "finish and output <promise>COMPLETE</promise>", task: "finish and output <promise>COMPLETE</promise>", max: 0, promise: "COMPLETE"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task, max, promise, err := parseLoopCommand(tt.input)
			if err != nil {
				t.Fatalf("parseLoopCommand: %v", err)
			}
			if task != tt.task || max != tt.max || promise != tt.promise {
				t.Fatalf("got task=%q max=%d promise=%q", task, max, promise)
			}
		})
	}
}

func TestParseLoopCommandErrors(t *testing.T) {
	for _, input := range []string{"", "--max 1"} {
		t.Run(input, func(t *testing.T) {
			if _, _, _, err := parseLoopCommand(input); err == nil {
				t.Fatalf("expected error for %q", input)
			}
		})
	}
}

func TestTryContinueLoopStopsAfterAgentError(t *testing.T) {
	app := &App{
		loop:     loopState{active: true, prompt: "keep going", completionPromise: "DONE"},
		renderCh: make(chan struct{}, 1),
	}
	defer app.stopRenderTimer()

	app.handleAgentEvent(core.Event{Kind: core.EventError, Data: "API unavailable"})
	if app.tryContinueLoop() {
		t.Fatal("errored loop must not continue")
	}
	if app.loop.active {
		t.Fatal("loop should be cleared after an agent error")
	}
	last := app.messages[len(app.messages)-1]
	if last.role != "system" || last.text != "loop stopped: agent error" {
		t.Fatalf("unexpected error-stop message: %+v", last)
	}
}

func TestTryContinueLoopStopsAfterAbort(t *testing.T) {
	app := &App{
		loop:     loopState{active: true, prompt: "keep going", completionPromise: "DONE", aborted: true},
		renderCh: make(chan struct{}, 1),
	}
	defer app.stopRenderTimer()

	if app.tryContinueLoop() {
		t.Fatal("aborted loop must not continue")
	}
	if app.loop.active {
		t.Fatal("loop should be cleared after abort")
	}
	last := app.messages[len(app.messages)-1]
	if last.role != "system" || last.text != "loop cancelled" {
		t.Fatalf("unexpected abort message: %+v", last)
	}
}

func TestHandleSubmitBlocksMessagesDuringActiveLoop(t *testing.T) {
	app := &App{
		loop:     loopState{active: true},
		editor:   NewTaskEditor(),
		renderCh: make(chan struct{}, 1),
	}
	defer app.stopRenderTimer()
	app.editor.SetValue("another task")

	app.handleSubmit()

	if app.running {
		t.Fatal("normal submit must not start an agent while a loop is active")
	}
	last := app.messages[len(app.messages)-1]
	if last.role != "error" || last.text != "loop active — use /loop-cancel" {
		t.Fatalf("unexpected submit-block message: %+v", last)
	}
}

func TestLoopCompleteUsesLastAssistantMessage(t *testing.T) {
	app := &App{
		loop: loopState{active: true, completionPromise: "DONE"},
		messages: []chatMsg{
			{role: "assistant", text: "<promise>DONE</promise>"},
			{role: "user", text: "later"},
			{role: "assistant", text: "still working"},
		},
	}
	if app.loopComplete() {
		t.Fatal("an older assistant promise must not complete the loop")
	}
	app.messages = append(app.messages, chatMsg{role: "system", text: "status"})
	app.messages = append(app.messages, chatMsg{role: "assistant", text: "finished <promise>DONE</promise>"})
	if !app.loopComplete() {
		t.Fatal("expected the last assistant promise to complete the loop")
	}
}

func TestTryContinueLoopStopsAtMax(t *testing.T) {
	app := &App{
		loop:     loopState{active: true, prompt: "keep going", maxIterations: 1, completionPromise: "DONE"},
		messages: []chatMsg{{role: "assistant", text: "not done"}},
		renderCh: make(chan struct{}, 1),
	}
	defer app.stopRenderTimer()

	if app.tryContinueLoop() {
		t.Fatal("max=1 must stop after the first completed iteration")
	}
	if app.loop.active {
		t.Fatal("loop should be cleared after reaching max")
	}
	last := app.messages[len(app.messages)-1]
	if last.role != "system" || last.text != "loop stopped: max iterations (1) reached" {
		t.Fatalf("unexpected max message: %+v", last)
	}
}

func TestTryContinueLoopReportsCompletionCount(t *testing.T) {
	app := &App{
		loop:     loopState{active: true, prompt: "finish", completionPromise: "DONE"},
		messages: []chatMsg{{role: "assistant", text: "complete <promise>DONE</promise>"}},
		renderCh: make(chan struct{}, 1),
	}
	defer app.stopRenderTimer()

	if app.tryContinueLoop() {
		t.Fatal("completed loop must not continue")
	}
	last := app.messages[len(app.messages)-1]
	if last.role != "system" || last.text != "loop finished (1 iterations)" {
		t.Fatalf("unexpected completion message: %+v", last)
	}
}

func TestIsLoopCancelCommand(t *testing.T) {
	if !isLoopCancelCommand(" /LOOP-CANCEL ") || !isLoopCancelCommand("/cancel-loop") {
		t.Fatal("expected loop cancel aliases")
	}
	if isLoopCancelCommand("/loop-cancel now") {
		t.Fatal("arguments must not match the cancel command")
	}
}
