package tui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/enough/enough/backend/auth"
)

type loopState struct {
	active            bool
	prompt            string
	iteration         int
	maxIterations     int
	completionPromise string
	aborted           bool
	lastRunErrored    bool
}

var loopPromisePattern = regexp.MustCompile(`<promise>([^<]+)</promise>`)
var trailingLoopMaxPattern = regexp.MustCompile(`(?:^|\s)--max\s+(\d+)\s*$`)

// parseLoopCommand parses /loop arguments. A zero max means unlimited.
func parseLoopCommand(arg string) (task string, maxIter int, promise string, err error) {
	task = strings.TrimSpace(arg)
	if match := trailingLoopMaxPattern.FindStringSubmatchIndex(task); match != nil {
		n, parseErr := strconv.Atoi(task[match[2]:match[3]])
		if parseErr != nil {
			return "", 0, "", fmt.Errorf("--max requires a non-negative integer")
		}
		maxIter = n
		task = strings.TrimSpace(task[:match[0]])
	}

	if task == "" {
		return "", 0, "", fmt.Errorf("Usage: /loop <task> [--max N]")
	}

	promise = "DONE"
	if match := loopPromisePattern.FindStringSubmatch(task); len(match) == 2 {
		if custom := strings.TrimSpace(match[1]); custom != "" {
			promise = custom
		}
	}
	return task, maxIter, promise, nil
}

func isLoopCancelCommand(input string) bool {
	switch strings.ToLower(strings.TrimSpace(input)) {
	case "/loop-cancel", "/cancel-loop":
		return true
	default:
		return false
	}
}

func (a *App) startLoop(arg string) {
	if a.running {
		a.appendMessage("error", "wait for the agent to finish")
		return
	}
	if a.loop.active {
		a.appendMessage("error", "a loop is already active")
		return
	}
	if !auth.Connected() {
		a.appendMessage("error", "not connected — type / and pick connect")
		return
	}

	task, maxIter, promise, err := parseLoopCommand(arg)
	if err != nil {
		a.appendMessage("error", err.Error())
		return
	}

	a.loop = loopState{
		active:            true,
		prompt:            task,
		maxIterations:     maxIter,
		completionPromise: promise,
	}
	a.appendUserMessage(task, nil)
	a.bumpChat()
	a.startLoopAgent(task)
	a.requestRender()
}

func (a *App) cancelLoop() {
	if !a.loop.active {
		a.appendMessage("system", "no active loop")
		return
	}
	if a.running {
		a.loop.aborted = true
		a.bumpChat()
		a.abortAgent()
		return
	}
	a.clearLoop()
	a.appendMessage("system", "loop cancelled")
}

func (a *App) tryContinueLoop() bool {
	if !a.loop.active {
		return false
	}
	if a.loop.aborted {
		a.clearLoop()
		a.appendMessage("system", "loop cancelled")
		return false
	}
	if a.loop.lastRunErrored {
		a.clearLoop()
		a.appendMessage("system", "loop stopped: agent error")
		return false
	}

	a.loop.iteration++
	if a.loopComplete() {
		iterations := a.loop.iteration
		a.clearLoop()
		a.appendMessage("system", fmt.Sprintf("loop finished (%d iterations)", iterations))
		return false
	}
	if a.loop.maxIterations > 0 && a.loop.iteration >= a.loop.maxIterations {
		iterations := a.loop.iteration
		a.clearLoop()
		a.appendMessage("system", fmt.Sprintf("loop stopped: max iterations (%d) reached", iterations))
		return false
	}

	nextIteration := a.loop.iteration + 1
	task := a.loop.prompt
	a.bumpChat()
	a.startAgentLoopContinuation(task, nextIteration)
	return true
}

func (a *App) loopComplete() bool {
	if !a.loop.active || a.loop.completionPromise == "" {
		return false
	}
	for i := len(a.messages) - 1; i >= 0; i-- {
		if a.messages[i].role != "assistant" {
			continue
		}
		want := "<promise>" + a.loop.completionPromise + "</promise>"
		return strings.Contains(a.messages[i].text, want)
	}
	return false
}

func (a *App) clearLoop() {
	a.loop = loopState{}
	a.forceAssistantBubble = false
	a.bumpChat()
}
