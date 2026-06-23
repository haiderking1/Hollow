// PORT STATUS: active
// source path: runtime/agent_runtime.ts
// confidence: high
// status: phase_b_compile

import { Effect, Context, PubSub } from "effect";
// Single registration barrel — attaches all Agent prototype methods and
// re-exports { Agent, New } so this is the only agent import needed.
import { Agent, New } from "../backend/agent/register";
import { type runtime, load_runtime } from "../backend/config/config";
import { apply_provider_model } from "../backend/config/provider";
import { EnsureBootstrapped } from "../backend/skills/bootstrap";
import { open_session, list_for_cwd, list_all } from "../backend/session/list";
import { continue_recent, start_new, type manager } from "../backend/session/manager";
import { delete_session } from "../backend/session/delete";
import { type info } from "../backend/session/types";
import { new_client_for_runtime } from "../backend/opencode/runtime_client";
import { content_string } from "../backend/opencode/types";

/** Best-effort reason string from a config/secrets error (they use `reason`, not `message`). */
const errReason = (e: unknown): string => {
  if (!e) return "";
  if (typeof e === "string") return e;
  if (typeof e === "object") {
    const o = e as Record<string, unknown>;
    return String(o.reason ?? o.message ?? "");
  }
  return String(e);
};

/**
 * True when a boot failure is purely a missing-provider-credential problem
 * (no API key, no codex auth). These degrade gracefully — the UI opens and
 * shows "connect a provider" — rather than bricking the app.
 */
export const isProviderKeyError = (e: unknown): boolean => {
  const s = `${errReason(e)} ${errReason((e as { cause?: unknown })?.cause)}`.toLowerCase();
  return /api key|credentials|not connected|keyring|\bauth\b|token/.test(s);
};

export const NOT_CONNECTED = "No provider connected. Add an API key in Settings, then retry.";

export class AgentRuntimeImpl {
  config!: runtime;
  workDir!: string;
  agent!: Agent;
  pubsub!: PubSub.PubSub<any>;
  /** False until a provider key is loaded and the agent is constructed. */
  available = false;
  loopState: {
    active: boolean;
    task: string;
    iteration: number;
    maxIterations: number;
    completionPromise: string;
    aborted: boolean;
  } | null = null;

  boot(workDir?: string): Effect.Effect<void, Error> {
    const self = this;
    return Effect.gen(function* () {
      self.pubsub = yield* PubSub.unbounded<any>();
      self.available = false;
      self.workDir = workDir && workDir !== "" ? workDir : process.cwd();

      // load_runtime hard-fails when no provider key is configured. Treat that
      // as a graceful "not connected" state — boot succeeds with available=false
      // so the UI can open and let the user connect a provider.
      const cfgResult = yield* Effect.either(load_runtime());
      if (cfgResult._tag === "Left") {
        if (isProviderKeyError(cfgResult.left)) {
          return;
        }
        return yield* Effect.fail(new Error(errReason(cfgResult.left) || "boot failed"));
      }

      yield* self.buildAgent(cfgResult.right);
    }).pipe(
      Effect.mapError((err) => {
        if (err instanceof Error) return err;
        return new Error(errReason(err) || "boot failed");
      })
    );
  }

  /**
   * Shared agent-build step used by boot() and reconnect(): bootstrap skills,
   * open the most recent session for the workdir, construct the agent, and mark
   * the runtime available. Reuses continue_recent (which scans the dir for an
   * existing session file) rather than the in-memory session_file() path — that
   * path is set by new_session before the file is actually written, so reading
   * it back on reconnect would ENOENT.
   */
  private buildAgent(cfg: runtime): Effect.Effect<void, Error> {
    const self = this;
    return Effect.gen(function* () {
      self.config = cfg;
      yield* EnsureBootstrapped();
      const sm = yield* continue_recent(self.workDir);
      // Tear down the previous agent (MCP manager, etc.) before replacing it.
      if (self.agent?.Close) {
        try {
          self.agent.Close();
        } catch {
          // ignore cleanup errors on reconnect
        }
      }
      self.agent = New(self.config, self.workDir, sm);
      self.agent.emit = (event: any) => {
        // Synchronous publish: prompt completion must mean all emitted events
        // are already delivered to subscribers before emit() returns.
        Effect.runSync(PubSub.publish(self.pubsub, event));
      };
      self.available = true;
    });
  }

  /**
   * Re-attempt a full boot after a provider key has been added/switched (called
   * by the bridge when the user connects a provider from Settings). Promotes
   * a degraded runtime to live in-place — no app restart. If the active provider
   * still has no key, stays degraded (best-effort, never hard-fails on a key
   * error so the UI keeps working).
   */
  reconnect(): Effect.Effect<void, Error> {
    const self = this;
    return Effect.gen(function* () {
      // pubsub/workDir are already set from the initial (degraded) boot.
      const cfgResult = yield* Effect.either(load_runtime());
      if (cfgResult._tag === "Left") {
        if (isProviderKeyError(cfgResult.left)) {
          self.available = false;
          return;
        }
        return yield* Effect.fail(new Error(errReason(cfgResult.left) || "reconnect failed"));
      }
      yield* self.buildAgent(cfgResult.right);
    }).pipe(
      Effect.mapError((err) => (err instanceof Error ? err : new Error(errReason(err) || "reconnect failed"))),
    );
  }

  /**
   * Minimal boot for the degraded/never-connected case: just the event bus and
   * a workdir, with available=false. Never fails — used so Electron IPC can
   * always be registered even when a real (non-key) boot error happens.
   */
  bootDegraded(workDir?: string): Effect.Effect<void, never> {
    const self = this;
    return Effect.gen(function* () {
      self.pubsub = yield* PubSub.unbounded<any>();
      self.available = false;
      self.workDir = workDir && workDir !== "" ? workDir : process.cwd();
    }).pipe(Effect.catchAll(() => Effect.void));
  }

  listSessions(cwd?: string): Effect.Effect<info[], Error> {
    return cwd ? list_for_cwd(cwd) : list_all();
  }

  /**
   * Open a session by id/path and return its manager. Reading history is a
   * disk operation, so this works without a provider key — the session is
   * wired into the agent only when one is booted. Clicking a session in the
   * UI must load its transcript even in degraded (no-key) mode.
   */
  openSession(id: string): Effect.Effect<manager, Error> {
    const self = this;
    return Effect.gen(function* () {
      const infos = yield* list_all();
      let targetPath = "";
      for (const info of infos) {
        if (info.id === id || info.path === id) {
          targetPath = info.path;
          break;
        }
      }
      if (targetPath === "") {
        targetPath = id;
      }
      const sm = yield* open_session(targetPath);
      // Keep the runtime workDir in sync with the active session so a reconnect
      // rebuilds the agent against this session's project, not the boot cwd.
      if (sm.cwd() !== "") self.workDir = sm.cwd();
      if (self.available) self.agent.LoadSession(sm);
      return sm;
    });
  }

  newSession(cwd?: string): Effect.Effect<manager, Error> {
    const self = this;
    return Effect.gen(function* () {
      const targetCwd = cwd || self.workDir;
      // Track the chosen cwd at the runtime layer too — otherwise a later
      // reconnect() rebuilds the agent from the stale boot workDir (process.cwd(),
      // i.e. the app's own dir) and `pwd` ends up pointing at ~/hollow instead
      // of the user's project.
      self.workDir = targetCwd;
      const sm = yield* start_new(targetCwd);
      if (self.available) self.agent.LoadSession(sm);
      return sm;
    });
  }

  deleteSession(id: string): Effect.Effect<void, Error> {
    const self = this;
    return Effect.gen(function* () {
      const infos = yield* list_all();
      let targetPath = "";
      for (const info of infos) {
        if (info.id === id || info.path === id) {
          targetPath = info.path;
          break;
        }
      }
      if (targetPath === "") {
        targetPath = id;
      }
      if (self.agent?.session && self.agent.session.session_file() === targetPath) {
        if (self.available) {
          self.agent.LoadSession(null);
        }
      }
      yield* delete_session(targetPath);
    });
  }

  prompt(text: string, attachments?: readonly any[]): Effect.Effect<void, Error> {
    const self = this;
    const cleanText = text.trim();

    // Check if this is a loop cancellation command
    if (cleanText === "/loop-cancel" || cleanText === "/cancel-loop") {
      return Effect.sync(() => {
        if (!self.loopState || !self.loopState.active) {
          if (self.agent?.emit) {
            self.agent.emit({ kind: "system", data: "No active loop to cancel." });
          }
          return;
        }
        self.loopState.aborted = true;
        self.agent.Abort();
        if (self.agent?.emit) {
          self.agent.emit({ kind: "system", data: "Loop cancelled by user." });
        }
      });
    }

    if (!self.available) return Effect.fail(new Error(NOT_CONNECTED));

    // Check if this is a loop start command
    if (cleanText.startsWith("/loop") && cleanText !== "/loop-cancel" && cleanText !== "/cancel-loop") {
      if (self.loopState && self.loopState.active) {
        return Effect.fail(new Error("A loop is already active. Cancel it first with /loop-cancel."));
      }

      const taskPart = cleanText.slice(5).trim();
      let maxIter = 0;
      const maxMatch = taskPart.match(/(?:^|\s)--max\s+(\d+)\s*$/);
      let task = taskPart;
      if (maxMatch) {
        maxIter = parseInt(maxMatch[1], 10);
        task = task.replace(/(?:^|\s)--max\s+(\d+)\s*$/, "").trim();
      }

      if (task === "") {
        return Effect.fail(new Error("Usage: /loop <task> [--max N]"));
      }

      let promise = "DONE";
      const promiseMatch = task.match(/<promise>([^<]+)<\/promise>/);
      if (promiseMatch) {
        const custom = promiseMatch[1].trim();
        if (custom !== "") {
          promise = custom;
        }
      }

      self.loopState = {
        active: true,
        task,
        iteration: 0,
        maxIterations: maxIter,
        completionPromise: promise,
        aborted: false,
      };

      return Effect.async<void, Error>((resume) => {
        const controller = new AbortController();
        const onAbort = () => {
          if (self.loopState) self.loopState.aborted = true;
          controller.abort();
        };
        // Propagate external signal abort to our loop cancellation
        controller.signal.addEventListener("abort", () => {
          if (self.loopState) self.loopState.aborted = true;
        });

        const runIteration = async () => {
          if (!self.loopState || self.loopState.aborted || controller.signal.aborted || self.agent.userAbortFired()) {
            if (self.agent.emit) {
              self.agent.emit({
                kind: "loop_status",
                data: { active: false, iteration: 0, maxIterations: 0, task: "" }
              });
            }
            self.loopState = null;
            return resume(Effect.void);
          }

          self.loopState.iteration++;
          const iter = self.loopState.iteration;

          if (self.agent.emit) {
            self.agent.emit({
              kind: "loop_status",
              data: {
                active: true,
                iteration: iter,
                maxIterations: maxIter,
                task: task,
              }
            });
          }

          try {
            if (iter === 1) {
              await self.agent.LoopPrompt(controller.signal, self.config, task, self.agent.emit);
            } else {
              await self.agent.LoopContinue(controller.signal, self.config, task, iter, self.agent.emit);
            }

            if (self.loopState?.aborted || controller.signal.aborted || self.agent.userAbortFired()) {
              if (self.agent.emit) {
                self.agent.emit({
                  kind: "loop_status",
                  data: { active: false, iteration: 0, maxIterations: 0, task: "" }
                });
              }
              self.loopState = null;
              return resume(Effect.void);
            }

            // Check if loop target completed
            let complete = false;
            for (let i = self.agent.messages.length - 1; i >= 0; i--) {
              if (self.agent.messages[i].role === "assistant") {
                const asstText = content_string(self.agent.messages[i]);
                if (asstText.includes(`<promise>${promise}</promise>`)) {
                  complete = true;
                }
                break;
              }
            }

            if (complete) {
              if (self.agent.emit) {
                self.agent.emit({
                  kind: "system",
                  data: `Loop finished successfully after ${iter} iterations.`,
                });
                self.agent.emit({
                  kind: "loop_status",
                  data: { active: false, iteration: 0, maxIterations: 0, task: "" }
                });
              }
              self.loopState = null;
              return resume(Effect.void);
            }

            if (maxIter > 0 && iter >= maxIter) {
              if (self.agent.emit) {
                self.agent.emit({
                  kind: "system",
                  data: `Loop stopped: max iterations (${maxIter}) reached.`,
                });
                self.agent.emit({
                  kind: "loop_status",
                  data: { active: false, iteration: 0, maxIterations: 0, task: "" }
                });
              }
              self.loopState = null;
              return resume(Effect.void);
            }

            // Small breathing room before next iteration
            await new Promise((r) => setTimeout(r, 100));
            await runIteration();
          } catch (cause) {
            if (self.agent.emit) {
              self.agent.emit({
                kind: "loop_status",
                data: { active: false, iteration: 0, maxIterations: 0, task: "" }
              });
            }
            if (self.loopState?.aborted || controller.signal.aborted || self.agent.userAbortFired()) {
              console.log("Loop execution interrupted by user.");
              self.loopState = null;
              return resume(Effect.void);
            }
            self.loopState = null;
            return resume(
              Effect.fail(cause instanceof Error ? cause : new Error(String(cause)))
            );
          }
        };

        runIteration().catch((err) => {
          if (self.agent.emit) {
            self.agent.emit({
              kind: "loop_status",
              data: { active: false, iteration: 0, maxIterations: 0, task: "" }
            });
          }
          self.loopState = null;
          resume(Effect.fail(err instanceof Error ? err : new Error(String(err))));
        });
      });
    }

    // Standard non-loop prompt path
    return Effect.async<void, Error>((resume) => {
      const controller = new AbortController();
      const mappedAttachments = attachments?.map((att) => ({
        MIMEType: att.mime,
        Data: Buffer.from(att.data, "base64"),
      })) || null;

      self.agent
        .Prompt(controller.signal, self.config, text, mappedAttachments, self.agent.emit)
        .then(() => resume(Effect.void))
        .catch((cause) =>
          resume(Effect.fail(cause instanceof Error ? cause : new Error(String(cause))))
        );
    });
  }

  interrupt(): Effect.Effect<void, Error> {
    const self = this;
    return Effect.sync(() => {
      self.agent.Abort();
    });
  }

  setModel(provider: string, model: string, thinkingLevel?: string): Effect.Effect<void, Error> {
    const self = this;
    return Effect.gen(function* () {
      yield* apply_provider_model(provider, model, thinkingLevel || "");
      const newCfg = yield* load_runtime();
      self.config = newCfg;
      self.agent.UpdateConfig(newCfg);
    }).pipe(
      Effect.mapError((err) => (err instanceof Error ? err : new Error(String(err))))
    );
  }
}

export const AgentRuntime = Context.GenericTag<AgentRuntimeImpl>("AgentRuntime");
