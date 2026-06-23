// PORT: backend/workflow/spawn.go

import { type AgentOptions, type AgentResult, type AgentSnapshot } from "./types";
import { Runtime } from "./runtime";
import { parseAndValidateJSON } from "./schema";
import { roleTemplate } from "./roles";
import { RunWorkflowAgent } from "../agent/workflow_worker";
import {
  event_workflow_agent_start,
  event_workflow_agent_end,
  event_workflow_agent_delta,
  event_workflow_phase,
  event_tool_start,
  type event,
  type tool_call_event,
} from "../core/events";
import { isQuotaError } from "./pool";
import { Effect } from "effect";

export async function executeJob(
  this: Runtime,
  parent: AbortSignal,
  phase: string,
  key: string,
  opts: AgentOptions
): Promise<AgentResult> {
  if (!opts.role) {
    opts.role = phase;
  }
  if (!opts.systemPrompt) {
    opts.systemPrompt = roleTemplate(opts.role);
  }
  if (opts.responseSchema && Object.keys(opts.responseSchema).length > 0) {
    opts.prompt += "\n\nRespond with JSON only matching this schema:\n" + JSON.stringify(opts.responseSchema, null, "  ");
  }
  if (opts.role === "audit" || opts.role === "rule" || opts.role === "verify") {
    opts.readonly = true;
  }

  if (!this.reserveAgent()) {
    const result: AgentResult = {
      key,
      role: opts.role,
      ok: false,
      text: "",
      error: `workflow exceeded maxTotalAgents=${this.maxTotalAgents}`,
    };
    this.finishAgent(key, result);
    return result;
  }

  // Create child controller/signal to handle cancellation
  const controller = new AbortController();
  const onParentAbort = () => controller.abort();
  if (parent.aborted) {
    onParentAbort();
  } else {
    parent.addEventListener("abort", onParentAbort);
  }

  this.startAgent(key, phase, opts, () => controller.abort());

  let result = await this.runAgentAttempt(controller.signal, phase, key, opts);

  if (!result.error && opts.responseSchema && Object.keys(opts.responseSchema).length > 0) {
    try {
      const parsedVal = await Effect.runPromise(parseAndValidateJSON(result.text, opts.responseSchema));
      result.json = parsedVal;
    } catch (err: any) {
      if (!this.reserveAgent()) {
        result.error = err.message || String(err);
      } else {
        const retryOpts = { ...opts };
        retryOpts.prompt = opts.prompt + "\n\nYour previous response was invalid: " + (err.message || String(err)) + "\nRespond with JSON only matching the response schema.";
        result = await this.runAgentAttempt(controller.signal, phase, key, retryOpts);
        if (!result.error) {
          try {
            const parsedVal = await Effect.runPromise(parseAndValidateJSON(result.text, opts.responseSchema));
            result.json = parsedVal;
          } catch (retryErr: any) {
            result.error = retryErr.message || String(retryErr);
          }
        }
      }
    }
  }

  result.ok = !result.error;
  this.finishAgent(key, result);
  
  parent.removeEventListener("abort", onParentAbort);
  controller.abort();
  return result;
}

export async function runAgentAttempt(
  this: Runtime,
  ctx: AbortSignal,
  phase: string,
  key: string,
  opts: AgentOptions
): Promise<AgentResult> {
  return this.agentRunner(ctx, phase, key, opts, (ev: event) => {
    this.handleWorkerEvent(phase, key, opts.role ?? "", ev);
  });
}

export async function defaultAgentRunner(
  this: Runtime,
  ctx: AbortSignal,
  phase: string,
  key: string,
  opts: AgentOptions,
  emit: (event: event) => void
): Promise<AgentResult> {
  const result = await RunWorkflowAgent(
    ctx,
    this.cfg,
    this.workDir,
    {
      Prompt: opts.prompt,
      SystemPrompt: opts.systemPrompt ?? "",
      Tools: opts.tools ?? [],
      Model: opts.model ?? "",
      MaxTurns: opts.maxTurns ?? 0,
      Readonly: !!opts.readonly,
    },
    emit
  );
  return {
    key,
    role: opts.role,
    ok: !result.Error,
    text: result.Text,
    error: result.Error,
    tokensUsed: result.TokensUsed,
    turnCount: result.TurnCount,
  };
}

export function reserveAgent(this: Runtime): boolean {
  if (this.maxTotalAgents > 0 && this.totalAgents >= this.maxTotalAgents) {
    return false;
  }
  this.totalAgents++;
  return true;
}

export function startAgent(
  this: Runtime,
  key: string,
  phase: string,
  opts: AgentOptions,
  cancel: () => void
): void {
  const now = new Date();
  this.active[key] = { cancel };
  this.ensurePhaseLocked(phase);

  const s = this.snapshot.agents[key] || {
    key: "",
    phase: "",
    role: "",
    status: "",
    prompt: "",
  };
  s.key = key;
  s.phase = phase;
  s.role = defaultRole(opts.role ?? "");
  s.prompt = opts.prompt;
  s.status = "running";
  s.startedAt = now;
  this.snapshot.agents[key] = s;

  this.recountLocked();
  const emit = this.emit;
  const workflowID = this.snapshot.id;
  if (emit) {
    emit({
      kind: event_workflow_agent_start,
      data: {
        workflow_id: workflowID,
        phase,
        key,
        role: opts.role ?? "",
        status: "running",
        prompt: opts.prompt,
      },
    });
  }
  this.emitRun(event_workflow_phase, phase);
}

export function finishAgent(this: Runtime, key: string, result: AgentResult): void {
  const now = new Date();
  delete this.active[key];

  const s = this.snapshot.agents[key] || {
    key: "",
    phase: "",
    role: "",
    status: "",
    prompt: "",
  };
  s.status = "done";
  if (!result.ok) {
    s.status = "failed";
  }
  s.result = result.text;
  s.json = result.json;
  s.error = result.error ?? "";
  s.tokens = result.tokensUsed;
  s.turns = result.turnCount;
  s.endedAt = now;
  this.snapshot.agents[key] = s;

  const cancelledForPause = this.paused && (result.error ?? "").toLowerCase().includes("context canceled");
  if (!isQuotaError(result.error ?? "") && !this.restartRequested[key] && !cancelledForPause) {
    this.completed[key] = result;
  }

  this.recountLocked();
  const emit = this.emit;
  const workflowID = this.snapshot.id;
  this.persist();

  if (emit) {
    emit({
      kind: event_workflow_agent_end,
      data: {
        workflow_id: workflowID,
        phase: s.phase,
        key,
        role: result.role ?? "",
        status: s.status,
        result: result.text,
        json: result.json,
        error: result.error ?? "",
        tokens: result.tokensUsed ?? 0,
        turns: result.turnCount ?? 0,
      },
    });
  }
  this.emitRun(event_workflow_phase, s.phase);
}

export function handleWorkerEvent(
  this: Runtime,
  phase: string,
  key: string,
  role: string,
  event: event
): void {
  if (event.kind !== event_tool_start) {
    return;
  }
  const tool = event.data as tool_call_event;
  if (!tool) {
    return;
  }

  const s = this.snapshot.agents[key];
  if (!s) return;
  if (!s.lastTools) {
    s.lastTools = [];
  }
  s.lastTools.push(tool.name);
  if (s.lastTools.length > 12) {
    s.lastTools = s.lastTools.slice(s.lastTools.length - 12);
  }
  this.snapshot.agents[key] = s;

  const emit = this.emit;
  const workflowID = this.snapshot.id;
  if (emit) {
    emit({
      kind: event_workflow_agent_delta,
      data: {
        workflow_id: workflowID,
        phase,
        key,
        role,
        status: "running",
        tool,
      },
    });
  }
}

export function defaultRole(role: string): string {
  role = role.trim();
  if (role === "") {
    return "agent";
  }
  return role;
}

// Attach prototype methods
const proto = Runtime.prototype as any;
proto.executeJob = executeJob;
proto.runAgentAttempt = runAgentAttempt;
proto.defaultAgentRunner = defaultAgentRunner;
proto.reserveAgent = reserveAgent;
proto.startAgent = startAgent;
proto.finishAgent = finishAgent;
proto.handleWorkerEvent = handleWorkerEvent;

/*
PORT STATUS
source path: backend/workflow/spawn.go
source lines: 178
draft lines: 275
confidence: high
status: phase_b_compile
*/
