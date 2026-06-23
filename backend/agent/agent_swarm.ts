// PORT: backend/agent/agent_swarm.go

import { Effect } from "effect";
import { type tool, type message, type chat_request, string_content, content_string } from "../opencode/types";
import { Agent, type toolResult, type swarmTask, type swarmWorkerResult, maxSwarmDepth } from "./agent";
import { workerTools, plannerTools, agentSwarmTool } from "./tools_registry";
import { systemPrompt } from "./prompt";
import { apply_thinking_to_request, parse_thinking_level } from "../opencode/thinking";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import fs from "node:fs";
import path from "node:path";
import os from "node:os";

const execFileAsync = promisify(execFile);

const defaultSwarmConcurrency = 16;
const maxSwarmWorkers = 100;
const defaultSwarmRetries = 3;
const swarmAuxMaxIterations = 12;

async function gitOutput(cwd: string, ...args: string[]): Promise<string> {
  try {
    const { stdout, stderr } = await execFileAsync("git", args, { cwd });
    return (stdout + stderr).trim();
  } catch (err: any) {
    const out = err.stdout || "";
    const serr = err.stderr || "";
    throw new Error(`git ${args.join(" ")}: ${(out + serr).trim() || err.message}`);
  }
}

async function git(cwd: string, ...args: string[]): Promise<void> {
  await gitOutput(cwd, ...args);
}

async function repoRootOf(workDir: string): Promise<string> {
  return await gitOutput(workDir, "rev-parse", "--show-toplevel");
}

export function toolAgentSwarm(
  this: Agent,
  ctx: AbortSignal,
  callID: string,
  argsJSON: string,
  depth: number
): Effect.Effect<toolResult, Error> {
  const self = this;
  return Effect.tryPromise({
    try: async () => {
      const [workerCtx, workerCancel] = linkedSwarmContext(ctx);
      try {
        let params: {
          goal?: string;
          tasks?: Array<{
            id?: string;
            prompt: string;
            depends_on?: string[];
          }>;
          shared_context?: string;
          max_concurrency?: number;
          retry?: number;
          max_turns_per_agent?: number;
          isolate?: string;
        } = {};
        try {
          params = JSON.parse(argsJSON);
        } catch (err: any) {
          return { output: swarmArgsParseError(err), isErr: true };
        }

        const tasks = parseSwarmTasks(params.tasks || []);
        let plannerErr = "";
        let finalTasks = [...tasks];
        if (finalTasks.length === 0) {
          const goal = (params.goal || "").trim();
          if (goal === "") {
            return { output: "agent_swarm: provide either tasks or a goal.", isErr: true };
          }
          self.emitToolDelta(callID, "agent_swarm: planning subtasks…\n");
          const [planned, pErr] = await self.planSwarmTasks(workerCtx, goal);
          if (pErr !== "") {
            plannerErr = pErr;
          }
          finalTasks = planned || [];
        }

        if (finalTasks.length === 0) {
          let msg = "agent_swarm: provide either tasks or a goal.";
          if (plannerErr !== "") {
            msg = `agent_swarm: planner produced no tasks (${plannerErr}).`;
          }
          return { output: msg, isErr: true };
        }

        if (finalTasks.length > maxSwarmWorkers) {
          return {
            output: `agent_swarm: ${finalTasks.length} tasks exceeds the limit of ${maxSwarmWorkers} per call. Split into multiple calls.`,
            isErr: true,
          };
        }

        if (params.isolate && params.isolate !== "worktree") {
          return { output: `agent_swarm: unsupported isolate mode "${params.isolate}"`, isErr: true };
        }

        if (params.isolate !== "worktree") {
          const conflict = detectSwarmPathConflicts(self, finalTasks);
          if (conflict !== "") {
            return { output: conflict, isErr: true };
          }
        }

        let concurrency = defaultSwarmConcurrency;
        if (params.max_concurrency && params.max_concurrency > 0) {
          concurrency = Math.floor(params.max_concurrency);
        }
        if (concurrency > finalTasks.length) {
          concurrency = finalTasks.length;
        }
        if (concurrency < 1) {
          concurrency = 1;
        }

        let retries = defaultSwarmRetries;
        if (params.retry !== undefined && params.retry >= 0) {
          retries = Math.floor(params.retry);
        }

        let maxTurns = 0;
        if (params.max_turns_per_agent && params.max_turns_per_agent > 0) {
          maxTurns = Math.floor(params.max_turns_per_agent);
        }

        const sharedContext = (params.shared_context || "").trim();
        let repoRoot = "";
        if (params.isolate === "worktree") {
          try {
            repoRoot = await repoRootOf(self.workDir);
          } catch {}
        }
        const runID = `${Date.now()}_${Math.floor(Math.random() * 1000000)}`;

        let completed = 0;
        const onEach = (r: swarmWorkerResult) => {
          completed++;
          self.emitToolDelta(callID, `agent_swarm: ${completed}/${finalTasks.length} agents finished\n`);
        };

        const runOne = async (task: swarmTask, index: number): Promise<swarmWorkerResult> => {
          if (params.isolate === "worktree") {
            return self.runIsolatedSwarmWorker(workerCtx, task, index, depth, sharedContext, retries, maxTurns, repoRoot, runID);
          }
          return self.runSwarmWorker(workerCtx, task, index, depth, sharedContext, retries, maxTurns);
        };

        const depIndices = resolveSwarmDependencies(finalTasks);
        let workers: swarmWorkerResult[] = [];
        if (hasSwarmDependencies(depIndices)) {
          workers = await runSwarmDAGPool(workerCtx, finalTasks, depIndices, concurrency, runOne, onEach);
        } else {
          workers = await runSwarmWorkerPool(workerCtx, finalTasks, concurrency, runOne, onEach);
        }

        const output = aggregateSwarmOutput(workers, concurrency, (params.goal || "").trim());
        return { output };
      } finally {
        workerCancel();
      }
    },
    catch: (err) => err instanceof Error ? err : new Error(String(err))
  });
}

Agent.prototype.toolAgentSwarm = toolAgentSwarm;

function swarmArgsParseError(err: Error): string {
  return `agent_swarm: invalid JSON in tool arguments (${err.message}). ` +
    `Pass a single valid JSON object. Escape newlines inside strings as \\n — raw line breaks inside JSON strings are not allowed. ` +
    `Example: {"tasks":[{"id":"w1","prompt":"short one-line instruction"}]}`;
}

function parseSwarmTasks(raw: any[]): swarmTask[] {
  const tasks: swarmTask[] = [];
  for (const t of raw) {
    const prompt = (t.prompt || "").trim();
    if (prompt === "") {
      continue;
    }
    tasks.push({
      ID: (t.id || "").trim(),
      Prompt: prompt,
      DependsOn: t.depends_on,
    });
  }
  return tasks;
}

const swarmPathCandidate = /`([^`]+)`|(?:^|\s)([A-Za-z0-9_./-]+\.(?:go|md|txt|json|yaml|yml|toml|js|ts|tsx|jsx|css|html|sh|py|rs|java|c|cc|cpp|h|hpp|sql|xml|env)|[A-Za-z0-9_./-]+\/[A-Za-z0-9_./-]+)(?:\s|$)/g;

function detectSwarmPathConflicts(a: Agent, tasks: swarmTask[]): string {
  const seen: Record<string, number> = {};
  const labels: Record<string, string> = {};
  for (let i = 0; i < tasks.length; i++) {
    const task = tasks[i];
    for (const p of promptPathCandidates(task.Prompt)) {
      let resolved = "";
      try {
        resolved = Effect.runSync(a.resolvePath(p));
      } catch {
        continue;
      }
      if (seen[resolved] !== undefined && seen[resolved] !== i) {
        return `agent_swarm: tasks ${labels[resolved]} and ${swarmTaskID(task, i)} both target path ${path.posix.normalize(p)} — split or use isolate=worktree`;
      }
      seen[resolved] = i;
      labels[resolved] = swarmTaskID(task, i);
    }
  }
  return "";
}

function promptPathCandidates(prompt: string): string[] {
  const paths: string[] = [];
  let match;
  swarmPathCandidate.lastIndex = 0;
  while ((match = swarmPathCandidate.exec(prompt)) !== null) {
    let candidate = (match[1] || match[2] || "").trim();
    candidate = candidate.replace(/^["'.,:;()\[\]{}<>]+|["'.,:;()\[\]{}<>]+$/g, "");
    if (candidate === "" || candidate.startsWith("http://") || candidate.startsWith("https://")) {
      continue;
    }
    paths.push(candidate);
  }
  return paths;
}

function swarmTaskID(task: swarmTask, index: number): string {
  const id = (task.ID || "").trim();
  if (id !== "") {
    return id;
  }
  return `agent-${index + 1}`;
}

Agent.prototype.runSwarmWorker = async function (
  this: Agent,
  ctx: AbortSignal,
  task: swarmTask,
  index: number,
  depth: number,
  sharedContext: string,
  retries: number,
  maxTurns: number
): Promise<swarmWorkerResult> {
  return this.runSwarmWorkerInDir(ctx, task, index, depth, sharedContext, retries, maxTurns, this.workDir);
};

Agent.prototype.runSwarmWorkerInDir = async function (
  this: Agent,
  ctx: AbortSignal,
  task: swarmTask,
  index: number,
  depth: number,
  sharedContext: string,
  retries: number,
  maxTurns: number,
  workDir: string
): Promise<swarmWorkerResult> {
  const id = swarmTaskID(task, index);
  let attempt = 0;
  let lastError = "";

  const worker = new Agent();
  worker.cfg = this.cfg;
  worker.client = this.client;
  worker.workDir = workDir;
  worker.swarmDepth = depth;
  worker.emit = null; // worker tools stay off the parent transcript

  while (true) {
    if (this.userAbortFired()) {
      return { ID: id, Prompt: task.Prompt, Status: "aborted", Output: "", Error: "", Turns: 0, Attempts: attempt + 1, Worktree: "", Branch: "" };
    }

    const prompt = buildSwarmWorkerPrompt(task, sharedContext, attempt, lastError);
    const [workerCtx, workerCancel] = linkedSwarmContext(ctx);
    let output = "";
    let turns = 0;
    let status = "";
    let errMsg = "";

    try {
      const res = await worker.runWorkerLoop(workerCtx, prompt, maxTurns);
      output = res[0];
      turns = res[1];
      status = res[2];
      errMsg = res[3];
    } catch (err: any) {
      status = "error";
      errMsg = err.message || String(err);
    } finally {
      workerCancel();
    }

    const result: swarmWorkerResult = {
      ID: id,
      Prompt: task.Prompt,
      Status: status,
      Output: output,
      Error: errMsg,
      Turns: turns,
      Attempts: attempt + 1,
      Worktree: "",
      Branch: "",
    };

    const emptyOK = status === "ok" && output.trim() === "";
    const emptyAbort = status === "aborted" && output.trim() === "";
    if ((status === "error" || emptyOK || (emptyAbort && !this.userAbortFired())) && attempt < retries) {
      if (emptyOK) {
        lastError = "returned no output";
      } else if (emptyAbort) {
        lastError = "aborted with no output";
      } else {
        lastError = errMsg;
      }
      attempt++;
      continue;
    }
    return result;
  }
};

Agent.prototype.runIsolatedSwarmWorker = async function (
  this: Agent,
  ctx: AbortSignal,
  task: swarmTask,
  index: number,
  depth: number,
  sharedContext: string,
  retries: number,
  maxTurns: number,
  repoRoot: string,
  runID: string
): Promise<swarmWorkerResult> {
  if (repoRoot === "") {
    return this.runSwarmWorker(ctx, task, index, depth, sharedContext, retries, maxTurns);
  }

  const id = swarmTaskID(task, index);
  const safe = safeSwarmID(id);
  const branch = `swarm/${runID}/${safe}`;
  const base = path.join(os.tmpdir(), `hollow-swarm-${runID}`);
  const dir = path.join(base, safe);

  try {
    await fs.promises.mkdir(base, { recursive: true, mode: 0o755 });
  } catch (err: any) {
    const fallback = await this.runSwarmWorker(ctx, task, index, depth, sharedContext, retries, maxTurns);
    fallback.Error = fallback.Error || `worktree setup failed: ${err.message}`;
    return fallback;
  }

  try {
    await git(repoRoot, "worktree", "add", "-b", branch, dir, "HEAD");
  } catch (err: any) {
    const fallback = await this.runSwarmWorker(ctx, task, index, depth, sharedContext, retries, maxTurns);
    fallback.Error = fallback.Error || `worktree setup failed: ${err.message}`;
    return fallback;
  }

  const result = await this.runSwarmWorkerInDir(ctx, task, index, maxSwarmDepth, sharedContext, retries, maxTurns, dir);
  let kept = true;
  try {
    const statusStr = await gitOutput(dir, "status", "--porcelain");
    kept = statusStr.trim() !== "";
    if (!kept) {
      await git(repoRoot, "worktree", "remove", "--force", dir);
      await git(repoRoot, "branch", "-D", branch);
    }
  } catch {
    // ignore git check failures
  }

  if (kept) {
    result.Worktree = dir;
    result.Branch = branch;
  }
  return result;
};

function safeSwarmID(id: string): string {
  const safe = id.replace(/[^a-zA-Z0-9._-]+/g, "-").replace(/^-+|-+$/g, "");
  if (safe === "") {
    return "agent";
  }
  return safe;
}

function buildSwarmWorkerPrompt(task: swarmTask, sharedContext: string, attempt: number, lastError: string): string {
  const parts: string[] = [];
  if (attempt > 0) {
    let msg = "Your previous attempt did not succeed";
    if (lastError !== "") {
      msg += ` (${lastError})`;
    }
    msg += ". Please try again and complete the task.";
    parts.push(msg);
  }
  if (sharedContext !== "") {
    parts.push(sharedContext);
  }
  if (task.upstream && task.upstream !== "") {
    parts.push(task.upstream);
  }
  parts.push(task.Prompt);
  return parts.join("\n\n---\n\n");
}

Agent.prototype.runWorkerLoop = async function (
  this: Agent,
  ctx: AbortSignal,
  prompt: string,
  maxTurns: number
): Promise<[string, number, string, string]> {
  const tools = workerTools(this.swarmDepth);
  const messages: message[] = [
    { role: "system", content: string_content(systemPrompt) },
    { role: "user", content: string_content(prompt) },
  ];
  let turns = 0;
  let lastSwarmOutput = "";

  while (true) {
    if (this.userAbortFired()) {
      return [extractLastAssistantText(messages), turns, "aborted", ""];
    }
    if (maxTurns > 0 && turns >= maxTurns) {
      return [extractLastAssistantText(messages), turns, "error", "max turns exceeded"];
    }

    const req: chat_request = {
      model: this.cfg.model,
      messages: messages,
      tools: tools,
    };
    apply_thinking_to_request(req, parse_thinking_level(this.cfg.thinking_level || ""), this.cfg.model);

    const [streamCtx, streamCancel] = linkedSwarmContext(ctx);
    let msg: message;
    try {
      msg = (await Effect.runPromise(
        this.client.chat_stream_retry(streamCtx, req, {})
      )) as message;
    } catch (err: any) {
      if (this.userAbortFired()) {
        return [extractLastAssistantText(messages), turns, "aborted", ""];
      }
      return [extractLastAssistantText(messages), turns, "error", err.message || String(err)];
    } finally {
      streamCancel();
    }

    turns++;
    messages.push(msg);

    if (!msg.tool_calls || msg.tool_calls.length === 0) {
      const text = resolveWorkerOutput(content_string(msg), lastSwarmOutput);
      return [text, turns, "ok", ""];
    }

    lastSwarmOutput = "";
    const onlySwarm = msg.tool_calls.length === 1 && msg.tool_calls[0].function.name === "agent_swarm";
    let swarmResult: toolResult = { output: "" };

    for (let idx = 0; idx < msg.tool_calls.length; idx++) {
      const call = msg.tool_calls[idx];
      if (call.function.name !== "agent_swarm" && this.userAbortFired()) {
        return [extractLastAssistantText(messages), turns, "aborted", ""];
      }
      const id = call.id || `worker_call_${idx}`;
      this.toolStart(id, call.function.name, call.function.arguments);

      let result: toolResult;
      try {
        result = await Effect.runPromise(
          this.executeSwarmTool(ctx, id, call.function.name, call.function.arguments)
        );
      } catch (err: any) {
        result = { output: err.message || String(err), isErr: true };
      }
      this.toolResult(id, result.output, result.isErr, result.details);

      if (call.function.name === "agent_swarm") {
        lastSwarmOutput = result.output;
        if (onlySwarm) {
          swarmResult = result;
        }
      }

      let toolMsg: message;
      if (result.content && result.content.length > 0) {
        toolMsg = {
          role: "tool",
          tool_call_id: id,
          name: call.function.name,
          content: new TextEncoder().encode(JSON.stringify(result.content)),
        };
      } else {
        toolMsg = {
          role: "tool",
          tool_call_id: id,
          name: call.function.name,
          content: string_content(result.output),
        };
      }
      messages.push(toolMsg);
    }

    if (onlySwarm && swarmWorkerSectionCount(swarmResult.output) <= 1) {
      const output = resolveSwarmReturnOutput(swarmResult.output);
      if (output !== "") {
        if (!swarmResult.isErr) {
          return [output, turns, "ok", ""];
        }
        const payload = extractSwarmPayload(swarmResult.output);
        if (payload !== "") {
          return [payload, turns, "ok", ""];
        }
      }
      if (swarmResult.isErr) {
        return ["", turns, "error", swarmResult.output.trim()];
      }
    }

    if (lastSwarmOutput !== "" && swarmWorkerSectionCount(lastSwarmOutput) <= 1) {
      const output = resolveSwarmReturnOutput(lastSwarmOutput);
      return [output, turns, "ok", ""];
    }
  }
};

function linkedSwarmContext(parent: AbortSignal): [AbortSignal, () => void] {
  const controller = new AbortController();
  const onAbort = () => controller.abort();

  if (parent.aborted) {
    controller.abort();
  } else {
    parent.addEventListener("abort", onAbort);
  }

  const cancel = () => {
    parent.removeEventListener("abort", onAbort);
    controller.abort();
  };

  return [controller.signal, cancel];
}

function resolveWorkerOutput(finalText: string, lastSwarmOutput: string): string {
  const trimmed = finalText.trim();
  if (trimmed !== "" && !isSwarmStubText(trimmed)) {
    return trimmed;
  }
  return resolveSwarmReturnOutput(lastSwarmOutput);
}

function isSwarmStubText(s: string): boolean {
  const trimmed = s.toLowerCase().trim().replace(/[.!?]+$/g, "").replace(/\s+/g, " ");
  switch (trimmed) {
    case "":
    case "ok":
    case "okay":
    case "done":
    case "all done":
    case "complete":
    case "completed":
    case "task complete":
    case "task completed":
    case "finished":
      return true;
    default:
      return false;
  }
}

const swarmSectionHeader = /^##\s+(.+?)\s+\[(ok|error|aborted)\]\s*(?:\([^)]+\))?.*$/gm;

function resolveSwarmReturnOutput(output: string): string {
  const trimmed = output.trim();
  if (trimmed === "") {
    return "";
  }
  if (swarmWorkerSectionCount(output) === 1) {
    const payload = extractSwarmPayload(output);
    if (payload !== "") {
      return payload;
    }
  }
  return trimmed;
}

function swarmWorkerSectionCount(output: string): number {
  swarmSectionHeader.lastIndex = 0;
  const matches = output.match(swarmSectionHeader);
  return matches ? matches.length : 0;
}

function extractSwarmPayload(output: string): string {
  swarmSectionHeader.lastIndex = 0;
  if (!swarmSectionHeader.test(output)) {
    return "";
  }
  const [payload] = extractSwarmPayloadAtDepth(output, 0);
  return payload;
}

function extractSwarmPayloadAtDepth(output: string, depth: number): [string, number] {
  swarmSectionHeader.lastIndex = 0;
  const matches: RegExpExecArray[] = [];
  let m;
  while ((m = swarmSectionHeader.exec(output)) !== null) {
    matches.push(m);
  }

  let bestPayload = "";
  let bestDepth = -1;

  if (matches.length === 0) {
    const clean = cleanSwarmBody(output);
    if (clean === "") {
      return ["", -1];
    }
    return [clean, depth];
  }

  for (let i = 0; i < matches.length; i++) {
    const status = matches[i][2];
    const bodyStart = matches[i].index + matches[i][0].length;
    const bodyEnd = i + 1 < matches.length ? matches[i + 1].index : output.length;
    const body = output.substring(bodyStart, bodyEnd);

    const [child, childDepth] = extractSwarmPayloadAtDepth(body, depth + 1);
    if (child !== "" && childDepth > bestDepth) {
      bestPayload = child;
      bestDepth = childDepth;
    }
    if (status !== "ok") {
      continue;
    }
    const clean = cleanSwarmBody(body);
    if (clean !== "" && depth >= bestDepth) {
      bestPayload = clean;
      bestDepth = depth;
    }
  }
  return [bestPayload, bestDepth];
}

function cleanSwarmBody(body: string): string {
  const lines = body.split("\n");
  const cleanLines: string[] = [];
  for (const line of lines) {
    const trimmed = line.trim();
    if (trimmed === "") {
      cleanLines.push(line);
      continue;
    }
    if (trimmed === "(no output)" || trimmed.startsWith("Ran ") || trimmed.startsWith("Goal: ")) {
      continue;
    }
    if (trimmed.startsWith("agent_swarm: ") && trimmed.includes(" agents finished")) {
      continue;
    }
    if (trimmed.startsWith("## ")) {
      continue;
    }
    cleanLines.push(line);
  }
  return cleanLines.join("\n").trim();
}

function extractLastAssistantText(messages: message[]): string {
  for (let i = messages.length - 1; i >= 0; i--) {
    if (messages[i].role === "assistant") {
      const text = content_string(messages[i]).trim();
      if (text !== "") {
        return text;
      }
    }
  }
  return "";
}

Agent.prototype.planSwarmTasks = async function (
  this: Agent,
  ctx: AbortSignal,
  goal: string
): Promise<[swarmTask[], string]> {
  const planner = new Agent();
  planner.cfg = this.cfg;
  planner.client = this.client;
  planner.workDir = this.workDir;
  planner.swarmDepth = maxSwarmDepth; // planner cannot nest swarms

  const prompt = [
    "You are a planner for a parallel agent swarm.",
    "Goal: " + goal,
    "Break this into subtasks. Prefer INDEPENDENT subtasks that can run in parallel (split by file, module, or area).",
    "Assign at most one writer to any file in this swarm call. Split by module/path; never ask parallel workers to edit the same path.",
    "When one subtask genuinely needs another's result, express that with depends_on instead of forcing it into one task.",
    "Use your read-only tools to inspect the repo as needed.",
    `Reply with ONLY a JSON array. Each element: {"id": "short-label", "prompt": "complete self-contained instruction", "depends_on": ["other-id", ...]}.`,
    "Omit depends_on (or use []) for subtasks that can start immediately. Do not create cycles.",
    "Keep it to a sensible number of tasks (usually 2-12).",
  ].join("\n");

  try {
    const [output, turns, status, errMsg] = await planner.runPlannerLoop(ctx, prompt);
    if (status !== "ok") {
      return [[], errMsg || "planner failed"];
    }
    const tasks = parsePlannedSwarmTasks(output);
    if (tasks.length === 0) {
      return [[], "planner did not return any usable tasks"];
    }
    return [tasks, ""];
  } catch (err: any) {
    return [[], err.message || String(err)];
  }
};

Agent.prototype.runPlannerLoop = async function (
  this: Agent,
  ctx: AbortSignal,
  prompt: string
): Promise<[string, number, string, string]> {
  const tools = plannerTools();
  const messages: message[] = [
    { role: "system", content: string_content(systemPrompt) },
    { role: "user", content: string_content(prompt) },
  ];
  let turns = 0;

  while (true) {
    if (this.userAbortFired()) {
      return [extractLastAssistantText(messages), turns, "aborted", ""];
    }
    if (turns >= swarmAuxMaxIterations) {
      return [extractLastAssistantText(messages), turns, "error", "planner max iterations exceeded"];
    }

    const req: chat_request = {
      model: this.cfg.model,
      messages: messages,
      tools: tools,
    };
    apply_thinking_to_request(req, parse_thinking_level(this.cfg.thinking_level || ""), this.cfg.model);

    const [streamCtx, streamCancel] = linkedSwarmContext(ctx);
    let msg: message;
    try {
      msg = (await Effect.runPromise(
        this.client.chat_stream_retry(streamCtx, req, {})
      )) as message;
    } catch (err: any) {
      if (this.userAbortFired()) {
        return [extractLastAssistantText(messages), turns, "aborted", ""];
      }
      return [extractLastAssistantText(messages), turns, "error", err.message || String(err)];
    } finally {
      streamCancel();
    }

    turns++;
    messages.push(msg);

    if (!msg.tool_calls || msg.tool_calls.length === 0) {
      return [content_string(msg).trim(), turns, "ok", ""];
    }

    for (let idx = 0; idx < msg.tool_calls.length; idx++) {
      const call = msg.tool_calls[idx];
      const id = call.id || `planner_call_${idx}`;
      const result = this.executePlannerTool(ctx, call.function.name, call.function.arguments);
      let toolMsg: message;
      if (result.content && result.content.length > 0) {
        toolMsg = {
          role: "tool",
          tool_call_id: id,
          name: call.function.name,
          content: new TextEncoder().encode(JSON.stringify(result.content)),
        };
      } else {
        toolMsg = {
          role: "tool",
          tool_call_id: id,
          name: call.function.name,
          content: string_content(result.output),
        };
      }
      messages.push(toolMsg);
    }
  }
};

Agent.prototype.executePlannerTool = function (
  this: Agent,
  ctx: AbortSignal,
  name: string,
  argsJSON: string
): toolResult {
  switch (name) {
    case "read_file": {
      const effect = this.toolReadFile(argsJSON);
      try {
        return Effect.runSync(effect);
      } catch (err: any) {
        return { output: err.message || String(err), isErr: true };
      }
    }
    case "list_dir": {
      const effect = this.toolListDir(argsJSON);
      try {
        return Effect.runSync(effect);
      } catch (err: any) {
        return { output: err.message || String(err), isErr: true };
      }
    }
    case "glob": {
      const effect = this.toolGlob(argsJSON);
      try {
        return Effect.runSync(effect);
      } catch (err: any) {
        return { output: err.message || String(err), isErr: true };
      }
    }
    case "grep": {
      const effect = this.toolGrep(ctx, argsJSON);
      try {
        return Effect.runSync(effect);
      } catch (err: any) {
        return { output: err.message || String(err), isErr: true };
      }
    }
    default:
      return {
        output: `tool "${name}" is not available to the planner (read-only: read_file, list_dir, glob, grep)`,
        isErr: true,
      };
  }
};

const jsonArrayFence = /```(?:json)?\s*([\s\S]*?)```/is;

function parsePlannedSwarmTasks(text: string): swarmTask[] {
  let body = text;
  const m = text.match(jsonArrayFence);
  if (m && m.length === 2) {
    body = m[1];
  }
  const start = body.indexOf("[");
  const end = body.lastIndexOf("]");
  if (start === -1 || end === -1 || end < start) {
    return [];
  }
  let raw: any[] = [];
  try {
    raw = JSON.parse(body.substring(start, end + 1));
  } catch {
    return [];
  }
  const tasks: swarmTask[] = [];
  for (const entry of raw) {
    if (typeof entry === "string" && entry.trim() !== "") {
      tasks.push({ ID: "", Prompt: entry.trim() });
      continue;
    }
    if (entry && typeof entry === "object" && entry.prompt && entry.prompt.trim() !== "") {
      tasks.push({
        ID: (entry.id || "").trim(),
        Prompt: entry.prompt.trim(),
        DependsOn: entry.depends_on || [],
      });
    }
  }
  return tasks;
}

async function runSwarmWorkerPool(
  ctx: AbortSignal,
  tasks: swarmTask[],
  concurrency: number,
  runOne: (task: swarmTask, index: number) => Promise<swarmWorkerResult>,
  onEach: (r: swarmWorkerResult) => void
): Promise<swarmWorkerResult[]> {
  const results: swarmWorkerResult[] = new Array(tasks.length);
  let activeCount = 0;
  let taskIndex = 0;

  await new Promise<void>((resolve) => {
    const next = () => {
      if (taskIndex >= tasks.length && activeCount === 0) {
        resolve();
        return;
      }

      while (taskIndex < tasks.length && activeCount < concurrency) {
        const i = taskIndex++;
        const task = tasks[i];
        activeCount++;

        runOne(task, i)
          .then((r) => {
            results[i] = r;
            onEach(r);
            activeCount--;
            next();
          })
          .catch(() => {
            activeCount--;
            next();
          });
      }
    };
    next();
  });

  return results;
}

function swarmAbortedStub(task: swarmTask, index: number): swarmWorkerResult {
  return {
    ID: swarmTaskID(task, index),
    Prompt: task.Prompt,
    Status: "aborted",
    Output: "",
    Error: "",
    Turns: 0,
    Attempts: 1,
    Worktree: "",
    Branch: "",
  };
}

function resolveSwarmDependencies(tasks: swarmTask[]): number[][] {
  const idToIndex: Record<string, number> = {};
  for (let i = 0; i < tasks.length; i++) {
    idToIndex[swarmTaskID(tasks[i], i)] = i;
  }
  const depIndices: number[][] = new Array(tasks.length);
  for (let i = 0; i < tasks.length; i++) {
    depIndices[i] = [];
    const deps = tasks[i].DependsOn || [];
    for (const depID of deps) {
      const trimmed = depID.trim();
      const j = idToIndex[trimmed];
      if (j !== undefined && j !== i) {
        depIndices[i].push(j);
      }
    }
  }
  return depIndices;
}

function hasSwarmDependencies(depIndices: number[][]): boolean {
  for (const deps of depIndices) {
    if (deps && deps.length > 0) {
      return true;
    }
  }
  return false;
}

function buildSwarmUpstreamContext(deps: number[], results: swarmWorkerResult[]): string {
  if (deps.length === 0) {
    return "";
  }
  const blocks: string[] = [];
  for (const d of deps) {
    const r = results[d];
    let body = r.Output;
    if (body === "") {
      body = "(no output)";
    }
    blocks.push(`### Output from "${r.ID}"\n${body}`);
  }
  return "Results from the agent(s) this task depends on. Build on these directly — do not re-derive or guess what they produced:\n\n" + blocks.join("\n\n");
}

async function runSwarmDAGPool(
  ctx: AbortSignal,
  tasks: swarmTask[],
  depIndices: number[][],
  concurrency: number,
  runOne: (task: swarmTask, index: number) => Promise<swarmWorkerResult>,
  onEach: (r: swarmWorkerResult) => void
): Promise<swarmWorkerResult[]> {
  const results: swarmWorkerResult[] = new Array(tasks.length);
  const done = new Array(tasks.length).fill(false);
  let doneCount = 0;
  const inflight = new Set<number>();
  const waiters = new Set<() => void>();

  const triggerWaiters = () => {
    for (const w of waiters) {
      w();
    }
  };

  return new Promise<swarmWorkerResult[]>((resolve) => {
    const checkAndSchedule = () => {
      if (doneCount >= tasks.length) {
        resolve(results);
        return;
      }

      let scheduled = 0;
      for (let i = 0; i < tasks.length && inflight.size < concurrency; i++) {
        if (done[i] || inflight.has(i)) {
          continue;
        }

        const deps = depIndices[i] || [];
        let ready = true;
        for (const d of deps) {
          if (!done[d]) {
            ready = false;
            break;
          }
        }
        if (!ready) {
          continue;
        }

        if (ctx.aborted) {
          continue;
        }

        let failedDep: number | null = null;
        for (const d of deps) {
          if (results[d].Status !== "ok") {
            failedDep = d;
            break;
          }
        }

        if (failedDep !== null) {
          const r: swarmWorkerResult = {
            ID: swarmTaskID(tasks[i], i),
            Prompt: tasks[i].Prompt,
            Status: "aborted",
            Output: "",
            Error: `skipped: dependency "${results[failedDep].ID}" did not succeed`,
            Turns: 0,
            Attempts: 1,
            Worktree: "",
            Branch: "",
          };
          results[i] = r;
          done[i] = true;
          doneCount++;
          onEach(r);
          scheduled++;
          triggerWaiters();
          continue;
        }

        const task = { ...tasks[i] };
        const upstream = buildSwarmUpstreamContext(deps, results);
        if (upstream !== "") {
          task.upstream = upstream;
        }

        const index = i;
        inflight.add(index);
        scheduled++;

        runOne(task, index)
          .then((r) => {
            results[index] = r;
            done[index] = true;
            doneCount++;
            onEach(r);
            inflight.delete(index);
            triggerWaiters();
            checkAndSchedule();
          })
          .catch(() => {
            inflight.delete(index);
            triggerWaiters();
            checkAndSchedule();
          });
      }

      if (scheduled === 0 && inflight.size === 0) {
        // Unresolved dependency cycle or aborted
        for (let i = 0; i < tasks.length; i++) {
          if (!done[i]) {
            const reason = ctx.aborted ? "" : "skipped: unresolved dependency cycle";
            const r = swarmAbortedStub(tasks[i], i);
            if (reason !== "") {
              r.Error = reason;
            }
            results[i] = r;
            done[i] = true;
            doneCount++;
            onEach(r);
          }
        }
        resolve(results);
      }
    };

    const loop = async () => {
      while (doneCount < tasks.length) {
        checkAndSchedule();
        if (doneCount >= tasks.length) {
          break;
        }
        await new Promise<void>((res) => {
          const w = () => {
            waiters.delete(w);
            res();
          };
          waiters.add(w);
        });
      }
    };

    loop();
  });
}

function aggregateSwarmOutput(workers: swarmWorkerResult[], concurrency: number, goal: string): string {
  let ok = 0;
  let failed = 0;
  let aborted = 0;
  for (const w of workers) {
    switch (w.Status) {
      case "ok":
        ok++;
        break;
      case "error":
        failed++;
        break;
      case "aborted":
        aborted++;
        break;
    }
  }

  let header = `Ran ${workers.length} agent(s) at concurrency ${concurrency} — ${ok} ok`;
  if (failed > 0) {
    header += `, ${failed} error`;
  }
  if (aborted > 0) {
    header += `, ${aborted} aborted`;
  }
  header += ".";

  const parts: string[] = [];
  if (goal !== "") {
    parts.push(`Goal: ${goal}`);
  }
  parts.push(header, "");

  for (const w of workers) {
    parts.push(swarmWorkerSection(w));
  }
  return parts.join("\n");
}

function swarmWorkerSection(w: swarmWorkerResult): string {
  let turns = `${w.Turns} turn`;
  if (w.Turns !== 1) {
    turns += "s";
  }
  let retry = "";
  if (w.Attempts > 1) {
    retry = ` ×${w.Attempts}`;
  }
  let header = `## ${w.ID} [${w.Status}] (${turns}${retry})`;
  if (w.Worktree !== "" && w.Branch !== "") {
    header += ` (worktree: ${w.Worktree} · branch: ${w.Branch})`;
  }
  if (w.Status !== "ok" && w.Error !== "") {
    return `${header}\nError: ${w.Error}`;
  }
  let body = w.Output;
  if (body === "") {
    body = "(no output)";
  }
  return `${header}\n${body}`;
}

/*
PORT STATUS
source path: backend/agent/agent_swarm.go
source lines: 1103
draft lines: 750
confidence: high
status: phase_b_compile
*/
