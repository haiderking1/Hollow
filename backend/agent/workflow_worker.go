package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/enough/enough/backend/config"
	"github.com/enough/enough/backend/core"
	"github.com/enough/enough/backend/opencode"
)

type WorkflowAgentOptions struct {
	Prompt       string
	SystemPrompt string
	Tools        []string
	Model        string
	MaxTurns     int
	Readonly     bool
}

type WorkflowAgentResult struct {
	Text       string
	Error      string
	TokensUsed int
	TurnCount  int
}

func RunWorkflowAgent(ctx context.Context, cfg config.Runtime, workDir string, opts WorkflowAgentOptions, emit func(core.Event)) WorkflowAgentResult {
	if strings.TrimSpace(opts.Model) != "" {
		cfg.Model = strings.TrimSpace(opts.Model)
	}
	allowed := workflowAllowedTools(opts.Tools, opts.Readonly)
	a := &Agent{
		cfg:          cfg,
		client:       opencode.NewClientForRuntime(cfg),
		workDir:      workDir,
		emit:         emit,
		swarmDepth:   1,
		allowedTools: allowed,
		readonlyRole: opts.Readonly,
	}
	system := strings.TrimSpace(opts.SystemPrompt)
	if system == "" {
		system = systemPrompt
	}
	messages := []opencode.Message{
		{Role: "system", Content: opencode.StringContent(system)},
		{Role: "user", Content: opencode.StringContent(opts.Prompt)},
	}
	tools := a.toolMenu()
	totalTokens := 0
	turns := 0

	for {
		if err := ctx.Err(); err != nil {
			return WorkflowAgentResult{Text: extractLastAssistantText(messages), Error: err.Error(), TokensUsed: totalTokens, TurnCount: turns}
		}
		if opts.MaxTurns > 0 && turns >= opts.MaxTurns {
			return WorkflowAgentResult{Text: extractLastAssistantText(messages), Error: "max turns exceeded", TokensUsed: totalTokens, TurnCount: turns}
		}
		req := opencode.ChatRequest{Model: cfg.Model, Messages: messages, Tools: tools}
		opencode.ApplyThinkingToRequest(&req, opencode.ParseThinkingLevel(cfg.ThinkingLevel), cfg.Model)
		msg, err := a.client.ChatStreamRetry(ctx, req, opencode.StreamCallbacks{})
		turns++
		if err != nil {
			return WorkflowAgentResult{Text: extractLastAssistantText(messages), Error: err.Error(), TokensUsed: totalTokens, TurnCount: turns}
		}
		if msg.Usage != nil {
			totalTokens += msg.Usage.TotalTokens
			if msg.Usage.TotalTokens == 0 {
				totalTokens += msg.Usage.Input + msg.Usage.Output
			}
		}
		messages = append(messages, msg)
		if len(msg.ToolCalls) == 0 {
			return WorkflowAgentResult{Text: opencode.ContentString(msg), TokensUsed: totalTokens, TurnCount: turns}
		}
		for idx, call := range msg.ToolCalls {
			if err := ctx.Err(); err != nil {
				return WorkflowAgentResult{Text: extractLastAssistantText(messages), Error: err.Error(), TokensUsed: totalTokens, TurnCount: turns}
			}
			id := call.ID
			if id == "" {
				id = fmt.Sprintf("workflow_call_%d", idx)
			}
			a.toolStart(id, call.Function.Name, call.Function.Arguments)
			result := a.executeTool(ctx, id, call.Function.Name, call.Function.Arguments)
			a.toolResult(id, result.output, result.isErr, result.details)
			messages = append(messages, opencode.Message{
				Role:       "tool",
				ToolCallID: id,
				Name:       call.Function.Name,
				Content:    opencode.StringContent(result.output),
			})
		}
	}
}

func workflowAllowedTools(requested []string, readonly bool) map[string]bool {
	allowed := map[string]bool{}
	if len(requested) == 0 {
		for _, name := range []string{"read_file", "list_dir", "glob", "grep", "bash", "web_search", "web_fetch", "browser"} {
			allowed[name] = true
		}
		if !readonly {
			allowed["write_file"] = true
			allowed["edit_file"] = true
		}
		return allowed
	}
	for _, name := range requested {
		name = strings.TrimSpace(name)
		if name == "" || name == "agent_swarm" {
			continue
		}
		if readonly && (name == "write_file" || name == "edit_file" || name == "skill_manage" || name == "memory") {
			continue
		}
		allowed[name] = true
	}
	return allowed
}
