// PORT: backend/agent/tools.go

import { Effect } from "effect";
import { type tool } from "../opencode/types";
import { type tool_content_block } from "../opencode/content";
import { Agent, type toolResult, WriteOriginBackgroundReview } from "./agent";
import { ExecuteSkillsList } from "../skills/tool_list";
import { ExecuteSkillView } from "../skills/tool_view";
import { ExecuteSkillManage } from "../skills/tool_manage";

export function executeTool(
  this: Agent,
  ctx: AbortSignal,
  id: string,
  name: string,
  argsJSON: string
): Effect.Effect<toolResult, Error> {
  return Effect.gen(this, function* () {
    const rejected = this.guardTool(name, argsJSON);
    if (rejected !== null) {
      return rejected;
    }

    let beforeHash = "";
    if (name === "write_file" || name === "edit_file") {
      beforeHash = this.fileHashIfExists(argsJSON);
    }

    const result = yield* this.dispatchTool(ctx, id, name, argsJSON);

    const scored = this.scoreToolStep(name, argsJSON, result);
    if (scored !== null) {
      return scored;
    }

    if (!result.isErr) {
      this.recordEvidence(name, argsJSON, beforeHash);
      if (name === "agent_swarm") {
        this.noteMutation();
      }
    }

    return result;
  }).pipe(
    Effect.catchAll((err: any) =>
      Effect.succeed({ output: err?.message || String(err), isErr: true })
    )
  );
}

Agent.prototype.executeTool = executeTool;

export function dispatchTool(
  this: Agent,
  ctx: AbortSignal,
  id: string,
  name: string,
  argsJSON: string
): Effect.Effect<toolResult, Error> {
  if (name.startsWith("mcp_")) {
    if (this.mcpManager === null) {
      return Effect.succeed({ output: "MCP manager not initialized", isErr: true });
    }
    return Effect.tryPromise({
      try: () => Effect.runPromise(this.mcpManager!.call_tool(ctx, name, argsJSON)),
      catch: (cause) => (cause instanceof Error ? cause : new Error(String(cause))),
    }).pipe(
      Effect.map(([outputBlock, contentBlocks, isErr]: any) => ({
        output: outputBlock.text,
        content: contentBlocks,
        isErr: isErr,
      })),
      Effect.catchAll((err: any) =>
        Effect.succeed({ output: err?.message || String(err), isErr: true })
      )
    );
  }

  switch (name) {
    case "read_file":
      return this.toolReadFile(argsJSON);
    case "write_file":
      return this.toolWriteFile(argsJSON);
    case "edit_file":
      return this.toolEditFile(argsJSON);
    case "list_dir":
      return this.toolListDir(argsJSON);
    case "glob":
      return this.toolGlob(argsJSON);
    case "grep":
      return this.toolGrep(ctx, argsJSON);
    case "bash":
      return this.toolBash(ctx, id, argsJSON);
    case "web_search":
      return this.toolWebSearch(ctx, argsJSON);
    case "web_fetch":
      return this.toolWebFetch(ctx, argsJSON);
    case "browser":
      return this.toolBrowser(ctx, argsJSON);
    case "agent_swarm":
      return this.toolAgentSwarm(ctx, id, argsJSON, 0);
    case "skills_list":
      return this.toolSkillsList(argsJSON);
    case "skill_view":
      return this.toolSkillView(argsJSON);
    case "skill_manage":
      return this.toolSkillManage(argsJSON);
    case "memory":
      return this.toolMemory(argsJSON);
    default:
      return Effect.succeed({ output: `unknown tool: ${name}`, isErr: true });
  }
}

Agent.prototype.dispatchTool = dispatchTool;

Agent.prototype.executeSwarmTool = function (
  this: Agent,
  ctx: AbortSignal,
  id: string,
  name: string,
  argsJSON: string
): Effect.Effect<toolResult, Error> {
  if (name === "agent_swarm") {
    return this.toolAgentSwarm(ctx, id, argsJSON, this.swarmDepth + 1);
  }
  return this.executeTool(ctx, id, name, argsJSON);
};

Agent.prototype.toolSkillsList = function (
  this: Agent,
  argsJSON: string
): Effect.Effect<toolResult, Error> {
  return ExecuteSkillsList(argsJSON, this.workDir, this.cfg).pipe(
    Effect.map(([output, isErr]) => ({ output, isErr })),
    Effect.catchAll((err: any) =>
      Effect.succeed({ output: err?.message || String(err), isErr: true })
    )
  );
};

Agent.prototype.toolSkillView = function (
  this: Agent,
  argsJSON: string
): Effect.Effect<toolResult, Error> {
  try {
    const sessionID = this.session ? this.session.session_id() : "";
    const [output, isErr] = ExecuteSkillView(argsJSON, this.workDir, this.cfg, sessionID);
    return Effect.succeed({ output, isErr });
  } catch (err: any) {
    return Effect.succeed({ output: err?.message || String(err), isErr: true });
  }
};

Agent.prototype.toolSkillManage = function (
  this: Agent,
  argsJSON: string
): Effect.Effect<toolResult, Error> {
  const opts = {
    GuardEnabled: this.cfg.skills.guard_agent_created,
    MarkCreatedAsAgent: this.writeOrigin === WriteOriginBackgroundReview,
    ArchiveOnDelete: this.writeOrigin === WriteOriginBackgroundReview,
    WriteApproval: this.cfg.skills.write_approval,
    BypassGate: false,
    Origin: this.writeOrigin,
  };
  return ExecuteSkillManage(argsJSON, opts).pipe(
    Effect.map(([output, isErr]) => ({ output, isErr })),
    Effect.catchAll((err: any) =>
      Effect.succeed({ output: err?.message || String(err), isErr: true })
    )
  );
};

/*
PORT STATUS
source path: backend/agent/tools.go
source lines: 139
draft lines: 153
confidence: high
status: phase_b_compile
*/
