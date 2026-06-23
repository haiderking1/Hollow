// PORT: backend/workflow/runtime.go

import { Effect } from "effect";
import vm from "node:vm";
import fs from "node:fs";
import path from "node:path";
import {
  type Meta,
  type Snapshot,
  type BashResult,
  type AgentResult,
  type RunOptions,
  type RunResult,
  cloneJSON,
  DefaultMaxConcurrency,
  DefaultMaxTotalAgents,
} from "./types";
import { type runtime } from "../config/config";
import { type State, LoadState, SaveState } from "./state";
import {
  type event,
  type workflow_run_event,
  event_workflow_start,
  event_workflow_end,
  event_workflow_paused,
  event_workflow_phase,
} from "../core/events";

const exportMetaPattern = /\bexport\s+const\s+meta\s*=/;
const exportRunPattern = /\bexport\s+(async\s+)?function\s+run\s*\(/;

export class Semaphore {
  private queue: (() => void)[] = [];
  constructor(public max: number) {}

  async acquire(signal?: AbortSignal): Promise<void> {
    if (this.max > 0) {
      this.max--;
      return;
    }
    if (signal?.aborted) {
      throw new Error("interrupted");
    }
    return new Promise<void>((resolve, reject) => {
      const onAbort = () => {
        const idx = this.queue.indexOf(resolve);
        if (idx >= 0) {
          this.queue.splice(idx, 1);
        }
        reject(new Error("interrupted"));
      };
      if (signal) {
        signal.addEventListener("abort", onAbort);
      }
      this.queue.push(() => {
        if (signal) {
          signal.removeEventListener("abort", onAbort);
        }
        resolve();
      });
    });
  }

  release(): void {
    const next = this.queue.shift();
    if (next) {
      next();
    } else {
      this.max++;
    }
  }
}

export class Runtime {
  cfg!: runtime;
  workDir!: string;
  snapshot!: Snapshot;
  meta!: Meta;
  state: State | null = null;
  emit: ((event: any) => void) | null = null;
  cancel: (() => void) | null = null;
  paused = false;
  pauseReason = "";
  wakeCallbacks = new Set<() => void>();
  active: Record<string, { cancel: () => void }> = {};
  restartRequested: Record<string, boolean> = {};
  completed: Record<string, AgentResult> = {};
  totalAgents = 0;
  maxConcurrency = 16;
  maxTotalAgents = 1000;
  directSem: Semaphore | null = null;

  agentRunner!: (
    ctx: AbortSignal,
    phase: string,
    key: string,
    opts: any,
    emit: (event: any) => void
  ) => Promise<AgentResult>;

  constructor() {
    this.agentRunner = this.defaultAgentRunner;
  }

  // Stub methods to be implemented by other files via prototype attachment
  runBash!: (ctx: AbortSignal, command: string) => Effect.Effect<BashResult, Error>;
  runPipeline!: (
    ctx: AbortSignal,
    input: any,
    stages: ((ctx: any) => Promise<any> | any)[]
  ) => Effect.Effect<any, Error>;
  runPool!: (ctx: AbortSignal, phase: string, jobs: any[]) => Effect.Effect<AgentResult[], Error>;
  executeJob!: (ctx: AbortSignal, phase: string, key: string, opts: any) => Promise<AgentResult>;
  registerJobs!: (phase: string, jobs: any[]) => void;
  completedResult!: (key: string) => AgentResult | undefined;
  markCompletedFromCheckpoint!: (job: any, result: AgentResult) => void;
  takeRestart!: (key: string) => boolean;
  resetQueued!: (job: any) => void;
  pauseForQuota!: () => void;
  cancelActiveAgents!: () => void;
  ensurePhaseLocked!: (name: string) => void;
  recountLocked!: () => void;
  currentPhase!: () => string;
  acquireDirect!: (ctx: AbortSignal) => Promise<void>;
  releaseDirect!: () => void;

  phaseName!: (index: number) => string;
  setPhase!: (phase: string, index: number) => void;
  reserveAgent!: () => boolean;
  finishAgent!: (key: string, result: AgentResult) => void;
  startAgent!: (key: string, phase: string, opts: any, cancel: () => void) => void;
  runAgentAttempt!: (ctx: AbortSignal, phase: string, key: string, opts: any) => Promise<AgentResult>;
  handleWorkerEvent!: (phase: string, key: string, role: string, event: any) => void;
  cancelRun!: () => void;
  defaultAgentRunner!: (
    ctx: AbortSignal,
    phase: string,
    key: string,
    opts: any,
    emit: (event: any) => void
  ) => Promise<AgentResult>;
  sdkObject!: (ctx: AbortSignal) => any;

  signal(): void {
    for (const cb of this.wakeCallbacks) {
      try {
        cb();
      } catch {}
    }
  }

  isQuotaPaused(): boolean {
    return this.pauseReason === "provider quota exceeded";
  }

  persist(): void {
    if (!this.state) {
      return;
    }
    this.state.meta = this.meta;
    this.state.status = this.snapshot.status;
    this.state.phase = this.snapshot.phase;
    this.state.completed = cloneJSON(this.completed);
    this.state.agents = cloneJSON(this.snapshot.agents);
    try {
      SaveState(cloneJSON(this.state));
    } catch {}
  }

  emitRun(kind: string, message: string): void {
    const emit = this.emit;
    if (!emit) {
      return;
    }
    const s = cloneJSON(this.snapshot);
    const phases = [...(this.meta.phases ?? [])];
    const elapsedMs = Date.now() - s.startedAt.getTime();
    emit({
      kind,
      data: {
        id: s.id,
        name: s.name,
        description: s.description,
        script_path: s.scriptPath,
        status: s.status,
        phase: s.phase,
        phases: phases,
        queued: s.queued,
        running: s.running,
        done: s.done,
        failed: s.failed,
        tokens: s.tokens,
        started_at: s.startedAt,
        elapsed: elapsedMs * 1000000, // Y in nanoseconds to match Go
        message: message,
      } as workflow_run_event,
    });
  }

  Run(
    ctx: AbortSignal,
    scriptPath: string,
    opts: RunOptions,
    emit: (event: any) => void
  ): Effect.Effect<RunResult, Error> {
    const self = this;
    return Effect.async<RunResult, Error>((resume) => {
      let completed = false;
      const onAbort = () => {
        if (completed) return;
        completed = true;
        self.cancelRun();
        resume(self.finishError(new Error("interrupted")));
      };

      if (ctx.aborted) {
        onAbort();
        return;
      }
      ctx.addEventListener("abort", onAbort);

      const runBody = async () => {
        try {
          const abs = path.resolve(scriptPath);
          const { script } = compileWorkflow(abs);

          self.emit = emit;
          self.cancel = () => {
            if (completed) return;
            completed = true;
            ctx.removeEventListener("abort", onAbort);
            self.cancelActiveAgents();
            self.signal();
          };

          self.snapshot = {
            id: opts.ID,
            name: "",
            description: "",
            scriptPath: abs,
            status: "loading",
            phase: "",
            phases: [],
            agents: {},
            queued: 0,
            running: 0,
            done: 0,
            failed: 0,
            tokens: 0,
            startedAt: new Date(),
          };

          if (self.snapshot.id === "") {
            self.snapshot.id = path.basename(path.dirname(abs));
          }
          self.paused = false;
          self.pauseReason = "";

          const sdk = self.sdkObject(ctx);
          const args = parseArgs(opts.Args);

          const sandbox = {
            globalThis: {},
            args: args === null ? undefined : args,
            sdk: sdk,
          };
          (sandbox as any).globalThis = sandbox;

          const context = vm.createContext(sandbox);
          script.runInContext(context);

          const rawMeta = (context as any).__hollow_meta;
          const runFn = (context as any).__hollow_run;

          if (!rawMeta) {
            throw new Error("workflow must export const meta");
          }
          if (typeof runFn !== "function") {
            throw new Error("workflow must export async function run(sdk)");
          }

          const meta = cloneJSON(coalesceMetaExport(rawMeta)) as Meta;
          if (!meta.name || meta.name.trim() === "") {
            throw new Error("workflow meta requires non-empty name and description");
          }
          self.configure(meta);

          let state: State = {
            version: 1,
            id: self.snapshot.id,
            scriptPath: abs,
            args: opts.Args,
            meta: meta,
            status: "running",
            completed: {},
            agents: {},
            startedAt: new Date(),
            updatedAt: new Date(),
          };

          if (!opts.Force) {
            try {
              const prior = LoadState(abs);
              if (prior.id === state.id) {
                state = prior;
                state.status = "running";
                state.pauseReason = "";
                state.args = opts.Args;
                if (!state.completed) {
                  state.completed = {};
                }
              }
            } catch {}
          }

          self.state = state;
          self.completed = cloneJSON(state.completed);
          self.snapshot.id = state.id;
          self.snapshot.name = meta.name;
          self.snapshot.description = meta.description;
          self.snapshot.status = "running";
          self.snapshot.startedAt = state.startedAt;
          self.snapshot.agents = cloneJSON(state.agents ?? {});
          self.totalAgents = Object.keys(state.completed).length;
          self.recountLocked();

          if (!self.snapshot.startedAt || isNaN(self.snapshot.startedAt.getTime())) {
            self.snapshot.startedAt = new Date();
            state.startedAt = self.snapshot.startedAt;
          }

          self.persist();
          self.emitRun(event_workflow_start, "");

          let resultVal: any;
          try {
            resultVal = await runFn(sdk);
          } catch (runErr: any) {
            if (completed) return;
            completed = true;
            ctx.removeEventListener("abort", onAbort);
            resume(self.finishError(runErr));
            return;
          }

          if (completed) return;
          completed = true;
          ctx.removeEventListener("abort", onAbort);

          self.snapshot.status = "done";
          self.snapshot.endedAt = new Date();
          if (self.state) {
            self.state.status = "done";
            self.state.pauseReason = "";
          }
          self.persist();

          let summary = "workflow complete";
          if (resultVal !== undefined && resultVal !== null) {
            try {
              let dataStr = JSON.stringify(resultVal);
              if (dataStr && dataStr !== "null") {
                if (dataStr.length > 4000) {
                  dataStr = dataStr.slice(0, 4000) + "...";
                }
                summary = dataStr;
              }
            } catch {}
          }

          self.emitRun(event_workflow_end, summary);
          resume(Effect.succeed({ id: state.id, meta: meta, value: resultVal, status: "done" }));
        } catch (err: any) {
          if (completed) return;
          completed = true;
          ctx.removeEventListener("abort", onAbort);
          resume(self.finishError(err));
        }
      };

      runBody();
    });
  }

  finishError(err: Error): Effect.Effect<RunResult, Error> {
    if (err.message === "workflow paused: provider quota exceeded" || this.isQuotaPaused()) {
      this.snapshot.status = "paused";
      this.snapshot.message = "provider quota exceeded";
      if (this.state) {
        this.state.status = "paused";
        this.state.pauseReason = "provider quota exceeded";
      }
      this.persist();
      this.emitRun(event_workflow_paused, "provider quota exceeded");
      return Effect.fail(new Error("workflow paused: provider quota exceeded"));
    }
    const status = this.snapshot.status;
    const statusMessage = this.snapshot.message;
    if (status === "paused") {
      this.persist();
      this.emitRun(event_workflow_paused, statusMessage ?? "");
      return Effect.fail(err);
    }
    if (err.message === "interrupted" || status === "cancelled") {
      this.snapshot.status = "cancelled";
      this.snapshot.endedAt = new Date();
      if (this.state) {
        this.state.status = "cancelled";
      }
      this.persist();
      this.emitRun(event_workflow_end, "workflow cancelled");
      return Effect.fail(new Error("workflow cancelled"));
    }
    this.snapshot.status = "failed";
    this.snapshot.message = err.message || String(err);
    this.snapshot.endedAt = new Date();
    if (this.state) {
      this.state.status = "failed";
      this.state.pauseReason = err.message || String(err);
    }
    this.persist();
    this.emitRun(event_workflow_end, err.message || String(err));
    return Effect.fail(err);
  }

  configure(meta: Meta): void {
    this.meta = meta;
    this.maxConcurrency = DefaultMaxConcurrency;
    const envConcurrency = process.env.HOLLOW_WORKFLOW_MAX_CONCURRENCY;
    if (envConcurrency) {
      const val = parseInt(envConcurrency, 10);
      if (!isNaN(val) && val > 0) {
        this.maxConcurrency = val;
      }
    }
    if (meta.maxConcurrency && meta.maxConcurrency > 0 && meta.maxConcurrency < this.maxConcurrency) {
      this.maxConcurrency = meta.maxConcurrency;
    }

    this.maxTotalAgents = DefaultMaxTotalAgents;
    const envTotalAgents = process.env.HOLLOW_WORKFLOW_MAX_TOTAL_AGENTS;
    if (envTotalAgents) {
      const val = parseInt(envTotalAgents, 10);
      if (!isNaN(val) && val >= 0) {
        this.maxTotalAgents = val;
      }
    }
    if (meta.maxTotalAgents && meta.maxTotalAgents > 0) {
      this.maxTotalAgents = meta.maxTotalAgents;
    }

    this.directSem = new Semaphore(this.maxConcurrency);
    this.snapshot.phases = [];
    if (meta.phases) {
      for (const phase of meta.phases) {
        if (phase) {
          this.snapshot.phases.push({
            name: phase,
            total: 0,
            queued: 0,
            running: 0,
            done: 0,
            failed: 0,
            tokens: 0,
          });
        }
      }
    }
  }

  Snapshot(): Snapshot {
    return cloneJSON(this.snapshot);
  }

  Pause(): void {
    if (this.snapshot.status === "running") {
      this.paused = true;
      this.pauseReason = "user";
      this.snapshot.status = "paused";
      this.snapshot.message = "paused by user";
      if (this.state) {
        this.state.status = "paused";
        this.state.pauseReason = "user";
      }
    }
    this.persist();
    this.emitRun(event_workflow_paused, "paused by user");
    this.signal();
  }

  Resume(): void {
    this.paused = false;
    this.pauseReason = "";
    if (this.snapshot.status === "paused") {
      this.snapshot.status = "running";
      this.snapshot.message = "";
    }
    if (this.state) {
      this.state.status = "running";
      this.state.pauseReason = "";
    }
    this.persist();
    this.emitRun(event_workflow_phase, "resumed");
    this.signal();
  }

  Cancel(): void {
    this.snapshot.status = "cancelled";
    if (this.cancel) {
      this.cancel();
    }
    this.signal();
  }

  CheckpointAndStop(reason: string): void {
    const trimmedReason = reason.trim() === "" ? "user" : reason;
    this.paused = true;
    this.pauseReason = trimmedReason;
    this.snapshot.status = "paused";
    this.snapshot.message = trimmedReason;
    if (this.state) {
      this.state.status = "paused";
      this.state.pauseReason = trimmedReason;
    }
    this.persist();
    if (this.cancel) {
      this.cancel();
    }
    this.signal();
  }

  StopAgent(key: string): boolean {
    const control = this.active[key];
    if (!control) {
      return false;
    }
    control.cancel();
    return true;
  }

  RestartAgent(key: string): boolean {
    const control = this.active[key];
    if (control) {
      this.restartRequested[key] = true;
    }
    if (!control) {
      return false;
    }
    control.cancel();
    return true;
  }
}

export function NewRuntime(cfg: runtime, workDir?: string): Runtime {
  const r = new Runtime();
  r.cfg = cfg;
  r.workDir = workDir || process.cwd();
  return r;
}

export function compileWorkflow(filePath: string): { source: string; script: vm.Script } {
  const data = fs.readFileSync(filePath, "utf8");
  if (!exportMetaPattern.test(data)) {
    throw new Error("workflow must export const meta");
  }
  if (!exportRunPattern.test(data)) {
    throw new Error("workflow must export async function run(sdk)");
  }
  let transformed = data.replace(exportMetaPattern, "const meta =");
  transformed = transformed.replace(exportRunPattern, (match) => {
    return match.replace(/^\bexport\s+/, "");
  });
  transformed += "\n;globalThis.__hollow_meta = meta; globalThis.__hollow_run = run;\n";
  const script = new vm.Script(transformed, { filename: filePath });
  return { source: data, script };
}

export function Inspect(scriptPath: string): Effect.Effect<Meta, Error> {
  return Effect.try({
    try: () => {
      const absPath = path.resolve(scriptPath);
      const { script } = compileWorkflow(absPath);

      const sandbox = {
        globalThis: {},
        args: undefined,
        sdk: {},
      };
      (sandbox as any).globalThis = sandbox;

      const context = vm.createContext(sandbox);
      script.runInContext(context, { timeout: 2000 });

      const rawMeta = (context as any).__hollow_meta;
      if (!rawMeta) {
        throw new Error("workflow must export const meta");
      }

      const coalesced = coalesceMetaExport(rawMeta);
      const meta = cloneJSON(coalesced) as Meta;
      if (!meta.name || meta.name.trim() === "") {
        throw new Error("invalid workflow meta: name is required");
      }
      return meta;
    },
    catch: (cause) => (cause instanceof Error ? cause : new Error(String(cause))),
  });
}

export function coalesceMetaExport(raw: any): any {
  if (typeof raw !== "object" || raw === null) {
    return raw;
  }
  const m = { ...raw };
  if (!("phases" in m)) {
    return m;
  }
  const phases = m.phases;
  if (Array.isArray(phases)) {
    const names: string[] = [];
    for (let i = 0; i < phases.length; i++) {
      const item = phases[i];
      if (typeof item !== "string" || item.trim() === "") {
        throw new Error(`phases[${i}] must be a non-empty phase name string`);
      }
      names.push(item.trim());
    }
    if (names.length === 0) {
      delete m.phases;
    } else {
      m.phases = names;
    }
  } else if (typeof phases === "string") {
    if (phases.trim() === "") {
      delete m.phases;
    } else {
      m.phases = [phases.trim()];
    }
  } else if (typeof phases === "number") {
    delete m.phases;
  } else {
    throw new Error(`phases must be an array of phase name strings (e.g. ["read", "write"]), not ${typeof phases}`);
  }
  return m;
}

export function parseArgs(raw: string): any {
  const trimmed = raw.trim();
  if (trimmed === "") {
    return null;
  }
  if (trimmed.startsWith("{") || trimmed.startsWith("[")) {
    try {
      return JSON.parse(trimmed);
    } catch {}
  }
  return trimmed;
}

/*
PORT STATUS
source path: backend/workflow/runtime.go
source lines: 607
draft lines: 504
confidence: high
status: phase_b_compile
*/
