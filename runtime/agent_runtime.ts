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

      self.config = cfgResult.right;
      yield* EnsureBootstrapped();

      const sm = yield* continue_recent(self.workDir);
      self.agent = New(self.config, self.workDir, sm);

      self.agent.emit = (event: any) => {
        // Synchronous publish: prompt completion must mean all emitted events
        // are already delivered to subscribers before emit() returns.
        Effect.runSync(PubSub.publish(self.pubsub, event));
      };
      self.available = true;
    }).pipe(
      Effect.mapError((err) => {
        if (err instanceof Error) return err;
        return new Error(errReason(err) || "boot failed");
      })
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
      if (self.available) self.agent.LoadSession(sm);
      return sm;
    });
  }

  newSession(cwd?: string): Effect.Effect<string, Error> {
    const self = this;
    return Effect.gen(function* () {
      const targetCwd = cwd || self.workDir;
      const sm = yield* start_new(targetCwd);
      self.agent.LoadSession(sm);
      return sm.session_id();
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
        return yield* Effect.fail(new Error("cannot delete the active session"));
      }
      yield* delete_session(targetPath);
    });
  }

  prompt(text: string, attachments?: readonly any[]): Effect.Effect<void, Error> {
    const self = this;
    // Bridge dispatch already guards agent-requiring commands; this guard is for
    // the CLI entry (runtime/main.ts --prompt), which calls prompt() directly.
    if (!self.available) return Effect.fail(new Error(NOT_CONNECTED));
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
