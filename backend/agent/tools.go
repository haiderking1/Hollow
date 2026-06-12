package agent

import (
	"context"
	"fmt"

	"github.com/enough/enough/backend/skills"
)

type toolResult struct {
	output string
	isErr  bool
}

func (a *Agent) executeTool(ctx context.Context, id, name, argsJSON string) toolResult {
	// Hard guard: rejected calls never reach the tool. Evidence is recorded
	// by the runtime on success only — the model cannot write ledger rows.
	if rejected := a.guardTool(name, argsJSON); rejected != nil {
		return *rejected
	}

	var beforeHash string
	if name == "write_file" || name == "edit_file" {
		beforeHash = a.fileHashIfExists(argsJSON)
	}

	result := a.dispatchTool(ctx, id, name, argsJSON)

	if !result.isErr {
		a.recordEvidence(name, argsJSON, beforeHash)
		// Swarm workers edit in worktrees that merge back; treat a completed
		// swarm as a workspace mutation so verification is obligated.
		if name == "agent_swarm" {
			a.noteMutation()
		}
	}
	return result
}

func (a *Agent) dispatchTool(ctx context.Context, id, name, argsJSON string) toolResult {
	switch name {
	case "read_file":
		return a.toolReadFile(argsJSON)
	case "write_file":
		return a.toolWriteFile(argsJSON)
	case "edit_file":
		return a.toolEditFile(argsJSON)
	case "list_dir":
		return a.toolListDir(argsJSON)
	case "glob":
		return a.toolGlob(argsJSON)
	case "grep":
		return a.toolGrep(argsJSON)
	case "bash":
		return a.toolBash(ctx, id, argsJSON)
	case "web_search":
		return a.toolWebSearch(argsJSON)
	case "agent_swarm":
		return a.toolAgentSwarm(ctx, id, argsJSON, 0)
	case "skills_list":
		return a.toolSkillsList(argsJSON)
	case "skill_view":
		return a.toolSkillView(argsJSON)
	case "skill_manage":
		return a.toolSkillManage(argsJSON)
	case "memory":
		return a.toolMemory(argsJSON)
	default:
		return toolResult{output: fmt.Sprintf("unknown tool: %s", name), isErr: true}
	}
}

func (a *Agent) executeSwarmTool(ctx context.Context, id, name, argsJSON string) toolResult {
	if name == "agent_swarm" {
		return a.toolAgentSwarm(ctx, id, argsJSON, a.swarmDepth+1)
	}
	return a.executeTool(ctx, id, name, argsJSON)
}

func (a *Agent) toolSkillsList(argsJSON string) toolResult {
	output, isErr := skills.ExecuteSkillsList(argsJSON, a.workDir, a.cfg)
	return toolResult{output: output, isErr: isErr}
}

func (a *Agent) toolSkillView(argsJSON string) toolResult {
	sessionID := ""
	if a.session != nil {
		sessionID = a.session.SessionID()
	}
	output, isErr := skills.ExecuteSkillView(argsJSON, a.workDir, a.cfg, sessionID)
	return toolResult{output: output, isErr: isErr}
}

func (a *Agent) toolSkillManage(argsJSON string) toolResult {
	// Skill provenance: only the background-review fork marks creations as
	// agent-created (curator-eligible). Foreground user-directed creations
	// belong to the user and are never auto-curated.
	output, isErr := skills.ExecuteSkillManage(argsJSON, skills.SkillManageOptions{
		GuardEnabled:       a.cfg.Skills.GuardAgentCreated,
		MarkCreatedAsAgent: a.writeOrigin == WriteOriginBackgroundReview,
		// Autonomous passes never destroy data: delete becomes archive.
		ArchiveOnDelete: a.writeOrigin == WriteOriginBackgroundReview,
		WriteApproval:      a.cfg.Skills.WriteApproval,
		BypassGate:         false,
		Origin:             a.writeOrigin,
	})
	return toolResult{output: output, isErr: isErr}
}
