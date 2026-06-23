// PORT: backend/workflow/pipeline.go

import { Effect } from "effect";
import { type PipelineResult, type StageResult, type AgentResult, type AgentOptions, cloneJSON } from "./types";
import { Runtime } from "./runtime";
import { event_workflow_phase } from "../core/events";

export interface queuedJob {
  Key: string;
  Phase: string;
  Options: AgentOptions;
}

export function runPipeline(
  this: Runtime,
  ctx: AbortSignal,
  input: any,
  stages: ((ctx: any) => Promise<any> | any)[]
): Effect.Effect<PipelineResult, Error> {
  const self = this;
  return Effect.gen(function* () {
    const out: PipelineResult = {
      input,
      stages: [],
      results: {},
    };

    let previous: AgentResult[] = [];

    for (let index = 0; index < stages.length; index++) {
      if (ctx.aborted) {
        return yield* Effect.fail(new Error("interrupted"));
      }
      const stage = stages[index];
      const phase = self.phaseName(index);
      self.setPhase(phase, index);

      const stageCtx = {
        input: input,
        previousResults: previous,
        results: out.results,
        stageIndex: index,
        phase: phase,
      };

      let value: any;
      try {
        const res = stage(cloneJSON(stageCtx));
        if (res && typeof res.then === "function") {
          value = yield* Effect.promise(() => res);
        } else {
          value = res;
        }
      } catch (err: any) {
        return yield* Effect.fail(new Error(`pipeline stage ${phase}: ${err.message || String(err)}`));
      }

      let jobs: queuedJob[];
      try {
        jobs = normalizeJobs(value, phase);
      } catch (err: any) {
        return yield* Effect.fail(new Error(`pipeline stage ${phase}: ${err.message || String(err)}`));
      }

      const results = yield* self.runPool(ctx, phase, jobs);
      previous = results;
      out.stages.push({ name: phase, results });
      for (const res of results) {
        if (res.key) {
          out.results[res.key] = res;
        }
      }
    }

    return out;
  });
}

export function normalizeJobs(value: any, phase: string): queuedJob[] {
  if (value === undefined || value === null) {
    return [];
  }
  if (!Array.isArray(value)) {
    throw new Error("stage must return an array of subjobs");
  }

  const jobs: queuedJob[] = [];
  for (let index = 0; index < value.length; index++) {
    const item = value[index];
    if (typeof item !== "object" || item === null) {
      throw new Error(`subjob ${index} is not an object`);
    }

    let optionsMap = { ...item };
    if (item.options && typeof item.options === "object") {
      optionsMap = { ...item.options };
      if (optionsMap.key === undefined) {
        optionsMap.key = item.key;
      }
    }

    const opts: AgentOptions = {
      key: optionsMap.key,
      role: optionsMap.role ?? "",
      prompt: optionsMap.prompt ?? "",
      systemPrompt: optionsMap.systemPrompt,
      tools: optionsMap.tools,
      model: optionsMap.model,
      responseSchema: optionsMap.responseSchema,
      maxTurns: optionsMap.maxTurns,
      readonly: optionsMap.readonly,
    };

    if (opts.role === "") {
      opts.role = phase;
    }
    if (!opts.key) {
      opts.key = `${phase}:${index + 1}`;
    }
    if (opts.prompt === "") {
      throw new Error(`subjob ${opts.key} has no prompt`);
    }

    jobs.push({ Key: opts.key, Phase: phase, Options: opts });
  }

  return jobs;
}

export function phaseName(this: Runtime, index: number): string {
  if (index >= 0 && this.meta.phases && index < this.meta.phases.length && this.meta.phases[index] !== "") {
    return this.meta.phases[index];
  }
  return `stage-${index + 1}`;
}

export function setPhase(this: Runtime, phase: string, index: number): void {
  this.snapshot.phase = phase;
  if (this.state) {
    this.state.phase = phase;
    this.state.stageIndex = index;
  }
  this.ensurePhaseLocked(phase);
  this.persist();
  this.emitRun(event_workflow_phase, phase);
}

// Attach prototype methods
const proto = Runtime.prototype as any;
proto.runPipeline = runPipeline;
proto.phaseName = phaseName;
proto.setPhase = setPhase;

/*
PORT STATUS
source path: backend/workflow/pipeline.go
source lines: 117
source lines: 111
confidence: high
status: phase_b_compile
*/
