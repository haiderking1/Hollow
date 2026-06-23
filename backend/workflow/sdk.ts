// PORT: backend/workflow/sdk.go

import { Effect } from "effect";
import { type AgentOptions, type AgentResult } from "./types";
import { Runtime } from "./runtime";
import { event_workflow_phase } from "../core/events";
import { defaultRole } from "./spawn";
import { isQuotaError } from "./pool";

export function sdkObject(this: Runtime, ctx: AbortSignal): any {
  const self = this;
  return {
    today(): string {
      const now = new Date();
      const year = now.getFullYear();
      const month = String(now.getMonth() + 1).padStart(2, "0");
      const day = String(now.getDate()).padStart(2, "0");
      return `${year}-${month}-${day}`;
    },
    log(level: string, message: string): void {
      self.emitRun(event_workflow_phase, `${level}: ${message}`);
    },
    emit(message: string): void {
      self.emitRun(event_workflow_phase, message);
    },
    runBash(command: string): Promise<any> {
      return Effect.runPromise(self.runBash(ctx, command));
    },
    async fetchJSON(command: string): Promise<any> {
      const result = await Effect.runPromise(self.runBash(ctx, command));
      if (result.exitCode !== 0) {
        throw new Error(`command exited ${result.exitCode}: ${result.stderr}`);
      }
      return JSON.parse(result.stdout);
    },
    async spawnAgent(opts: AgentOptions): Promise<any> {
      const optsCopy = { ...opts };
      if (!optsCopy.key) {
        // Fallback key: role + microtime + random
        optsCopy.key = `${defaultRole(optsCopy.role ?? "")}:${Date.now() * 1000 + Math.floor(Math.random() * 1000)}`;
      }
      await self.acquireDirect(ctx);
      try {
        const result = await self.executeJob(ctx, self.currentPhase(), optsCopy.key, optsCopy);
        if (isQuotaError(result.error || "")) {
          self.pauseForQuota();
          self.cancelActiveAgents();
          self.cancelRun();
          throw new Error("workflow paused: provider quota exceeded");
        }
        return result;
      } finally {
        self.releaseDirect();
      }
    },
    async pipeline(input: any, ...stages: ((ctx: any) => Promise<any> | any)[]): Promise<any> {
      if (stages.length === 0) {
        throw new Error("pipeline requires input and at least one stage");
      }
      return Effect.runPromise(self.runPipeline(ctx, input, stages));
    },
  };
}

export function cancelRun(this: Runtime): void {
  if (this.cancel) {
    this.cancel();
  }
}

export function currentPhase(this: Runtime): string {
  if (this.snapshot.phase !== "") {
    return this.snapshot.phase;
  }
  return "agent";
}

export async function acquireDirect(this: Runtime, ctx: AbortSignal): Promise<void> {
  if (this.directSem) {
    await this.directSem.acquire(ctx);
  }
}

export function releaseDirect(this: Runtime): void {
  if (this.directSem) {
    this.directSem.release();
  }
}

// Attach prototype methods
const proto = Runtime.prototype as any;
proto.sdkObject = sdkObject;
proto.cancelRun = cancelRun;
proto.currentPhase = currentPhase;
proto.acquireDirect = acquireDirect;
proto.releaseDirect = releaseDirect;

/*
PORT STATUS
source path: backend/workflow/sdk.go
source lines: 146
draft lines: 98
confidence: high
status: phase_b_compile
*/
