// PORT: backend/workflow/pool.go

import { Effect } from "effect";
import { type AgentResult, type PhaseSnapshot, type AgentSnapshot, cloneJSON } from "./types";
import { type queuedJob } from "./pipeline";
import { Runtime } from "./runtime";
import { event_workflow_phase } from "../core/events";
import { defaultRole } from "./spawn";

export class QuotaError extends Error {
  constructor() {
    super("provider quota exceeded");
    this.name = "QuotaError";
  }
}

export function runPool(
  this: Runtime,
  ctx: AbortSignal,
  phase: string,
  jobs: queuedJob[]
): Effect.Effect<AgentResult[], Error> {
  if (jobs.length === 0) {
    return Effect.succeed([]);
  }

  return Effect.async<AgentResult[], Error>((resume) => {
    this.registerJobs(phase, jobs);

    let pending = [...jobs];
    const results: AgentResult[] = [];
    let running = 0;
    const runningJobs = new Map<string, Promise<void>>();

    let finishedResolver: (() => void) | null = null;
    let wakeResolver: (() => void) | null = null;

    const onAbort = () => {
      this.cancelActiveAgents();
      resume(Effect.fail(new Error("interrupted")));
    };

    if (ctx.aborted) {
      onAbort();
      return;
    }
    ctx.addEventListener("abort", onAbort);

    const wakeCallback = () => {
      if (wakeResolver) {
        wakeResolver();
      }
    };
    this.wakeCallbacks.add(wakeCallback);

    const cleanup = () => {
      ctx.removeEventListener("abort", onAbort);
      this.wakeCallbacks.delete(wakeCallback);
    };

    const loop = async () => {
      try {
        while (pending.length > 0 || running > 0) {
          if (ctx.aborted) {
            this.cancelActiveAgents();
            throw new Error("interrupted");
          }

          const paused = this.paused;
          const concurrency = this.maxConcurrency;

          // Start new jobs if we have capacity and are not paused
          while (!paused && running < concurrency && pending.length > 0) {
            const job = pending.shift()!;
            const completed = this.completedResult(job.Key);
            if (completed) {
              results.push(completed);
              this.markCompletedFromCheckpoint(job, completed);
              continue;
            }

            running++;
            const jobPromise = (async () => {
              try {
                const res = await this.executeJob(ctx, phase, job.Key, job.Options);
                running--;
                runningJobs.delete(job.Key);

                if (this.takeRestart(job.Key)) {
                  this.resetQueued(job);
                  pending.unshift(job); // Add to the front
                } else if (isQuotaError(res.error || "")) {
                  this.pauseForQuota();
                  this.cancelActiveAgents();
                  throw new QuotaError();
                } else {
                  results.push(res);
                }
              } catch (e) {
                throw e;
              } finally {
                if (finishedResolver) {
                  finishedResolver();
                }
              }
            })();

            runningJobs.set(job.Key, jobPromise);
          }

          if (running === 0) {
            if (pending.length === 0) {
              break;
            }
            // If we're paused and no jobs are running but there are pending jobs,
            // we must wait for a wake signal.
            await new Promise<void>((resolve) => {
              wakeResolver = resolve;
            });
            wakeResolver = null;
            continue;
          }

          // Wait until either a job finishes or a wake signal occurs
          await Promise.race([
            new Promise<void>((resolve) => {
              finishedResolver = resolve;
            }),
            new Promise<void>((resolve) => {
              wakeResolver = resolve;
            })
          ]);
          finishedResolver = null;
          wakeResolver = null;
        }

        cleanup();
        resume(Effect.succeed(results));
      } catch (err: any) {
        cleanup();
        if (err instanceof QuotaError) {
          resume(Effect.fail(new Error("workflow paused: provider quota exceeded")));
        } else {
          resume(Effect.fail(err));
        }
      }
    };

    loop();
  });
}

export function registerJobs(this: Runtime, phase: string, jobs: queuedJob[]): void {
  this.ensurePhaseLocked(phase);
  for (const job of jobs) {
    if (this.snapshot.agents[job.Key]) {
      continue;
    }
    this.snapshot.agents[job.Key] = {
      key: job.Key,
      phase: phase,
      role: defaultRole(job.Options.role ?? ""),
      status: "queued",
      prompt: job.Options.prompt,
    };
  }
  this.recountLocked();
  this.persist();
  this.emitRun(event_workflow_phase, phase);
}

export function completedResult(this: Runtime, key: string): AgentResult | undefined {
  return this.completed[key];
}

export function markCompletedFromCheckpoint(this: Runtime, job: queuedJob, result: AgentResult): void {
  const s = this.snapshot.agents[job.Key] || {
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
  this.snapshot.agents[job.Key] = s;
  this.recountLocked();
}

export function takeRestart(this: Runtime, key: string): boolean {
  const restart = !!this.restartRequested[key];
  delete this.restartRequested[key];
  return restart;
}

export function resetQueued(this: Runtime, job: queuedJob): void {
  const s = this.snapshot.agents[job.Key] || {
    key: "",
    phase: "",
    role: "",
    status: "",
    prompt: "",
  };
  s.status = "queued";
  s.error = "";
  s.result = "";
  s.json = undefined;
  this.snapshot.agents[job.Key] = s;
  delete this.completed[job.Key];
  this.recountLocked();
  this.persist();
}

export function pauseForQuota(this: Runtime): void {
  this.paused = true;
  this.pauseReason = "provider quota exceeded";
  this.snapshot.status = "paused";
  this.snapshot.message = "provider quota exceeded";
  if (this.state) {
    this.state.status = "paused";
    this.state.pauseReason = "provider quota exceeded";
  }
  this.persist();
}

export function cancelActiveAgents(this: Runtime): void {
  const cancels = Object.values(this.active).map((c) => c.cancel);
  for (const cancel of cancels) {
    try {
      cancel();
    } catch {}
  }
}

export function ensurePhaseLocked(this: Runtime, name: string): void {
  for (const phase of this.snapshot.phases) {
    if (phase.name === name) {
      return;
    }
  }
  this.snapshot.phases.push({
    name,
    total: 0,
    queued: 0,
    running: 0,
    done: 0,
    failed: 0,
    tokens: 0,
  });
}

export function recountLocked(this: Runtime): void {
  this.snapshot.queued = 0;
  this.snapshot.running = 0;
  this.snapshot.done = 0;
  this.snapshot.failed = 0;
  this.snapshot.tokens = 0;

  const phases: Record<string, PhaseSnapshot> = {};
  for (const p of this.snapshot.phases) {
    p.total = 0;
    p.queued = 0;
    p.running = 0;
    p.done = 0;
    p.failed = 0;
    p.tokens = 0;
    phases[p.name] = p;
  }

  for (const agent of Object.values(this.snapshot.agents)) {
    const phase = phases[agent.phase];
    if (!phase) {
      continue;
    }
    phase.total++;
    phase.tokens += agent.tokens ?? 0;
    this.snapshot.tokens += agent.tokens ?? 0;
    switch (agent.status) {
      case "queued":
        phase.queued++;
        this.snapshot.queued++;
        break;
      case "running":
        phase.running++;
        this.snapshot.running++;
        break;
      case "done":
        phase.done++;
        this.snapshot.done++;
        break;
      case "failed":
      case "stopped":
        phase.failed++;
        this.snapshot.failed++;
        break;
    }
  }
}

export function isQuotaError(text: string): boolean {
  if (text === "") {
    return false;
  }
  const needles = ["rate limit", "rate_limit", "quota", "usage limit", "too many requests", "status 429", "http 429"];
  const lowerText = text.toLowerCase();
  for (const needle of needles) {
    if (lowerText.includes(needle)) {
      return true;
    }
  }
  return false;
}

// Attach prototype methods
const proto = Runtime.prototype as any;
proto.runPool = runPool;
proto.registerJobs = registerJobs;
proto.completedResult = completedResult;
proto.markCompletedFromCheckpoint = markCompletedFromCheckpoint;
proto.takeRestart = takeRestart;
proto.resetQueued = resetQueued;
proto.pauseForQuota = pauseForQuota;
proto.cancelActiveAgents = cancelActiveAgents;
proto.ensurePhaseLocked = ensurePhaseLocked;
proto.recountLocked = recountLocked;

/*
PORT STATUS
source path: backend/workflow/pool.go
source lines: 241
draft lines: 290
confidence: high
status: phase_b_compile
*/
