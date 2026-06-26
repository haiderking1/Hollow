import { Effect } from "effect";
import { type message, type tool, string_content, content_string, get_reasoning, type chat_request } from "../opencode/types";
import { type tool_content_block, tool_content_from_agent } from "../opencode/content";
import { apply_thinking_to_request, parse_thinking_level } from "../opencode/thinking";
import { ToolName as MemoryToolName } from "../memory/tool";
import { type runtime, continuity_enabled, goal_lock_enabled } from "../config/config";
import { type manager } from "../session/manager";
import { type file_entry } from "../session/types";
import { Store } from "../memory/store";
import { BuildSessionSystemPromptParts, type SystemPromptParts } from "./system_prompt";
import { LoadSoul, soul_mtime_ms } from "../memory/soul";
import { ledger } from "./evidence/ledger";
import { registry as obligationsRegistry } from "./obligations/registry";
import { new_client_for_runtime } from "../opencode/runtime_client";
import { client } from "../opencode/client";
import { goalLockNotice } from "./goal_lock";
import { userMessageSignalsProfileCorrection } from "./memory_correction";
export { userMessageSignalsProfileCorrection };
import { ModelContextWindow } from "./models";
import {
  should_compact,
  estimate_context_tokens,
  calculate_context_tokens,
  get_latest_compaction_entry
} from "../session/compaction_utils";
import { repair_tool_messages } from "../opencode/messages";
import {
  event_assistant_start,
  event_assistant_thinking_delta,
  event_assistant_delta,
  event_tool_start,
  event_tool_delta,
  event_tool_result,
  event_system,
  event_error,
  event_compaction_start,
  event_compaction_end
} from "../core/events";
import { manager as McpManager } from "../mcp/manager";
import { IsContextOverflowError } from "./overflow";
import { detect_verify_command } from "./obligations/derive";
import { extract_task_verify_commands } from "./obligations/match";
import { seed_continuity_reads } from "./evidence/continuity";
import { sessionFingerprints } from "./session_fingerprints";

export interface toolResult {
  output: string;
  content?: tool_content_block[];
  isErr?: boolean;
  details?: string; // JSON string
}

export const WriteOriginForeground = "foreground";
export const WriteOriginBackgroundReview = "background-review";

export interface swarmTask {
  ID: string;
  Prompt: string;
  DependsOn?: string[];
  upstream?: string;
}

export interface swarmWorkerResult {
  ID: string;
  Prompt: string;
  Status: string; // ok, error, aborted
  Output: string;
  Error: string;
  Turns: number;
  Attempts: number;
  Worktree: string;
  Branch: string;
}

export const maxSwarmDepth = 3;

export class Agent {
  cfg!: runtime;
  client!: client;
  workDir!: string;
  session: manager | null = null;
  messages: message[] = [];
  busy = false;
  cancel: (() => void) | null = null;
  userAbortCtx: AbortSignal | null = null;
  userAbortCancel: (() => void) | null = null;
  swarmDepth = 0;
  ledger: ledger | null = null;
  obligations: obligationsRegistry | null = null;
  allowedTools: Record<string, boolean> | null = null;
  readonlyRole = false;
  lastUserPrompt = "";
  lockedGoal = "";
  verifyFailures = 0;
  parallelForksAttempted = false;
  turnCtx: AbortSignal | null = null;
  step: {
    lastVerifyFailed: boolean;
    failurePaths: string[] | null;
    lastVerifyOutput: string;
    lastBashCommand: string;
    lastBashFailed: boolean;
  } = {
    lastVerifyFailed: false,
    failurePaths: null,
    lastVerifyOutput: "",
    lastBashCommand: "",
    lastBashFailed: false,
  };
  completionRounds = 0;
  compactionCancel: (() => void) | null = null;
  overflowRecoveryAttempted = false;
  memStore: Store | null = null;
  cachedSystemPrompt = "";
  /** Stable+context tiers cached for prefix stability; volatile tier rebuilt every turn. */
  cachedStableContextPrompt = "";
  /** Tracks SOUL.md mtime baked into cachedStableContextPrompt; rebuild when the file changes. */
  soulPromptMtime = -1;
  emit: ((event: any) => void) | null = null;
  notify: ((msg: string) => void) | null = null;
  approvalPrompt: ((subsystem: string, pendingId: string) => void) | null = null;
  writeOrigin = WriteOriginForeground;
  userTurnCount = 0;
  turnsSinceMemory = 0;
  itersSinceSkill = 0;
  activeTools: Record<string, string> = {};
  activeBashCmd: any = null; // Used by bash tool
  maxIterations = 0;
  reviewWG: Promise<void>[] = [];
  mcpManager: McpManager | null = null;

  resolvePath!: (p: string) => Effect.Effect<string, Error>;

  toolReadFile!: (argsJSON: string) => Effect.Effect<toolResult, Error>;
  toolWriteFile!: (argsJSON: string) => Effect.Effect<toolResult, Error>;
  toolEditFile!: (argsJSON: string) => Effect.Effect<toolResult, Error>;
  toolListDir!: (argsJSON: string) => Effect.Effect<toolResult, Error>;
  toolGlob!: (argsJSON: string) => Effect.Effect<toolResult, Error>;
  toolGrep!: (ctx: AbortSignal, argsJSON: string) => Effect.Effect<toolResult, Error>;
  toolWebSearch!: (ctx: AbortSignal, argsJSON: string) => Effect.Effect<toolResult, Error>;
  toolWebFetch!: (ctx: AbortSignal, argsJSON: string) => Effect.Effect<toolResult, Error>;
  toolBrowser!: (ctx: AbortSignal, argsJSON: string) => Effect.Effect<toolResult, Error>;
  toolBash!: (ctx: AbortSignal, id: string, argsJSON: string) => Effect.Effect<toolResult, Error>;

  prompt!: (
    ctx: AbortSignal,
    cfg: runtime,
    userText: string,
    attachments: any[] | null,
    runtimeNotice: string,
    emit: ((event: any) => void) | null
  ) => Promise<void>;


  LoopPrompt!: (
    ctx: AbortSignal,
    cfg: runtime,
    lockedPrompt: string,
    emit: ((event: any) => void) | null
  ) => Promise<void>;

  LoopContinue!: (
    ctx: AbortSignal,
    cfg: runtime,
    lockedPrompt: string,
    iteration: number,
    emit: ((event: any) => void) | null
  ) => Promise<void>;

  toolAgentSwarm!: (ctx: AbortSignal, callID: string, argsJSON: string, depth: number) => Effect.Effect<toolResult, Error>;
  runWorkerLoop!: (ctx: AbortSignal, prompt: string, maxTurns: number) => Promise<[string, number, string, string]>;
  toolSkillsList!: (argsJSON: string) => Effect.Effect<toolResult, Error>;
  toolSkillView!: (argsJSON: string) => Effect.Effect<toolResult, Error>;
  toolSkillManage!: (argsJSON: string) => Effect.Effect<toolResult, Error>;
  toolMemory!: (argsJSON: string) => Effect.Effect<toolResult, Error>;
  runSwarmWorkerInDir!: (
    ctx: AbortSignal,
    task: swarmTask,
    index: number,
    depth: number,
    sharedContext: string,
    retries: number,
    maxTurns: number,
    workDir: string
  ) => Promise<swarmWorkerResult>;

  resetEvidenceLedger!: (turnID: string) => void;
  runSwarmWorker!: (
    ctx: AbortSignal,
    task: swarmTask,
    index: number,
    depth: number,
    sharedContext: string,
    retries: number,
    maxTurns: number
  ) => Promise<swarmWorkerResult>;
  runIsolatedSwarmWorker!: (
    ctx: AbortSignal,
    task: swarmTask,
    index: number,
    depth: number,
    sharedContext: string,
    retries: number,
    maxTurns: number,
    repoRoot: string,
    runID: string
  ) => Promise<swarmWorkerResult>;
  planSwarmTasks!: (ctx: AbortSignal, goal: string) => Promise<[swarmTask[], string]>;
  runPlannerLoop!: (ctx: AbortSignal, prompt: string) => Promise<[string, number, string, string]>;
  executePlannerTool!: (ctx: AbortSignal, name: string, argsJSON: string) => toolResult;
  Prompt!: (
    ctx: AbortSignal,
    cfg: runtime,
    userText: string,
    attachments: any[] | null,
    emit: ((event: any) => void) | null
  ) => Promise<void>;


  recordCommandRun!: (command: string, exitCode: number, text: string, duration: number) => void;
  evidenceEnabled!: () => boolean;
  obligationRegistry!: () => obligationsRegistry | null;
  runVerifier!: (ctx: AbortSignal) => Promise<string[]>;
  evidenceLedger!: () => ledger;

  noteVerifyFailure!: () => void;
  noteVerifySuccess!: () => void;
  maybeParallelForks!: () => void | Promise<void>;

  registerBashCmd!: (cmd: any) => void;
  unregisterBashCmd!: (cmd: any) => void;
  killActiveBash!: () => void;
  currentLockedGoal!: () => string;
  enforceCompletion!: (ctx: AbortSignal) => Promise<boolean>;
  notifyStagedWrite!: (toolOutput: string) => void;
  notifyDirectMemoryWrite!: (argsJSON: string, toolOutput: string) => void;
  noteMutation!: () => void;
  recordStepOutcome!: (name: string, argsJSON: string, result: toolResult) => void;
  scoreToolStep!: (name: string, argsJSON: string, result: toolResult) => toolResult | null;
  executeTool!: (ctx: AbortSignal, id: string, name: string, argsJSON: string) => Effect.Effect<toolResult, Error>;
  dispatchTool!: (ctx: AbortSignal, id: string, name: string, argsJSON: string) => Effect.Effect<toolResult, Error>;
  executeSwarmTool!: (ctx: AbortSignal, id: string, name: string, argsJSON: string) => Effect.Effect<toolResult, Error>;
  guardTool!: (name: string, argsJSON: string) => toolResult | null;
  fileHashIfExists!: (argsJSON: string) => string;
  recordEvidence!: (name: string, argsJSON: string, beforeHash: string) => void;

  toolMenu!: () => tool[];
  Compact!: (ctx: AbortSignal, customInstructions: string) => Promise<any>;
  RunAutoCompaction!: (ctx: AbortSignal, reason: string, willRetry: boolean) => Promise<boolean>;
  NavigateToEntry!: (ctx: AbortSignal, targetID: string, opts: any) => Promise<boolean>;
  maybeSpawnBackgroundReview!: (shouldReviewMemory: boolean) => void;
  runBackgroundReview!: (cfg: runtime, cachedPrompt: string, snapshot: message[], prompt: string, notify: ((msg: string) => void) | null) => void;
  ReloadMessagesFromSession!: () => void;
  emitEvidence!: (kind: string, path: string) => void;
  emitObligations!: () => void;

  runParallelForks!: (
    ctx: AbortSignal,
    lockedGoal: string,
    lastOutput: string,
    verifyCmd: string,
    forkCount: number
  ) => Promise<[string, boolean]>;

  constructor() {
    this.messages = [];
    this.busy = false;
    this.swarmDepth = 0;
    this.verifyFailures = 0;
    this.parallelForksAttempted = false;
    this.lockedGoal = "";
    this.writeOrigin = WriteOriginForeground;
    this.mcpManager = new McpManager();
    this.activeTools = {};
    this.reviewWG = [];

    const proto = Object.getPrototypeOf(this);
    for (const key of Object.getOwnPropertyNames(proto)) {
      if (key !== "constructor") {
        const desc = Object.getOwnPropertyDescriptor(this, key);
        if (desc && desc.writable && desc.configurable && (this as any)[key] === undefined) {
          delete (this as any)[key];
        }
      }
    }
  }

  Close(): void {
    if (this.mcpManager !== null && this.swarmDepth === 0) {
      this.mcpManager.close();
    }
  }

  initMemoryStore(): void {
    if (!this.cfg.memory?.memory_enabled && !this.cfg.memory?.user_profile_enabled) {
      this.memStore = null;
      return;
    }
    this.memStore = new Store(
      this.cfg.memory.memory_char_limit ?? 2200,
      this.cfg.memory.user_char_limit ?? 1375
    );
    this.memStore.loadFromDisk();
  }

  Session(): manager | null {
    return this.session;
  }

  LoadSession(sm: manager | null): void {
    const sessionChanged = this.session === null || sm === null || this.session.session_id() !== sm.session_id();
    let cwdChanged = false;
    if (sm !== null && sm.cwd() !== "" && sm.cwd() !== this.workDir) {
      this.workDir = sm.cwd();
      cwdChanged = true;
    }
    this.session = sm;
    this.invalidateSystemPrompt();
    if (sessionChanged || cwdChanged) {
      this.userTurnCount = 0;
      this.turnsSinceMemory = 0;
      this.itersSinceSkill = 0;
    }
    this.messages = [
      { role: "system", content: string_content(this.systemPrompt()) }
    ];
    if (sm !== null) {
      this.messages = [...this.messages, ...repair_tool_messages(sm.messages())];
    }
  }

  async Reset(): Promise<void> {
    this.invalidateSystemPrompt();
    this.userTurnCount = 0;
    this.turnsSinceMemory = 0;
    this.itersSinceSkill = 0;

    if (this.session !== null) {
      await Effect.runPromise(this.session.new_session());
    }

    this.messages = [
      { role: "system", content: string_content(this.systemPrompt()) }
    ];
  }

  persist(msg: message): void {
    this.persistWithDetails(msg, "");
  }

  persistWithDetails(msg: message, toolDetails: string): void {
    if (this.session === null || msg.role === "system") {
      return;
    }
    Effect.runPromise(
      Effect.ignore(
        this.session.append_message_with_details(msg, toolDetails)
      )
    ).catch(() => {});
  }

  Abort(): void {
    const cancel = this.cancel;
    const compactionCancel = this.compactionCancel;
    const userAbortCancel = this.userAbortCancel;
    this.killActiveBash();
    if (userAbortCancel !== null) {
      userAbortCancel();
    }
    if (cancel !== null) {
      cancel();
    }
    if (compactionCancel !== null) {
      compactionCancel();
    }
    if (this.mcpManager !== null && this.swarmDepth === 0) {
      this.mcpManager.close();
    }
  }

  AbortCompaction(): void {
    const compactionCancel = this.compactionCancel;
    if (compactionCancel !== null) {
      compactionCancel();
    }
  }

  async AbortAndWait(): Promise<void> {
    const cancel = this.cancel;
    const compactionCancel = this.compactionCancel;
    const userAbortCancel = this.userAbortCancel;
    this.killActiveBash();
    if (userAbortCancel !== null) {
      userAbortCancel();
    }
    if (cancel !== null) {
      cancel();
    }
    if (compactionCancel !== null) {
      compactionCancel();
    }

    while (true) {
      if (!this.busy) {
        break;
      }
      await new Promise((resolve) => setTimeout(resolve, 10));
    }
  }

  UpdateConfig(cfg: runtime): void {
    this.applyConfigLocked(cfg);
  }

  applyConfigLocked(cfg: runtime): void {
    if (JSON.stringify(this.cfg) === JSON.stringify(cfg)) {
      return;
    }
    const promptAffecting =
      JSON.stringify(this.cfg?.memory) !== JSON.stringify(cfg.memory) ||
      JSON.stringify(this.cfg?.skills) !== JSON.stringify(cfg.skills) ||
      this.cfg?.model !== cfg.model ||
      this.cfg?.provider !== cfg.provider ||
      this.cfg?.thinking_level !== cfg.thinking_level;
    const mcpChanged = JSON.stringify(this.cfg?.mcp_servers) !== JSON.stringify(cfg.mcp_servers);
    this.cfg = cfg;
    this.client = new_client_for_runtime(cfg);
    if (promptAffecting) {
      this.initMemoryStore();
      this.invalidateSystemPrompt();
    }
    if (mcpChanged && this.mcpManager !== null && this.swarmDepth === 0) {
      const ctx = this.turnCtx || new AbortController().signal;
      Effect.runPromise(
        Effect.ignore(
          this.mcpManager.reload(ctx, cfg.mcp_servers || {})
        )
      ).catch(() => {});
    }
  }

  SetEmit(emit: (event: any) => void): void {
    this.emit = emit;
  }

  SetNotify(notify: (msg: string) => void): void {
    this.notify = notify;
  }

  SetApprovalPrompt(fn: (subsystem: string, pendingId: string) => void): void {
    this.approvalPrompt = fn;
  }

  MemoryStore(): Store | null {
    return this.memStore;
  }

  emitCompactionEnd(reason: string, result: any, aborted: boolean, willRetry: boolean, errMsg: string): void {
    if (this.emit !== null) {
      this.emit({
        kind: event_compaction_end,
        data: {
          reason: reason,
          result: result,
          aborted: aborted,
          will_retry: willRetry,
          error_message: errMsg,
        },
      });
    }
  }

  hydrateNudgeCountersLocked(): void {
    if (this.userTurnCount !== 0 || this.session === null) {
      return;
    }
    let priorUserTurns = 0;
    for (const msg of this.session.messages()) {
      if (msg.role === "user") {
        priorUserTurns++;
      }
    }
    if (priorUserTurns === 0) {
      return;
    }
    this.userTurnCount = priorUserTurns;
    if (this.cfg.memory?.nudge_interval && this.cfg.memory.nudge_interval > 0 && this.turnsSinceMemory === 0) {
      this.turnsSinceMemory = priorUserTurns % this.cfg.memory.nudge_interval;
    }
  }

  emitToolStart(id: string, name: string, args: string): void {
    if (this.emit !== null) {
      this.activeTools[id] = name;
      this.emit({
        kind: event_tool_start,
        data: { id: id, name: name, args: args },
      });
    }
  }

  emitToolDelta(id: string, chunk: string): void {
    if (this.emit !== null && chunk !== "") {
      const name = this.activeTools[id] || "";
      this.emit({
        kind: event_tool_delta,
        data: { id: id, name: name, result: chunk },
      });
    }
  }

  emitToolResult(id: string, result: string, isErr: boolean, details?: string): void {
    if (this.emit !== null) {
      const name = this.activeTools[id] || "";
      delete this.activeTools[id];
      let rawDetails: any = null;
      if (details) {
        try {
          rawDetails = JSON.parse(details);
        } catch {}
      }
      this.emit({
        kind: event_tool_result,
        data: { id: id, name: name, result: result, error: isErr, details: rawDetails },
      });
    }
  }

  toolStart(id: string, name: string, args: string): void {
    this.emitToolStart(id, name, args);
  }

  toolDelta(id: string, chunk: string): void {
    this.emitToolDelta(id, chunk);
  }

  toolResult(id: string, output: string, isErr?: boolean, details?: string): void {
    this.emitToolResult(id, output, isErr || false, details);
  }

  userAbortFired(): boolean {
    if (this.userAbortCtx !== null && this.userAbortCtx.aborted) {
      return true;
    }
    return false;
  }

  emitCompactionStart(reason: string): void {
    if (this.emit !== null) {
      this.emit({
        kind: event_compaction_start,
        data: { reason: reason },
      });
    }
  }

  systemPrompt(): string {
    const soulMtime = soul_mtime_ms();
    const parts = this.buildSessionPromptParts();
    const stableContext = [parts.stable, parts.context].filter((p) => p.trim() !== "").join("\n\n");
    if (this.cachedStableContextPrompt === "" || soulMtime !== this.soulPromptMtime) {
      this.cachedStableContextPrompt = stableContext;
      this.soulPromptMtime = soulMtime;
    }
    const full = [this.cachedStableContextPrompt, parts.volatile].filter((p) => p.trim() !== "").join("\n\n");
    if (full !== this.cachedSystemPrompt) {
      this.cachedSystemPrompt = full;
      this.persistSystemPrompt();
    }
    return full;
  }

  buildSessionPromptParts(): SystemPromptParts {
    const tools = this.toolMenu();
    const toolNames = tools.map((t) => t.function.name);
    const sessionID = this.session ? this.session.session_id() : "";
    return BuildSessionSystemPromptParts({
      WorkDir: this.workDir,
      Cfg: this.cfg,
      ToolNames: toolNames,
      Store: this.memStore,
      SessionID: sessionID,
      PreloadedSkillsPrompt: this.cfg.preloaded_skills_prompt,
    });
  }

  buildSessionPrompt(): string {
    const parts = this.buildSessionPromptParts();
    return [parts.stable, parts.context, parts.volatile].filter((p) => p.trim() !== "").join("\n\n");
  }

  persistSystemPrompt(): void {
    if (this.session && this.cachedSystemPrompt !== "") {
      Effect.runPromise(
        Effect.ignore(
          this.session.set_system_prompt(this.cachedSystemPrompt)
        )
      ).catch(() => {});
    }
  }

  invalidateSystemPrompt(): void {
    this.cachedSystemPrompt = "";
    this.cachedStableContextPrompt = "";
    this.soulPromptMtime = -1;
    if (this.memStore !== null) {
      this.memStore.loadFromDisk();
    }
    if (this.messages.length > 0 && this.messages[0].role === "system") {
      const prompt = this.systemPrompt();
      this.messages[0].content = string_content(prompt);
    }
  }

  InvalidateSystemPrompt(): void {
    this.invalidateSystemPrompt();
  }

  Cfg(): runtime {
    return this.cfg;
  }

  WorkDir(): string {
    return this.workDir;
  }

  MCPManager(): McpManager | null {
    return this.mcpManager;
  }

  // --- Emit helpers ---

  streamStart(): void {
    if (this.emit !== null) {
      this.emit({ kind: event_assistant_start });
    }
  }

  streamDelta(text: string): void {
    if (this.emit !== null && text !== "") {
      this.emit({ kind: event_assistant_delta, data: text });
    }
  }

  thinkingDelta(text: string): void {
    if (this.emit !== null && text !== "") {
      this.emit({ kind: event_assistant_thinking_delta, data: text });
    }
  }

  interrupted(): void {
    if (this.emit !== null) {
      this.emit({ kind: event_system, data: "interrupted" });
    }
  }

  emitError(text: string): void {
    if (this.emit !== null) {
      this.emit({ kind: event_error, data: text });
    }
  }

  appendToolStubs(calls: { id: string; function: { name: string } }[], text: string): void {
    for (let idx = 0; idx < calls.length; idx++) {
      const call = calls[idx];
      let id = call.id;
      if (!id || id === "") {
        id = `call_${idx}`;
      }
      const toolMsg: message = {
        role: "tool",
        tool_call_id: id,
        name: call.function.name,
        content: string_content(text),
      };
      this.messages.push(toolMsg);
      this.persist(toolMsg);
    }
  }

  // --- Main inference loop ---

  async runLoop(ctx: AbortSignal): Promise<void> {
    this.turnCtx = ctx;

    const tools = this.toolMenu();
    let iterations = 0;

    while (true) {
      iterations++;
      if (this.maxIterations > 0 && iterations > this.maxIterations) {
        return;
      }

      if (ctx.aborted) {
        this.interrupted();
        return;
      }

      // Build messages for the LLM
      let messages: message[];
      if (this.session !== null) {
        const sessionMsgs = this.session.build_session_context().messages || [];
        const repaired = repair_tool_messages(sessionMsgs);
        messages = [
          { role: "system", content: string_content(this.systemPrompt()) },
          ...repaired,
        ];
      } else {
        messages = [...this.messages];
        if (messages.length > 0 && messages[0].role === "system") {
          messages[0].content = string_content(this.systemPrompt());
        }
      }
      const cfg = this.cfg;

      let streamStarted = false;
      let streamedReasoningLen = 0;
      let streamedTextLen = 0;
      const startStream = () => {
        if (streamStarted) return;
        streamStarted = true;
        this.streamStart();
      };
      const flushStreamTail = (finalMsg: message): void => {
        const reasoning = get_reasoning(finalMsg);
        if (reasoning !== "" && reasoning.length > streamedReasoningLen) {
          startStream();
          this.thinkingDelta(reasoning.slice(streamedReasoningLen));
        }
        const text = content_string(finalMsg);
        if (text !== "" && text.length > streamedTextLen) {
          startStream();
          this.streamDelta(text.slice(streamedTextLen));
        }
      };

      const req: chat_request = {
        model: cfg.model,
        messages: messages,
        tools: tools,
      };
      apply_thinking_to_request(req, parse_thinking_level(cfg.thinking_level || ""), cfg.model);

      let msg: message;
      try {
        msg = await Effect.runPromise(
          this.client.chat_stream(ctx, req, {
            on_thinking: (delta: string) => {
              startStream();
              this.thinkingDelta(delta);
              streamedReasoningLen += delta.length;
            },
            on_text: (delta: string) => {
              startStream();
              this.streamDelta(delta);
              streamedTextLen += delta.length;
            },
          })
        );
      } catch (err: any) {
        if (ctx.aborted) {
          this.interrupted();
          return;
        }
        // Check for context overflow
        const errObj = err instanceof Error ? err : new Error(String(err?.reason || err?.message || err));
        if (this.session !== null && IsContextOverflowError(errObj)) {
          if (!this.overflowRecoveryAttempted) {
            this.overflowRecoveryAttempted = true;
            if (this.messages.length > 0 && this.messages[this.messages.length - 1].role === "assistant") {
              this.messages.pop();
            }
            try {
              const compacted = await this.RunAutoCompaction(ctx, "overflow", true);
              if (compacted) {
                continue;
              }
            } catch {}
          } else {
            this.emitCompactionEnd("overflow", null, false, false,
              "Context overflow recovery failed after one compact-and-retry attempt. Try reducing context or switching to a larger-context model.");
          }
        }
        this.emitError(errObj.message);
        throw errObj;
      }

      // No tool calls — text-only response
      if (!msg.tool_calls || msg.tool_calls.length === 0) {
        flushStreamTail(msg);
        this.messages.push(msg);
        this.persist(msg);

        // Completion contract: enforce obligations
        const shouldContinue = await this.enforceCompletion(ctx);
        if (shouldContinue) {
          continue;
        }

        // Post-turn compaction check
        if (this.session !== null && cfg.compaction?.enabled) {
          const pathEntries = this.session.get_branch(this.session.leaf_id());
          const compactionEntry = get_latest_compaction_entry(pathEntries);
          let lastAsst: file_entry | null = null;
          for (let i = pathEntries.length - 1; i >= 0; i--) {
            if (pathEntries[i].type === "message" && pathEntries[i].message && pathEntries[i].message!.role === "assistant") {
              lastAsst = pathEntries[i];
              break;
            }
          }
          let skip = false;
          if (lastAsst !== null && compactionEntry !== null) {
            const tAsst = new Date(lastAsst.timestamp || "");
            const tComp = new Date(compactionEntry.timestamp || "");
            if (tAsst <= tComp) {
              skip = true;
            }
          }

          if (!skip) {
            const contextWindow = ModelContextWindow(cfg.provider, cfg.model, cfg.compaction.context_window || 0);
            let tokens = 0;
            if (msg.usage) {
              tokens = calculate_context_tokens(msg.usage);
            } else {
              let lastUsageEntry: file_entry | null = null;
              for (let i = pathEntries.length - 1; i >= 0; i--) {
                if (pathEntries[i].type === "message" && pathEntries[i].message &&
                    pathEntries[i].message!.role === "assistant" && pathEntries[i].message!.usage) {
                  lastUsageEntry = pathEntries[i];
                  break;
                }
              }
              let usageSkip = false;
              if (compactionEntry !== null && lastUsageEntry !== null) {
                const tUsage = new Date(lastUsageEntry.timestamp || "");
                const tComp2 = new Date(compactionEntry.timestamp || "");
                if (tUsage <= tComp2) {
                  usageSkip = true;
                }
              }
              if (!usageSkip) {
                const sessionMsgs = this.session.build_session_context().messages || [];
                tokens = estimate_context_tokens(sessionMsgs).tokens;
              } else {
                skip = true;
              }
            }
            if (!skip && should_compact(tokens, contextWindow, cfg.compaction)) {
              await this.RunAutoCompaction(ctx, "threshold", false);
            }
          }
        }
        return;
      }

      // Has tool calls — process them
      flushStreamTail(msg);
      this.messages.push(msg);
      if (cfg.memory?.skill_nudge_interval && cfg.memory.skill_nudge_interval > 0) {
        this.itersSinceSkill++;
      }
      this.persist(msg);

      for (let idx = 0; idx < msg.tool_calls!.length; idx++) {
        if (ctx.aborted) {
          this.appendToolStubs(msg.tool_calls!.slice(idx), "Interrupted");
          this.interrupted();
          return;
        }

        const call = msg.tool_calls![idx];
        let id = call.id;
        if (!id || id === "") {
          id = `call_${idx}`;
          call.id = id;
        }

        this.emitToolStart(id, call.function.name, call.function.arguments);

        let result: toolResult;
        try {
          result = await Effect.runPromise(this.executeTool(ctx, id, call.function.name, call.function.arguments));
        } catch (execErr: any) {
          result = { output: execErr?.message || String(execErr), isErr: true };
        }
        this.emitToolResult(id, result.output, result.isErr || false, result.details);
        this.notifyStagedWrite(result.output);
        if (call.function.name === MemoryToolName) {
          this.notifyDirectMemoryWrite(call.function.arguments, result.output);
        }

        let toolMsg: message;
        if (result.content && result.content.length > 0) {
          const toolContent = tool_content_from_agent(result.content);
          toolMsg = {
            role: "tool",
            tool_call_id: id,
            name: call.function.name,
            content: toolContent || string_content(result.output),
          };
        } else {
          toolMsg = {
            role: "tool",
            tool_call_id: id,
            name: call.function.name,
            content: string_content(result.output),
          };
        }

        this.messages.push(toolMsg);
        this.persistWithDetails(toolMsg, result.details || "");
      }
    }
  }
}

export function New(cfg: runtime, workDir: string, sm: manager | null): Agent {
  if (workDir === "" && sm !== null && sm.cwd() !== "") {
    workDir = sm.cwd();
  }
  if (workDir === "") {
    workDir = process.cwd();
  }

  const a = new Agent();
  a.cfg = cfg;
  a.client = new_client_for_runtime(cfg);
  a.workDir = workDir;
  a.session = sm;
  a.writeOrigin = WriteOriginForeground;
  a.initMemoryStore();

  if (cfg.mcp_servers && Object.keys(cfg.mcp_servers).length > 0) {
    try {
      const ctx = a.turnCtx || new AbortController().signal;
      Effect.runPromise(
        Effect.ignore(
          a.mcpManager!.reload(ctx, cfg.mcp_servers)
        )
      ).catch(() => {});
    } catch {}
  }

  a.messages = [
    { role: "system", content: string_content(a.systemPrompt()) }
  ];

  if (sm !== null) {
    a.messages = [...a.messages, ...repair_tool_messages(sm.messages())];
  }

  return a;
}

// Assign prototype methods
Agent.prototype.prompt = async function (
  this: Agent,
  ctx: AbortSignal,
  cfg: runtime,
  userText: string,
  attachments: any[] | null,
  runtimeNotice: string,
  emit: ((event: any) => void) | null
): Promise<void> {
  if (this.busy) {
    throw new Error("agent is already processing");
  }
  this.busy = true;

  const controller = new AbortController();
  const onAbort = () => controller.abort();
  ctx.addEventListener("abort", onAbort);
  this.cancel = () => controller.abort();

  const userAbortController = new AbortController();
  this.userAbortCtx = userAbortController.signal;
  this.userAbortCancel = () => userAbortController.abort();

  this.applyConfigLocked(cfg);
  if (emit !== null) {
    this.emit = emit;
  }

  this.hydrateNudgeCountersLocked();
  this.userTurnCount++;
  let shouldReviewMemory = false;
  if (cfg.memory?.nudge_interval && cfg.memory.nudge_interval > 0 && this.memStore !== null && cfg.memory.memory_enabled) {
    this.turnsSinceMemory++;
    if (this.turnsSinceMemory >= cfg.memory.nudge_interval) {
      shouldReviewMemory = true;
      this.turnsSinceMemory = 0;
    }
  }
  if (cfg.memory?.memory_enabled && cfg.memory?.user_profile_enabled && this.memStore !== null &&
      userMessageSignalsProfileCorrection(userText)) {
    shouldReviewMemory = true;
  }

  this.overflowRecoveryAttempted = false;
  const turnID = `turn_${Date.now()}_${Math.floor(Math.random() * 1000000)}`;
  this.resetEvidenceLedger(turnID);

  this.lastUserPrompt = userText;
  this.lockedGoal = userText;
  this.verifyFailures = 0;
  this.parallelForksAttempted = false;
  this.step = {
    lastVerifyFailed: false,
    failurePaths: null,
    lastVerifyOutput: "",
    lastBashCommand: "",
    lastBashFailed: false,
  };
  this.completionRounds = 0;

  if (cfg.evidence?.enabled) {
    const verifyCmd = detect_verify_command(this.workDir);
    const taskVerify = extract_task_verify_commands(userText);
    this.obligations = new obligationsRegistry(
      turnID,
      verifyCmd,
      taskVerify,
      cfg.evidence.strict_verify_reset || false,
      cfg.evidence.verifier_enabled || false
    );
  } else {
    this.obligations = null;
  }

  if (cfg.evidence?.enabled && continuity_enabled(cfg.evidence) && this.session !== null) {
    seed_continuity_reads(this.ledger!, sessionFingerprints(this.session));
  }

  if (this.session !== null && cfg.compaction?.enabled) {
    const pathEntries = this.session.get_branch(this.session.leaf_id());
    const compactionEntry = get_latest_compaction_entry(pathEntries);

    let lastUsageEntry: file_entry | null = null;
    for (let i = pathEntries.length - 1; i >= 0; i--) {
      if (pathEntries[i].type === "message" && pathEntries[i].message && pathEntries[i].message!.role === "assistant" && pathEntries[i].message!.usage) {
        lastUsageEntry = pathEntries[i];
        break;
      }
    }

    let skip = false;
    if (compactionEntry !== null && lastUsageEntry !== null) {
      const tUsage = new Date(lastUsageEntry.timestamp || "");
      const tComp = new Date(compactionEntry.timestamp || "");
      if (tUsage <= tComp) {
        skip = true;
      }
    }

    if (!skip) {
      const contextWindow = ModelContextWindow(cfg.provider, cfg.model, cfg.compaction.context_window || 0);
      const sessionMsgs = this.session.build_session_context().messages || [];
      const tokens = estimate_context_tokens(sessionMsgs).tokens;
      if (should_compact(tokens, contextWindow, cfg.compaction)) {
        await this.RunAutoCompaction(controller.signal, "threshold", false);
      }
    }
  }

  const userMsg: message = {
    role: "user",
    content: string_content(userText),
  };

  if (attachments && attachments.length > 0) {
    const parts: any[] = [];
    for (const att of attachments) {
      parts.push({
        type: "image",
        image_url: {
          url: `data:${att.MIMEType || att.mime_type};base64,${Buffer.from(att.Data || att.data).toString("base64")}`,
        }
      });
    }
    parts.push({ type: "text", text: userText });
    userMsg.content = parts as any;
  }

  this.messages.push(userMsg);
  this.persist(userMsg);

  if (cfg.evidence?.enabled && goal_lock_enabled(cfg.evidence)) {
    const lockMsg: message = {
      role: "user",
      content: string_content(goalLockNotice(userText)),
    };
    this.messages.push(lockMsg);
    this.persist(lockMsg);
  }

  if (runtimeNotice !== "") {
    const noticeMsg: message = {
      role: "user",
      content: string_content(runtimeNotice),
    };
    this.messages.push(noticeMsg);
    this.persist(noticeMsg);
  }

  try {
    await this.runLoop(controller.signal);
    if (!controller.signal.aborted && !this.userAbortFired()) {
      this.maybeSpawnBackgroundReview(shouldReviewMemory);
    }
  } finally {
    ctx.removeEventListener("abort", onAbort);
    this.busy = false;
    this.cancel = null;
    this.userAbortCtx = null;
    this.userAbortCancel = null;
  }
};

Agent.prototype.Prompt = async function (
  this: Agent,
  ctx: AbortSignal,
  cfg: runtime,
  userText: string,
  attachments: any[] | null,
  emit: ((event: any) => void) | null
): Promise<void> {
  return this.prompt(ctx, cfg, userText, attachments, "", emit);
};


