package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/enough/enough/backend/opencode"
)

const (
	defaultSwarmConcurrency = 16
	maxSwarmWorkers         = 100
	maxSwarmDepth           = 3
	defaultSwarmRetries     = 3
	swarmAuxMaxIterations   = 12
)

type swarmTask struct {
	ID        string   `json:"id,omitempty"`
	Prompt    string   `json:"prompt"`
	DependsOn []string `json:"depends_on,omitempty"`
	upstream  string
}

type swarmWorkerResult struct {
	ID       string
	Prompt   string
	Status   string // ok, error, aborted
	Output   string
	Error    string
	Turns    int
	Attempts int
	Worktree string
	Branch   string
}

func agentSwarmTool() opencode.Tool {
	return opencode.Tool{
		Type: "function",
		Function: opencode.ToolFunction{
			Name: "agent_swarm",
			Description: fmt.Sprintf(
				`Run many sub-agents in parallel. Pass "tasks" (one self-contained prompt each) or a single "goal" to have a planner split it into parallel subtasks. Each agent gets a fresh, isolated context with the standard coding tools (read_file, grep, glob, list_dir, bash, write_file, edit_file, web_search, web_fetch) scoped to the current directory. For pipelines, give a task a depends_on=[ids]: it waits for those agents and receives their output in its prompt. Up to %d agents per call; max_concurrency (default %d) run at once. All agents run on your model. Agents run with no turn limit by default (like you). Do not set max_turns_per_agent unless the user explicitly requests a cap. Agents can nest agent_swarm up to %d nested swarm calls; from a top-level call this supports a four-worker chain (level1 -> level2 -> level3 -> level4). Keep tasks disjoint to avoid conflicting edits.`,
				maxSwarmWorkers, defaultSwarmConcurrency, maxSwarmDepth,
			),
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"goal": {
						"type": "string",
						"description": "High-level goal to auto-decompose into independent parallel subtasks via a planner agent. Provide this OR tasks; if both are given, tasks win."
					},
					"tasks": {
						"type": "array",
						"items": {
							"type": "object",
							"properties": {
								"id": {
									"type": "string",
									"description": "Optional short label for this agent's task. Defaults to agent-<n>."
								},
								"prompt": {
									"type": "string",
									"description": "The full, self-contained instruction for this agent. It runs in isolation with no memory of the other agents."
								},
								"depends_on": {
									"type": "array",
									"items": { "type": "string" },
									"description": "Ids of other tasks in THIS call that must finish before this agent starts. The dependent receives those agents' outputs in its prompt."
								}
							},
							"required": ["prompt"]
						},
						"description": "One entry per agent to spawn. Each agent runs in parallel in its own fresh context with the standard coding tools."
					},
					"shared_context": {
						"type": "string",
						"description": "Optional briefing prepended to every agent's prompt."
					},
					"max_concurrency": {
						"type": "number",
						"description": "Maximum number of agents running at the same time. Defaults to 16."
					},
					"retry": {
						"type": "number",
						"description": "How many times to retry an agent that errors or returns nothing. Default 1."
					},
					"max_turns_per_agent": {
						"type": "number",
						"description": "Do not use unless the user explicitly asks for a cap. Each agent runs to completion with no turn limit by default."
					},
					"isolate": {
						"type": "string",
						"enum": ["worktree"],
						"description": "Set to \"worktree\" to run each agent in its own git worktree/branch so parallel edits never collide. Dirty worktrees are left for review; clean ones are removed."
					}
				}
			}`),
		},
	}
}

func (a *Agent) toolAgentSwarm(ctx context.Context, callID, argsJSON string, depth int) toolResult {
	ctx, cancel := linkedSwarmContext(ctx)
	defer cancel()

	var params struct {
		Goal  string `json:"goal"`
		Tasks []struct {
			ID        string   `json:"id"`
			Prompt    string   `json:"prompt"`
			DependsOn []string `json:"depends_on"`
		} `json:"tasks"`
		SharedContext    string  `json:"shared_context"`
		MaxConcurrency   float64 `json:"max_concurrency"`
		Retry            float64 `json:"retry"`
		MaxTurnsPerAgent float64 `json:"max_turns_per_agent"`
		Isolate          string  `json:"isolate"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &params); err != nil {
		return toolResult{output: swarmArgsParseError(err), isErr: true}
	}

	tasks := parseSwarmTasks(params.Tasks)
	var plannerErr string
	if len(tasks) == 0 {
		goal := strings.TrimSpace(params.Goal)
		if goal == "" {
			return toolResult{output: "agent_swarm: provide either tasks or a goal.", isErr: true}
		}
		a.toolDelta(callID, "agent_swarm: planning subtasks…\n")
		planned, err := a.planSwarmTasks(ctx, goal)
		if err != "" {
			plannerErr = err
		}
		tasks = planned
	}
	if len(tasks) == 0 {
		msg := "agent_swarm: provide either tasks or a goal."
		if plannerErr != "" {
			msg = fmt.Sprintf("agent_swarm: planner produced no tasks (%s).", plannerErr)
		}
		return toolResult{output: msg, isErr: true}
	}
	if len(tasks) > maxSwarmWorkers {
		return toolResult{
			output: fmt.Sprintf("agent_swarm: %d tasks exceeds the limit of %d per call. Split into multiple calls.", len(tasks), maxSwarmWorkers),
			isErr:  true,
		}
	}
	if params.Isolate != "" && params.Isolate != "worktree" {
		return toolResult{output: fmt.Sprintf("agent_swarm: unsupported isolate mode %q", params.Isolate), isErr: true}
	}
	if params.Isolate != "worktree" {
		if conflict := detectSwarmPathConflicts(a, tasks); conflict != "" {
			return toolResult{output: conflict, isErr: true}
		}
	}

	concurrency := defaultSwarmConcurrency
	if params.MaxConcurrency > 0 {
		concurrency = int(params.MaxConcurrency)
	}
	if concurrency > len(tasks) {
		concurrency = len(tasks)
	}
	if concurrency < 1 {
		concurrency = 1
	}

	retries := defaultSwarmRetries
	if params.Retry >= 0 {
		retries = int(params.Retry)
	}

	maxTurns := 0
	if params.MaxTurnsPerAgent > 0 {
		maxTurns = int(params.MaxTurnsPerAgent)
	}

	sharedContext := strings.TrimSpace(params.SharedContext)
	repoRoot := ""
	if params.Isolate == "worktree" {
		repoRoot, _ = repoRootOf(a.workDir)
	}
	runID := fmt.Sprintf("%d", time.Now().UnixNano())
	// onEach is called from every worker goroutine in the pool, so the progress
	// counter and the progress emit must be serialized — concurrent appends to a
	// shared slice here corrupt memory and surface as nondeterministic hangs at
	// higher nesting depth (more workers => more racing callbacks).
	var progressMu sync.Mutex
	completed := 0
	onEach := func(swarmWorkerResult) {
		progressMu.Lock()
		completed++
		done := completed
		progressMu.Unlock()
		a.toolDelta(callID, fmt.Sprintf("agent_swarm: %d/%d agents finished\n", done, len(tasks)))
	}

	runOne := func(task swarmTask, index int) swarmWorkerResult {
		if params.Isolate == "worktree" {
			return a.runIsolatedSwarmWorker(ctx, task, index, depth, sharedContext, retries, maxTurns, repoRoot, runID)
		}
		return a.runSwarmWorker(ctx, task, index, depth, sharedContext, retries, maxTurns)
	}

	depIndices := resolveSwarmDependencies(tasks)
	var workers []swarmWorkerResult
	if hasSwarmDependencies(depIndices) {
		workers = runSwarmDAGPool(ctx, tasks, depIndices, concurrency, runOne, onEach)
	} else {
		workers = runSwarmWorkerPool(ctx, tasks, concurrency, runOne, onEach)
	}

	output := aggregateSwarmOutput(workers, concurrency, strings.TrimSpace(params.Goal))
	return toolResult{output: output}
}

func swarmArgsParseError(err error) string {
	return fmt.Sprintf(
		"agent_swarm: invalid JSON in tool arguments (%v). "+
			"Pass a single valid JSON object. Escape newlines inside strings as \\n — raw line breaks inside JSON strings are not allowed. "+
			"Example: {\"tasks\":[{\"id\":\"w1\",\"prompt\":\"short one-line instruction\"}]}",
		err,
	)
}

func parseSwarmTasks(raw []struct {
	ID        string   `json:"id"`
	Prompt    string   `json:"prompt"`
	DependsOn []string `json:"depends_on"`
}) []swarmTask {
	var tasks []swarmTask
	for _, t := range raw {
		prompt := strings.TrimSpace(t.Prompt)
		if prompt == "" {
			continue
		}
		tasks = append(tasks, swarmTask{
			ID:        strings.TrimSpace(t.ID),
			Prompt:    prompt,
			DependsOn: t.DependsOn,
		})
	}
	return tasks
}

var swarmPathCandidate = regexp.MustCompile("`([^`]+)`|(?:^|\\s)([A-Za-z0-9_./-]+\\.(?:go|md|txt|json|yaml|yml|toml|js|ts|tsx|jsx|css|html|sh|py|rs|java|c|cc|cpp|h|hpp|sql|xml|env)|[A-Za-z0-9_./-]+/[A-Za-z0-9_./-]+)(?:\\s|$)")

func detectSwarmPathConflicts(a *Agent, tasks []swarmTask) string {
	seen := map[string]int{}
	labels := map[string]string{}
	for i, task := range tasks {
		for _, p := range promptPathCandidates(task.Prompt) {
			resolved, err := a.resolvePath(p)
			if err != nil {
				continue
			}
			if prev, ok := seen[resolved]; ok && prev != i {
				return fmt.Sprintf("agent_swarm: tasks %s and %s both target path %s — split or use isolate=worktree",
					labels[resolved], swarmTaskID(task, i), filepath.ToSlash(p))
			}
			seen[resolved] = i
			labels[resolved] = swarmTaskID(task, i)
		}
	}
	return ""
}

func promptPathCandidates(prompt string) []string {
	var paths []string
	for _, m := range swarmPathCandidate.FindAllStringSubmatch(prompt, -1) {
		candidate := strings.TrimSpace(firstNonEmpty(m[1], m[2]))
		candidate = strings.Trim(candidate, `"'.,:;()[]{}<>`)
		if candidate == "" || strings.HasPrefix(candidate, "http://") || strings.HasPrefix(candidate, "https://") {
			continue
		}
		paths = append(paths, candidate)
	}
	return paths
}

func swarmTaskID(task swarmTask, index int) string {
	if id := strings.TrimSpace(task.ID); id != "" {
		return id
	}
	return fmt.Sprintf("agent-%d", index+1)
}

func (a *Agent) runSwarmWorker(ctx context.Context, task swarmTask, index, depth int, sharedContext string, retries, maxTurns int) swarmWorkerResult {
	return a.runSwarmWorkerInDir(ctx, task, index, depth, sharedContext, retries, maxTurns, a.workDir)
}

func (a *Agent) runSwarmWorkerInDir(ctx context.Context, task swarmTask, index, depth int, sharedContext string, retries, maxTurns int, workDir string) swarmWorkerResult {
	id := swarmTaskID(task, index)
	attempt := 0
	var lastError string

	worker := &Agent{
		cfg:        a.cfg,
		client:     a.client,
		workDir:    workDir,
		emit:       nil, // worker tools stay off the parent transcript
		swarmDepth: depth,
	}

	for {
		if userAbortFired(ctx) {
			return swarmWorkerResult{ID: id, Prompt: task.Prompt, Status: "aborted", Turns: 0, Attempts: attempt + 1}
		}

		prompt := buildSwarmWorkerPrompt(task, sharedContext, attempt, lastError)
		workerCtx, workerCancel := linkedSwarmContext(ctx)
		output, turns, status, errMsg := worker.runWorkerLoop(workerCtx, prompt, maxTurns)
		workerCancel()

		result := swarmWorkerResult{
			ID:       id,
			Prompt:   task.Prompt,
			Status:   status,
			Output:   output,
			Error:    errMsg,
			Turns:    turns,
			Attempts: attempt + 1,
		}

		emptyOK := status == "ok" && strings.TrimSpace(output) == ""
		emptyAbort := status == "aborted" && strings.TrimSpace(output) == ""
		if (status == "error" || emptyOK || (emptyAbort && !userAbortFired(ctx))) && attempt < retries {
			if emptyOK {
				lastError = "returned no output"
			} else if emptyAbort {
				lastError = "aborted with no output"
			} else {
				lastError = errMsg
			}
			attempt++
			continue
		}
		return result
	}
}

func (a *Agent) runIsolatedSwarmWorker(ctx context.Context, task swarmTask, index, depth int, sharedContext string, retries, maxTurns int, repoRoot, runID string) swarmWorkerResult {
	if repoRoot == "" {
		return a.runSwarmWorker(ctx, task, index, depth, sharedContext, retries, maxTurns)
	}

	id := swarmTaskID(task, index)
	safe := safeSwarmID(id)
	branch := fmt.Sprintf("swarm/%s/%s", runID, safe)
	base := filepath.Join(os.TempDir(), "enough-swarm-"+runID)
	dir := filepath.Join(base, safe)
	if err := os.MkdirAll(base, 0o755); err != nil {
		fallback := a.runSwarmWorker(ctx, task, index, depth, sharedContext, retries, maxTurns)
		fallback.Error = firstNonEmpty(fallback.Error, "worktree setup failed: "+err.Error())
		return fallback
	}
	if err := git(repoRoot, "worktree", "add", "-b", branch, dir, "HEAD"); err != nil {
		fallback := a.runSwarmWorker(ctx, task, index, depth, sharedContext, retries, maxTurns)
		fallback.Error = firstNonEmpty(fallback.Error, "worktree setup failed: "+err.Error())
		return fallback
	}

	result := a.runSwarmWorkerInDir(ctx, task, index, maxSwarmDepth, sharedContext, retries, maxTurns, dir)
	kept := true
	if status, err := gitOutput(dir, "status", "--porcelain"); err == nil {
		kept = strings.TrimSpace(status) != ""
		if !kept {
			_ = git(repoRoot, "worktree", "remove", "--force", dir)
			_ = git(repoRoot, "branch", "-D", branch)
		}
	}
	if kept {
		result.Worktree = dir
		result.Branch = branch
	}
	return result
}

func safeSwarmID(id string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9._-]+`)
	safe := strings.Trim(re.ReplaceAllString(id, "-"), "-")
	if safe == "" {
		return "agent"
	}
	return safe
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func git(cwd string, args ...string) error {
	_, err := gitOutput(cwd, args...)
	return err
}

func gitOutput(cwd string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = cwd
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func repoRootOf(workDir string) (string, error) {
	return gitOutput(workDir, "rev-parse", "--show-toplevel")
}

func buildSwarmWorkerPrompt(task swarmTask, sharedContext string, attempt int, lastError string) string {
	var parts []string
	if attempt > 0 {
		msg := "Your previous attempt did not succeed"
		if lastError != "" {
			msg += " (" + lastError + ")"
		}
		msg += ". Please try again and complete the task."
		parts = append(parts, msg)
	}
	if sharedContext != "" {
		parts = append(parts, sharedContext)
	}
	if task.upstream != "" {
		parts = append(parts, task.upstream)
	}
	parts = append(parts, task.Prompt)
	return strings.Join(parts, "\n\n---\n\n")
}

func (a *Agent) runWorkerLoop(ctx context.Context, prompt string, maxTurns int) (output string, turns int, status string, errMsg string) {
	tools := workerTools(a.swarmDepth)
	messages := []opencode.Message{
		{Role: "system", Content: opencode.StringContent(systemPrompt)},
		{Role: "user", Content: opencode.StringContent(prompt)},
	}

	// lastSwarmOutput holds the aggregated output of a nested agent_swarm run in
	// the most recent tool turn. If the model then finishes with empty text we
	// fall back to it so deep results still propagate up instead of vanishing.
	var lastSwarmOutput string

	for {
		if userAbortFired(ctx) {
			return extractLastAssistantText(messages), turns, "aborted", ""
		}
		if maxTurns > 0 && turns >= maxTurns {
			return extractLastAssistantText(messages), turns, "error", "max turns exceeded"
		}

		req := opencode.ChatRequest{
			Model:    a.cfg.Model,
			Messages: messages,
			Tools:    tools,
		}
		opencode.ApplyThinkingToRequest(&req, opencode.ParseThinkingLevel(a.cfg.ThinkingLevel), a.cfg.Model)

		streamCtx, streamCancel := linkedSwarmContext(ctx)
		msg, err := a.client.ChatStreamRetry(streamCtx, req, opencode.StreamCallbacks{})
		streamCancel()
		turns++
		if err != nil {
			if userAbortFired(ctx) {
				return extractLastAssistantText(messages), turns, "aborted", ""
			}
			return extractLastAssistantText(messages), turns, "error", err.Error()
		}

		messages = append(messages, msg)
		if len(msg.ToolCalls) == 0 {
			text := resolveWorkerOutput(opencode.ContentString(msg), lastSwarmOutput)
			return text, turns, "ok", ""
		}

		// New tool turn: only a nested swarm in THIS turn should back-stop an
		// empty final answer, so clear any output carried from earlier turns.
		lastSwarmOutput = ""
		onlySwarm := len(msg.ToolCalls) == 1 && msg.ToolCalls[0].Function.Name == "agent_swarm"
		var swarmResult toolResult
		for idx, call := range msg.ToolCalls {
			// Don't bail before a nested swarm — it runs on its own linked context
			// and may still succeed even if a parent stream ctx flickered.
			if call.Function.Name != "agent_swarm" && userAbortFired(ctx) {
				return extractLastAssistantText(messages), turns, "aborted", ""
			}
			id := call.ID
			if id == "" {
				id = fmt.Sprintf("worker_call_%d", idx)
			}
			a.toolStart(id, call.Function.Name, call.Function.Arguments)
			result := a.executeSwarmTool(ctx, id, call.Function.Name, call.Function.Arguments)
			a.toolResult(id, result.output, result.isErr, result.details)

			if call.Function.Name == "agent_swarm" {
				lastSwarmOutput = result.output
				if onlySwarm {
					swarmResult = result
				}
			}

			var toolMsg opencode.Message
			if len(result.content) > 0 {
				toolMsg = opencode.Message{
					Role:       "tool",
					ToolCallID: id,
					Name:       call.Function.Name,
					Content:    opencode.ToolContentFromAgent(result.content),
				}
			} else {
				toolMsg = opencode.Message{
					Role:       "tool",
					ToolCallID: id,
					Name:       call.Function.Name,
					Content:    opencode.StringContent(result.output),
				}
			}
			messages = append(messages, toolMsg)
		}

		// Pure delegation turn: the worker only nested a swarm. Don't round-trip
		// through the model again for a single child — real models often return
		// empty or a useless one-liner instead of echoing the child's payload,
		// which breaks deep nesting. Multi-child swarms must go through the
		// final model turn so the parent can combine/summarize all children.
		if onlySwarm && swarmWorkerSectionCount(swarmResult.output) <= 1 {
			if output := resolveSwarmReturnOutput(swarmResult.output); output != "" {
				if !swarmResult.isErr {
					return output, turns, "ok", ""
				}
				if payload := extractSwarmPayload(swarmResult.output); payload != "" {
					return payload, turns, "ok", ""
				}
			}
			if swarmResult.isErr {
				return "", turns, "error", strings.TrimSpace(swarmResult.output)
			}
		}
		if lastSwarmOutput != "" && swarmWorkerSectionCount(lastSwarmOutput) <= 1 {
			output := resolveSwarmReturnOutput(lastSwarmOutput)
			return output, turns, "ok", ""
		}
	}
}

// linkedSwarmContext returns a context for a swarm run that still respects an
// explicit user abort on parent, but won't inherit stray cancellation from
// ancestor contexts that can fire mid-nest and produce "aborted (1 turn)" flakes.
func linkedSwarmContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx := context.WithoutCancel(parent)
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		select {
		case <-userAbortDone(parent):
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

// resolveWorkerOutput decides what a swarm worker hands back to its parent. The
// model's own final text wins, but when it ends with empty text right after a
// nested agent_swarm, fall back to that swarm's aggregated output so deeply
// nested results survive the trip back up instead of being dropped.
func resolveWorkerOutput(finalText, lastSwarmOutput string) string {
	if trimmed := strings.TrimSpace(finalText); trimmed != "" && !isSwarmStubText(trimmed) {
		return trimmed
	}
	return resolveSwarmReturnOutput(lastSwarmOutput)
}

func isSwarmStubText(s string) bool {
	trimmed := strings.TrimSpace(strings.ToLower(s))
	trimmed = strings.Trim(trimmed, " \t\r\n.!?")
	trimmed = strings.Join(strings.Fields(trimmed), " ")
	switch trimmed {
	case "", "ok", "okay", "done", "all done", "complete", "completed", "task complete", "task completed", "finished":
		return true
	default:
		return false
	}
}

var swarmSectionHeader = regexp.MustCompile(`(?m)^##\s+(.+?)\s+\[(ok|error|aborted)\]\s*(?:\([^)]+\))?.*$`)

func resolveSwarmReturnOutput(output string) string {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return ""
	}
	if swarmWorkerSectionCount(output) == 1 {
		if payload := extractSwarmPayload(output); payload != "" {
			return payload
		}
	}
	return trimmed
}

func swarmWorkerSectionCount(output string) int {
	return len(swarmSectionHeader.FindAllStringIndex(output, -1))
}

func extractSwarmPayload(output string) string {
	if !swarmSectionHeader.MatchString(output) {
		return ""
	}
	payload, _ := extractSwarmPayloadAtDepth(output, 0)
	return payload
}

func extractSwarmPayloadAtDepth(output string, depth int) (string, int) {
	matches := swarmSectionHeader.FindAllStringSubmatchIndex(output, -1)
	bestPayload := ""
	bestDepth := -1
	if len(matches) == 0 {
		clean := cleanSwarmBody(output)
		if clean == "" {
			return "", -1
		}
		return clean, depth
	}
	for i, m := range matches {
		status := output[m[4]:m[5]]
		bodyStart := m[1]
		bodyEnd := len(output)
		if i+1 < len(matches) {
			bodyEnd = matches[i+1][0]
		}
		body := output[bodyStart:bodyEnd]
		child, childDepth := extractSwarmPayloadAtDepth(body, depth+1)
		if child != "" && childDepth > bestDepth {
			bestPayload = child
			bestDepth = childDepth
		}
		if status != "ok" {
			continue
		}
		clean := cleanSwarmBody(body)
		if clean != "" && depth >= bestDepth {
			bestPayload = clean
			bestDepth = depth
		}
	}
	return bestPayload, bestDepth
}

func cleanSwarmBody(body string) string {
	var lines []string
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			lines = append(lines, line)
			continue
		}
		if trimmed == "(no output)" || strings.HasPrefix(trimmed, "Ran ") || strings.HasPrefix(trimmed, "Goal: ") {
			continue
		}
		if strings.HasPrefix(trimmed, "agent_swarm: ") && strings.Contains(trimmed, " agents finished") {
			continue
		}
		if strings.HasPrefix(trimmed, "## ") {
			continue
		}
		lines = append(lines, line)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func extractLastAssistantText(messages []opencode.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" {
			text := strings.TrimSpace(opencode.ContentString(messages[i]))
			if text != "" {
				return text
			}
		}
	}
	return ""
}

func (a *Agent) planSwarmTasks(ctx context.Context, goal string) ([]swarmTask, string) {
	planner := &Agent{
		cfg:        a.cfg,
		client:     a.client,
		workDir:    a.workDir,
		swarmDepth: maxSwarmDepth, // planner cannot nest swarms
	}
	prompt := strings.Join([]string{
		"You are a planner for a parallel agent swarm.",
		"Goal: " + goal,
		"Break this into subtasks. Prefer INDEPENDENT subtasks that can run in parallel (split by file, module, or area).",
		"Assign at most one writer to any file in this swarm call. Split by module/path; never ask parallel workers to edit the same path.",
		"When one subtask genuinely needs another's result, express that with depends_on instead of forcing it into one task.",
		"Use your read-only tools to inspect the repo as needed.",
		`Reply with ONLY a JSON array. Each element: {"id": "short-label", "prompt": "complete self-contained instruction", "depends_on": ["other-id", ...]}.`,
		"Omit depends_on (or use []) for subtasks that can start immediately. Do not create cycles.",
		"Keep it to a sensible number of tasks (usually 2-12).",
	}, "\n")

	output, _, status, errMsg := planner.runPlannerLoop(ctx, prompt)
	if status != "ok" {
		if errMsg != "" {
			return nil, errMsg
		}
		return nil, "planner failed"
	}
	tasks := parsePlannedSwarmTasks(output)
	if len(tasks) == 0 {
		return nil, "planner did not return any usable tasks"
	}
	return tasks, ""
}

func (a *Agent) runPlannerLoop(ctx context.Context, prompt string) (output string, turns int, status string, errMsg string) {
	tools := plannerTools()
	messages := []opencode.Message{
		{Role: "system", Content: opencode.StringContent(systemPrompt)},
		{Role: "user", Content: opencode.StringContent(prompt)},
	}

	for {
		if userAbortFired(ctx) {
			return extractLastAssistantText(messages), turns, "aborted", ""
		}
		if turns >= swarmAuxMaxIterations {
			return extractLastAssistantText(messages), turns, "error", "planner max iterations exceeded"
		}

		req := opencode.ChatRequest{
			Model:    a.cfg.Model,
			Messages: messages,
			Tools:    tools,
		}
		opencode.ApplyThinkingToRequest(&req, opencode.ParseThinkingLevel(a.cfg.ThinkingLevel), a.cfg.Model)

		streamCtx, streamCancel := linkedSwarmContext(ctx)
		msg, err := a.client.ChatStreamRetry(streamCtx, req, opencode.StreamCallbacks{})
		streamCancel()
		turns++
		if err != nil {
			if userAbortFired(ctx) {
				return extractLastAssistantText(messages), turns, "aborted", ""
			}
			return extractLastAssistantText(messages), turns, "error", err.Error()
		}

		messages = append(messages, msg)
		if len(msg.ToolCalls) == 0 {
			return strings.TrimSpace(opencode.ContentString(msg)), turns, "ok", ""
		}

		for idx, call := range msg.ToolCalls {
			id := call.ID
			if id == "" {
				id = fmt.Sprintf("planner_call_%d", idx)
			}
			result := a.executePlannerTool(ctx, call.Function.Name, call.Function.Arguments)
			var toolMsg opencode.Message
			if len(result.content) > 0 {
				toolMsg = opencode.Message{
					Role:       "tool",
					ToolCallID: id,
					Name:       call.Function.Name,
					Content:    opencode.ToolContentFromAgent(result.content),
				}
			} else {
				toolMsg = opencode.Message{
					Role:       "tool",
					ToolCallID: id,
					Name:       call.Function.Name,
					Content:    opencode.StringContent(result.output),
				}
			}
			messages = append(messages, toolMsg)
		}
	}
}

func (a *Agent) executePlannerTool(ctx context.Context, name, argsJSON string) toolResult {
	switch name {
	case "read_file":
		return a.toolReadFile(argsJSON)
	case "list_dir":
		return a.toolListDir(argsJSON)
	case "glob":
		return a.toolGlob(argsJSON)
	case "grep":
		return a.toolGrep(ctx, argsJSON)
	default:
		return toolResult{output: fmt.Sprintf("tool %q is not available to the planner (read-only: read_file, list_dir, glob, grep)", name), isErr: true}
	}
}

var jsonArrayFence = regexp.MustCompile("(?is)```(?:json)?\\s*([\\s\\S]*?)```")

func parsePlannedSwarmTasks(text string) []swarmTask {
	body := text
	if m := jsonArrayFence.FindStringSubmatch(text); len(m) == 2 {
		body = m[1]
	}
	start := strings.Index(body, "[")
	end := strings.LastIndex(body, "]")
	if start == -1 || end == -1 || end < start {
		return nil
	}
	var raw []json.RawMessage
	if err := json.Unmarshal([]byte(body[start:end+1]), &raw); err != nil {
		return nil
	}
	var tasks []swarmTask
	for _, entry := range raw {
		var s string
		if err := json.Unmarshal(entry, &s); err == nil && strings.TrimSpace(s) != "" {
			tasks = append(tasks, swarmTask{Prompt: strings.TrimSpace(s)})
			continue
		}
		var obj struct {
			ID        string   `json:"id"`
			Prompt    string   `json:"prompt"`
			DependsOn []string `json:"depends_on"`
		}
		if err := json.Unmarshal(entry, &obj); err != nil || strings.TrimSpace(obj.Prompt) == "" {
			continue
		}
		tasks = append(tasks, swarmTask{
			ID:        strings.TrimSpace(obj.ID),
			Prompt:    strings.TrimSpace(obj.Prompt),
			DependsOn: obj.DependsOn,
		})
	}
	return tasks
}

func runSwarmWorkerPool(
	ctx context.Context,
	tasks []swarmTask,
	concurrency int,
	runOne func(swarmTask, int) swarmWorkerResult,
	onEach func(swarmWorkerResult),
) []swarmWorkerResult {
	results := make([]swarmWorkerResult, len(tasks))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i, task := range tasks {
		i, task := i, task
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			var r swarmWorkerResult
			r = runOne(task, i)
			results[i] = r
			onEach(r)
		}()
	}
	wg.Wait()
	return results
}

func swarmAbortedStub(task swarmTask, index int) swarmWorkerResult {
	return swarmWorkerResult{
		ID:     swarmTaskID(task, index),
		Prompt: task.Prompt,
		Status: "aborted",
	}
}

func resolveSwarmDependencies(tasks []swarmTask) [][]int {
	idToIndex := make(map[string]int, len(tasks))
	for i := range tasks {
		idToIndex[swarmTaskID(tasks[i], i)] = i
	}
	depIndices := make([][]int, len(tasks))
	for i, task := range tasks {
		for _, depID := range task.DependsOn {
			if j, ok := idToIndex[strings.TrimSpace(depID)]; ok && j != i {
				depIndices[i] = append(depIndices[i], j)
			}
		}
	}
	return depIndices
}

func hasSwarmDependencies(depIndices [][]int) bool {
	for _, deps := range depIndices {
		if len(deps) > 0 {
			return true
		}
	}
	return false
}

func buildSwarmUpstreamContext(deps []int, results []swarmWorkerResult) string {
	if len(deps) == 0 {
		return ""
	}
	var blocks []string
	for _, d := range deps {
		r := results[d]
		body := r.Output
		if body == "" {
			body = "(no output)"
		}
		blocks = append(blocks, fmt.Sprintf("### Output from %q\n%s", r.ID, body))
	}
	return "Results from the agent(s) this task depends on. Build on these directly — do not re-derive or guess what they produced:\n\n" + strings.Join(blocks, "\n\n")
}

func runSwarmDAGPool(
	ctx context.Context,
	tasks []swarmTask,
	depIndices [][]int,
	concurrency int,
	runOne func(swarmTask, int) swarmWorkerResult,
	onEach func(swarmWorkerResult),
) []swarmWorkerResult {
	results := make([]swarmWorkerResult, len(tasks))
	done := make([]bool, len(tasks))
	doneCount := 0
	inflight := make(map[int]chan struct{})
	var mu sync.Mutex

	finish := func(index int, result swarmWorkerResult) {
		results[index] = result
		done[index] = true
		doneCount++
		onEach(result)
	}

	for doneCount < len(tasks) {
		mu.Lock()
		scheduled := 0
		for i := 0; i < len(tasks) && len(inflight) < concurrency; i++ {
			if done[i] || inflight[i] != nil {
				continue
			}
			deps := depIndices[i]
			ready := true
			for _, d := range deps {
				if !done[d] {
					ready = false
					break
				}
			}
			if !ready {
				continue
			}
			if userAbortFired(ctx) {
				continue
			}
			var failedDep *int
			for _, d := range deps {
				if results[d].Status != "ok" {
					failedDep = &d
					break
				}
			}
			if failedDep != nil {
				finish(i, swarmWorkerResult{
					ID:     swarmTaskID(tasks[i], i),
					Prompt: tasks[i].Prompt,
					Status: "aborted",
					Error:  fmt.Sprintf("skipped: dependency %q did not succeed", results[*failedDep].ID),
				})
				scheduled++
				continue
			}

			task := tasks[i]
			if upstream := buildSwarmUpstreamContext(deps, results); upstream != "" {
				task.upstream = upstream
			}
			index := i
			ch := make(chan struct{})
			inflight[index] = ch
			go func(task swarmTask, index int) {
				var r swarmWorkerResult
				r = runOne(task, index)
				mu.Lock()
				finish(index, r)
				delete(inflight, index)
				close(ch)
				mu.Unlock()
			}(task, index)
			scheduled++
		}
		inflightCount := len(inflight)
		allDone := doneCount >= len(tasks)
		mu.Unlock()

		if allDone {
			break
		}
		if scheduled == 0 && inflightCount == 0 {
			mu.Lock()
			for i := 0; i < len(tasks); i++ {
				if !done[i] {
					reason := "skipped: unresolved dependency cycle"
					if userAbortFired(ctx) {
						reason = ""
					}
					r := swarmAbortedStub(tasks[i], i)
					if reason != "" {
						r.Error = reason
					}
					finish(i, r)
				}
			}
			mu.Unlock()
			break
		}
		if inflightCount > 0 {
			mu.Lock()
			var waitCh chan struct{}
			for _, ch := range inflight {
				waitCh = ch
				break
			}
			mu.Unlock()
			if waitCh != nil {
				select {
				case <-waitCh:
				case <-userAbortDone(ctx):
				}
			}
		}
	}
	return results
}

func aggregateSwarmOutput(workers []swarmWorkerResult, concurrency int, goal string) string {
	ok, failed, aborted := 0, 0, 0
	for _, w := range workers {
		switch w.Status {
		case "ok":
			ok++
		case "error":
			failed++
		case "aborted":
			aborted++
		}
	}
	header := fmt.Sprintf("Ran %d agent(s) at concurrency %d — %d ok", len(workers), concurrency, ok)
	if failed > 0 {
		header += fmt.Sprintf(", %d error", failed)
	}
	if aborted > 0 {
		header += fmt.Sprintf(", %d aborted", aborted)
	}
	header += "."

	var parts []string
	if goal != "" {
		parts = append(parts, "Goal: "+goal)
	}
	parts = append(parts, header, "")

	for _, w := range workers {
		parts = append(parts, swarmWorkerSection(w))
	}
	return strings.Join(parts, "\n")
}

func swarmWorkerSection(w swarmWorkerResult) string {
	turns := fmt.Sprintf("%d turn", w.Turns)
	if w.Turns != 1 {
		turns += "s"
	}
	retry := ""
	if w.Attempts > 1 {
		retry = fmt.Sprintf(" ×%d", w.Attempts)
	}
	header := fmt.Sprintf("## %s [%s] (%s%s)", w.ID, w.Status, turns, retry)
	if w.Worktree != "" && w.Branch != "" {
		header += fmt.Sprintf(" (worktree: %s · branch: %s)", w.Worktree, w.Branch)
	}
	if w.Status != "ok" && w.Error != "" {
		return header + "\nError: " + w.Error
	}
	body := w.Output
	if body == "" {
		body = "(no output)"
	}
	return header + "\n" + body
}
