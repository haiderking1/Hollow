package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/enough/enough/backend/agent"
	"github.com/enough/enough/backend/core"
)

func (r *Runtime) executeJob(parent context.Context, phase, key string, opts AgentOptions) AgentResult {
	if opts.Role == "" {
		opts.Role = phase
	}
	if opts.SystemPrompt == "" {
		opts.SystemPrompt = roleTemplate(opts.Role)
	}
	if len(opts.ResponseSchema) > 0 {
		if schema, err := json.MarshalIndent(opts.ResponseSchema, "", "  "); err == nil {
			opts.Prompt += "\n\nRespond with JSON only matching this schema:\n" + string(schema)
		}
	}
	if opts.Role == "audit" || opts.Role == "rule" || opts.Role == "verify" {
		opts.Readonly = true
	}
	if !r.reserveAgent() {
		result := AgentResult{Key: key, Role: opts.Role, OK: false, Error: fmt.Sprintf("workflow exceeded maxTotalAgents=%d", r.maxTotalAgents)}
		r.finishAgent(key, result)
		return result
	}

	ctx, cancel := context.WithCancel(parent)
	r.startAgent(key, phase, opts, cancel)
	result := r.runAgentAttempt(ctx, phase, key, opts)
	if result.Error == "" && len(opts.ResponseSchema) > 0 {
		value, err := parseAndValidateJSON(result.Text, opts.ResponseSchema)
		if err != nil {
			if !r.reserveAgent() {
				result.Error = err.Error()
			} else {
				retry := opts
				retry.Prompt = opts.Prompt + "\n\nYour previous response was invalid: " + err.Error() + "\nRespond with JSON only matching the response schema."
				result = r.runAgentAttempt(ctx, phase, key, retry)
				if result.Error == "" {
					value, err = parseAndValidateJSON(result.Text, opts.ResponseSchema)
				}
				if err != nil {
					result.Error = err.Error()
				} else {
					result.JSON = value
				}
			}
		} else {
			result.JSON = value
		}
	}
	result.OK = result.Error == ""
	r.finishAgent(key, result)
	cancel()
	return result
}

func (r *Runtime) runAgentAttempt(ctx context.Context, phase, key string, opts AgentOptions) AgentResult {
	return r.agentRunner(ctx, phase, key, opts, func(event core.Event) {
		r.handleWorkerEvent(phase, key, opts.Role, event)
	})
}

func (r *Runtime) defaultAgentRunner(ctx context.Context, phase, key string, opts AgentOptions, emit func(core.Event)) AgentResult {
	worker := agent.RunWorkflowAgent(ctx, r.cfg, r.workDir, agent.WorkflowAgentOptions{
		Prompt: opts.Prompt, SystemPrompt: opts.SystemPrompt, Tools: opts.Tools,
		Model: opts.Model, MaxTurns: opts.MaxTurns, Readonly: opts.Readonly,
	}, emit)
	return AgentResult{
		Key: key, Role: opts.Role, Text: worker.Text, Error: worker.Error,
		TokensUsed: worker.TokensUsed, TurnCount: worker.TurnCount,
	}
}

func (r *Runtime) reserveAgent() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.maxTotalAgents > 0 && r.totalAgents >= r.maxTotalAgents {
		return false
	}
	r.totalAgents++
	return true
}

func (r *Runtime) startAgent(key, phase string, opts AgentOptions, cancel context.CancelFunc) {
	now := time.Now()
	r.mu.Lock()
	r.active[key] = &jobControl{cancel: cancel}
	r.ensurePhaseLocked(phase)
	s := r.snapshot.Agents[key]
	s.Key, s.Phase, s.Role, s.Prompt = key, phase, defaultRole(opts.Role), opts.Prompt
	s.Status, s.StartedAt = "running", now
	r.snapshot.Agents[key] = s
	r.recountLocked()
	emit := r.emit
	workflowID := r.snapshot.ID
	r.mu.Unlock()
	if emit != nil {
		emit(core.Event{Kind: core.EventWorkflowAgentStart, Data: core.WorkflowAgentEvent{
			WorkflowID: workflowID, Phase: phase, Key: key, Role: opts.Role,
			Status: "running", Prompt: opts.Prompt,
		}})
	}
	r.emitRun(core.EventWorkflowPhase, phase)
}

func (r *Runtime) finishAgent(key string, result AgentResult) {
	now := time.Now()
	r.mu.Lock()
	delete(r.active, key)
	s := r.snapshot.Agents[key]
	s.Status = "done"
	if !result.OK {
		s.Status = "failed"
	}
	s.Result, s.JSON, s.Error = result.Text, result.JSON, result.Error
	s.Tokens, s.Turns, s.EndedAt = result.TokensUsed, result.TurnCount, now
	r.snapshot.Agents[key] = s
	cancelledForPause := r.paused && strings.Contains(strings.ToLower(result.Error), "context canceled")
	if !isQuotaError(result.Error) && !r.restartRequested[key] && !cancelledForPause {
		r.completed[key] = result
	}
	r.recountLocked()
	emit := r.emit
	workflowID := r.snapshot.ID
	r.mu.Unlock()
	r.persist()
	if emit != nil {
		emit(core.Event{Kind: core.EventWorkflowAgentEnd, Data: core.WorkflowAgentEvent{
			WorkflowID: workflowID, Phase: s.Phase, Key: key, Role: result.Role,
			Status: s.Status, Result: result.Text, JSON: result.JSON, Error: result.Error,
			Tokens: result.TokensUsed, Turns: result.TurnCount,
		}})
	}
	r.emitRun(core.EventWorkflowPhase, s.Phase)
}

func (r *Runtime) handleWorkerEvent(phase, key, role string, event core.Event) {
	tool, ok := event.Data.(core.ToolCallEvent)
	if !ok {
		return
	}
	r.mu.Lock()
	s := r.snapshot.Agents[key]
	if event.Kind == core.EventToolStart {
		s.LastTools = append(s.LastTools, tool.Name)
		if len(s.LastTools) > 12 {
			s.LastTools = s.LastTools[len(s.LastTools)-12:]
		}
	}
	r.snapshot.Agents[key] = s
	emit := r.emit
	workflowID := r.snapshot.ID
	r.mu.Unlock()
	if emit != nil {
		emit(core.Event{Kind: core.EventWorkflowAgentDelta, Data: core.WorkflowAgentEvent{
			WorkflowID: workflowID, Phase: phase, Key: key, Role: role,
			Status: "running", Tool: tool,
		}})
	}
}

func defaultRole(role string) string {
	role = strings.TrimSpace(role)
	if role == "" {
		return "agent"
	}
	return role
}
