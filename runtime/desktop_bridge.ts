// PORT STATUS: active
// source path: runtime/desktop_bridge.ts
// confidence: high
// status: phase_b_compile

/**
 * Renderer IPC Layer wraps this; agent never leaves main process.
 */

import { Effect, Stream, PubSub } from "effect";
import os from "node:os";
import path from "node:path";
import { AgentRuntimeImpl, NOT_CONNECTED } from "./agent_runtime";
import {
  DesktopCommand,
  DesktopEvent,
  type ConnectionInfo,
} from "./schemas";
import { manager } from "../backend/session/manager";
import { info } from "../backend/session/types";
import { format_relative } from "../backend/session/list";
import {
  model_providers,
  models_for_provider,
  provider_codex,
  provider_opencode,
  provider_opencode_zen,
  provider_neuralwatt,
  lookup_catalog_model
} from "../backend/opencode/providers";
import {
  default_registry,
  format_context_window
} from "../backend/opencode/models";
import {
  supported_thinking_levels,
  format_thinking_level_for_model,
  supports_thinking
} from "../backend/opencode/thinking";
import {
  apply_provider_model,
  connection_settings,
  enable_codex_provider,
} from "../backend/config/provider";
import { delete_api_key, has_api_key, get_api_key } from "../backend/secrets/store";
import {
  has_codex_auth,
  resolve_codex_credentials,
  start_codex_device_auth,
  poll_codex_device_auth,
  type device_auth_start,
} from "../backend/auth/codex_oauth";
import { save_api_key } from "../backend/auth/connect";
import { clear_codex_auth } from "../backend/auth/store";
import {
  default_endpoint,
  default_zen_endpoint,
  default_neuralwatt_endpoint,
  default_model,
  default_zen_model,
  default_neuralwatt_model,
  default_codex_model,
} from "../backend/config/config";

export type SessionResponse = {
  id: string;
  path: string;
  cwd: string;
  title: string;
  createdAt: string; // relative formatted time
  created: string; // ISO string
  modified: string; // ISO string
  messageCount: number;
};

export type WsHistoryTool = {
  id: string;
  name: string;
  arguments: string;
  status: "completed" | "failed";
  result?: string;
};

export type WsHistoryMessage = {
  id: string;
  role: string;
  content: string;
  thinking?: string;
  timestamp: string;
  tools?: WsHistoryTool[];
};

export interface WsProviderDTO {
  id: string;
  name: string;
  connected: boolean;
}

export interface WsModelDTO {
  id: string;
  name: string;
  provider: string;
  contextWindow: number;
  contextLabel: string;
  reasoning: boolean;
  thinkingLevels: string[];
  thinkingLevelLabels: string[];
}

export interface WsModelStateDTO {
  provider: string;
  modelId: string;
  modelName: string;
  thinkingLevel: string;
  contextLabel: string;
  reasoning: boolean;
}

export interface WsModelsCatalog {
  type: "models.catalog";
  providers: WsProviderDTO[];
  models: WsModelDTO[];
  state: WsModelStateDTO;
}

export type DesktopResponse =
  | { type: "session.list"; sessions: SessionResponse[] }
  | { type: "session.history"; sessionId: string; cwd: string; messages: WsHistoryMessage[] }
  | { type: "deleteSession.success" }
  | { type: "prompt.success" }
  | { type: "interrupt.success" }
  | { type: "setModel.success" }
  | { type: "connections.list"; connections: ConnectionInfo[]; catalog: WsModelsCatalog }
  | { type: "codex.login.start"; user_code: string; verify_url: string; poll_interval: number }
  | { type: "codex.login.cancelled" }
  | WsModelsCatalog;

/** Payload carried by the `connection.changed` event (and the connections responses). */
export type ConnectionPayload = {
  connections: ConnectionInfo[];
  catalog: WsModelsCatalog;
  error?: string;
};

/** Normalize any thrown error (config/secrets/auth errors carry `reason`, not `message`) into an Error. */
const asError = (e: unknown): Error => {
  if (e instanceof Error) return e;
  if (e && typeof e === "object") {
    const o = e as Record<string, unknown>;
    const r = String(o.reason ?? o.message ?? "");
    if (r) return new Error(r);
  }
  return new Error(typeof e === "string" && e ? e : "unknown error");
};

/** Default model id for a provider, used when switching active provider on connect. */
const defaultModelFor = (provider: string): string => {
  switch (provider) {
    case provider_codex:
      return default_codex_model;
    case provider_opencode_zen:
      return default_zen_model;
    case provider_neuralwatt:
      return default_neuralwatt_model;
    default:
      return default_model;
  }
};

/** Build the full Settings snapshot: refreshed model catalog + per-provider connection state. */
const buildConnectionsResult = (
  runtime: AgentRuntimeImpl,
): Effect.Effect<{ type: "connections.list"; connections: ConnectionInfo[]; catalog: WsModelsCatalog }, never> =>
  Effect.gen(function* () {
    yield* refreshDesktopModelRegistry();
    const catalog = yield* buildModelsCatalog(runtime);
    const connections = yield* Effect.all(
      model_providers().map((p) =>
        Effect.map(checkConnected(p.id), (connected): ConnectionInfo => ({
          provider: p.id,
          displayName: p.name,
          kind: p.id === provider_codex ? "oauth" : "key",
          connected,
        })),
      ),
    );
    return { type: "connections.list" as const, connections, catalog };
  });

const shouldShowDesktopSession = (cwd: string): boolean => {
  if (cwd === "." || cwd === "") {
    return true;
  }
  const cleanCwd = path.resolve(cwd);
  const tmp = path.resolve(os.tmpdir());
  if (!cleanCwd.startsWith(tmp + path.sep)) {
    return true;
  }
  try {
    const rel = path.relative(tmp, cleanCwd);
    const parts = rel.split(path.sep);
    for (const part of parts) {
      if (
        part.startsWith("Test") ||
        part.startsWith("enough-test-tree-") ||
        part.includes("enough-test-tree")
      ) {
        return false;
      }
    }
  } catch {
    return true;
  }
  return true;
};

const mapSessionInfo = (info: info): SessionResponse => {
  return {
    id: info.id,
    path: info.path,
    cwd: info.cwd,
    title: info.first_message,
    createdAt: format_relative(info.modified),
    created: info.created.toISOString(),
    modified: info.modified.toISOString(),
    messageCount: info.message_count,
  };
};

const buildHistory = (sm: manager): WsHistoryMessage[] => {
  const history: WsHistoryMessage[] = [];
  for (const line of sm.chat_lines()) {
    if (line.role === "user") {
      history.push({
        id: `msg-${history.length}`,
        role: "user",
        content: line.text,
        timestamp: "Just now",
      });
    } else if (line.role === "assistant") {
      history.push({
        id: `msg-${history.length}`,
        role: "assistant",
        content: line.text,
        thinking: line.thinking,
        timestamp: "Just now",
      });
    } else if (line.role === "tool") {
      if (history.length > 0 && history[history.length - 1].role === "assistant") {
        const lastIdx = history.length - 1;
        if (!history[lastIdx].tools) {
          history[lastIdx].tools = [];
        }
        history[lastIdx].tools!.push({
          id: `tool-${history[lastIdx].tools!.length}`,
          name: line.tool_name,
          arguments: line.tool_args,
          status: line.tool_error ? "failed" : "completed",
          result: line.tool_result,
        });
      }
    } else if (line.role === "system" || line.role === "error") {
      history.push({
        id: `msg-${history.length}`,
        role: "system",
        content: line.text,
        timestamp: "Just now",
      });
    }
  }
  return history;
};

const checkConnected = (providerId: string): Effect.Effect<boolean, never> => {
  switch (providerId) {
    case provider_codex:
      return has_codex_auth();
    case provider_opencode:
    case provider_opencode_zen:
      return has_api_key(provider_opencode);
    case provider_neuralwatt:
      return has_api_key(provider_neuralwatt);
    default:
      return has_api_key(providerId);
  }
};

const refreshDesktopModelRegistry = (): Effect.Effect<void, never> => {
  return Effect.gen(function* () {
    // OpenCode and Zen share the same account/key; NeuralWatt has its own.
    const go_key_res = yield* Effect.either(get_api_key(provider_opencode));
    if (go_key_res._tag === "Right" && go_key_res.right !== "") {
      const key = go_key_res.right;
      yield* Effect.promise(() => default_registry.refresh(undefined, provider_opencode, default_endpoint, key));
      yield* Effect.promise(() => default_registry.refresh(undefined, provider_opencode_zen, default_zen_endpoint, key));
    }
    const neuralwatt_key_res = yield* Effect.either(get_api_key(provider_neuralwatt));
    if (neuralwatt_key_res._tag === "Right" && neuralwatt_key_res.right !== "") {
      yield* Effect.promise(() => default_registry.refresh(undefined, provider_neuralwatt, default_neuralwatt_endpoint, neuralwatt_key_res.right));
    }
    const has_cdx = yield* has_codex_auth();
    if (has_cdx) {
      const creds_res = yield* Effect.either(resolve_codex_credentials(new AbortController().signal));
      if (creds_res._tag === "Right") {
        yield* Effect.promise(() => default_registry.refresh_codex(undefined, creds_res.right.access_token));
      }
    }
  }).pipe(
    Effect.catchAll(() => Effect.void)
  );
};

const buildModelsCatalog = (runtime: AgentRuntimeImpl): Effect.Effect<WsModelsCatalog, never> => {
  return Effect.gen(function* () {
    const providers = yield* Effect.all(
      model_providers().map((p) =>
        Effect.map(checkConnected(p.id), (connected) => ({
          id: p.id,
          name: p.name,
          connected,
        }))
      )
    );

    const models: WsModelDTO[] = [];
    for (const p of model_providers()) {
      const pModels = models_for_provider(p.id, default_registry);
      for (const m of pModels) {
        const levels = supported_thinking_levels(m.id);
        const outLevels = levels.map((lvl) => String(lvl));
        const outLabels = levels.map((lvl) => format_thinking_level_for_model(m.id, lvl));
        models.push({
          id: m.id,
          name: m.name,
          provider: p.id,
          contextWindow: m.context_window,
          contextLabel: format_context_window(m.context_window),
          reasoning: m.reasoning,
          thinkingLevels: outLevels,
          thinkingLevelLabels: outLabels,
        });
      }
    }

    const settings = yield* Effect.either(connection_settings());
    let provider = provider_opencode;
    let modelId = "deepseek-v4-flash";
    if (settings._tag === "Right") {
      provider = settings.right[0] || provider_opencode;
      modelId = settings.right[2] || "deepseek-v4-flash";
    }

    const cfg = runtime.config;
    let thinking = cfg?.thinking_level || "";
    if (thinking === "" && supports_thinking(modelId)) {
      thinking = "medium";
    }

    let name = modelId;
    let contextLabel = "";
    let reasoning = false;

    const pModels = models_for_provider(provider, default_registry);
    for (const m of pModels) {
      if (m.id === modelId) {
        name = m.name;
        contextLabel = format_context_window(m.context_window);
        reasoning = m.reasoning;
        break;
      }
    }
    if (contextLabel === "") {
      const [m, ok] = lookup_catalog_model(modelId);
      if (ok) {
        name = m.name;
        contextLabel = format_context_window(m.context_window);
        reasoning = m.reasoning;
      }
    }

    const state: WsModelStateDTO = {
      provider,
      modelId,
      modelName: name,
      thinkingLevel: thinking,
      contextLabel,
      reasoning,
    };

    return {
      type: "models.catalog" as const,
      providers,
      models,
      state,
    };
  });
};

export class DesktopBridge {
  /** Bridge-emitted events (e.g. connection.changed) merged into the event stream. */
  private connectionEvents: PubSub.PubSub<DesktopEvent>;
  /** A single in-flight Codex OAuth device-code login, if any. */
  private codexLogin: { controller: AbortController; start: device_auth_start } | null = null;

  constructor(private runtime: AgentRuntimeImpl) {
    this.connectionEvents = Effect.runSync(PubSub.unbounded<DesktopEvent>());
  }

  /** Emit a bridge (non-agent) event on the merged stream the renderer subscribes to. */
  private emitConnectionChanged = (payload: ConnectionPayload): void => {
    const event: DesktopEvent = { kind: "connection.changed", data: payload } as DesktopEvent;
    Effect.runSync(PubSub.publish(this.connectionEvents, event));
  };

  dispatch(command: DesktopCommand): Effect.Effect<DesktopResponse, Error> {
    const runtime = this.runtime;
    const self = this;
    return Effect.gen(function* () {
      // Commands that need a booted agent. listSessions/listModels/openSession/
      // deleteSession work without one (disk reads only), so the UI can render
      // history and the "connect a provider" state instead of erroring on every call.
      const requiresAgent =
        command.type === "newSession" ||
        command.type === "prompt" ||
        command.type === "interrupt" ||
        command.type === "setModel";
      if (requiresAgent && !runtime.available) {
        return yield* Effect.fail(new Error(NOT_CONNECTED));
      }
      switch (command.type) {
        case "listSessions": {
          const list = yield* runtime.listSessions();
          const filtered = list
            .filter((info) => shouldShowDesktopSession(info.cwd))
            .map(mapSessionInfo);
          return { type: "session.list" as const, sessions: filtered };
        }
        case "openSession": {
          const sm = yield* runtime.openSession(command.id);
          const history = buildHistory(sm);
          return {
            type: "session.history" as const,
            sessionId: sm.session_id(),
            cwd: sm.cwd(),
            messages: history,
          };
        }
        case "newSession": {
          yield* runtime.newSession(command.cwd || undefined);
          const sm = runtime.agent.session;
          if (!sm) {
            return yield* Effect.fail(new Error("No active session loaded"));
          }
          return {
            type: "session.history" as const,
            sessionId: sm.session_id(),
            cwd: sm.cwd(),
            messages: [],
          };
        }
        case "deleteSession": {
          yield* runtime.deleteSession(command.id);
          return { type: "deleteSession.success" as const };
        }
        case "prompt": {
          yield* runtime.prompt(command.text, command.attachments || undefined);
          return { type: "prompt.success" as const };
        }
        case "interrupt": {
          yield* runtime.interrupt();
          return { type: "interrupt.success" as const };
        }
        case "setModel": {
          yield* runtime.setModel(command.provider, command.model, command.thinkingLevel || undefined);
          return yield* buildModelsCatalog(runtime);
        }
        case "listModels": {
          yield* refreshDesktopModelRegistry();
          const catalog = yield* buildModelsCatalog(runtime);
          return catalog;
        }
        case "listConnections": {
          return yield* buildConnectionsResult(runtime);
        }
        case "setApiKey": {
          const provider = command.provider;
          if (provider === provider_codex) {
            return yield* Effect.fail(
              new Error("Codex uses OAuth login — use “Connect Codex” instead of an API key."),
            );
          }
          // opencode + zen share one key slot (provider_opencode); zen's key is
          // read from provider_opencode (apply_provider_model:147 / load_runtime),
          // so store it there — NOT under provider_opencode_zen.
          const slot = provider === provider_opencode_zen ? provider_opencode : provider;
          yield* Effect.gen(function* () {
            yield* save_api_key(command.key, slot);
            // Switch the active provider to the one just connected, forcing its
            // default model. apply_provider_model validates the just-saved key.
            yield* apply_provider_model(provider, defaultModelFor(provider), "");
          }).pipe(Effect.mapError(asError));
          yield* runtime.reconnect();
          return yield* buildConnectionsResult(runtime);
        }
        case "removeKey": {
          const provider = command.provider;
          if (provider === provider_codex) {
            yield* clear_codex_auth().pipe(Effect.mapError(asError));
          } else {
            const slot = provider === provider_opencode_zen ? provider_opencode : provider;
            yield* delete_api_key(slot).pipe(Effect.mapError(asError));
          }
          // Removing the active provider's key degrades the runtime (available=false).
          yield* runtime.reconnect();
          return yield* buildConnectionsResult(runtime);
        }
        case "startCodexLogin": {
          // Abort any previous in-flight login before starting a new one.
          if (self.codexLogin) {
            self.codexLogin.controller.abort();
            self.codexLogin = null;
          }
          const controller = new AbortController();
          const start = yield* start_codex_device_auth(controller.signal).pipe(Effect.mapError(asError));
          self.codexLogin = { controller, start };
          // Detached poll: on success enable codex + reconnect + notify; on
          // failure/abort notify with an error (or silently if user-cancelled).
          void Effect.runPromise(poll_codex_device_auth(controller.signal, start).pipe(Effect.mapError(asError)))
            .then(async () => {
              await Effect.runPromise(enable_codex_provider().pipe(Effect.mapError(asError)));
              await Effect.runPromise(runtime.reconnect());
              const result = await Effect.runPromise(buildConnectionsResult(runtime));
              self.emitConnectionChanged(result);
            })
            .catch(async (e) => {
              const result = await Effect.runPromise(buildConnectionsResult(runtime));
              self.emitConnectionChanged({
                connections: result.connections,
                catalog: result.catalog,
                error: controller.signal.aborted ? undefined : asError(e).message,
              });
            });
          return {
            type: "codex.login.start" as const,
            user_code: start.user_code,
            verify_url: start.verify_url,
            poll_interval: start.poll_interval,
          };
        }
        case "cancelCodexLogin": {
          if (self.codexLogin) {
            self.codexLogin.controller.abort();
            self.codexLogin = null;
          }
          return { type: "codex.login.cancelled" as const };
        }
        default:
          return yield* Effect.fail(new Error(`Unknown command type: ${(command as any).type}`));
      }
    });
  }

  subscribeEvents(): Stream.Stream<DesktopEvent, never> {
    // Merge agent events (prompt streaming) with bridge events (connection.changed).
    return Stream.merge(
      Stream.fromPubSub(this.runtime.pubsub),
      Stream.fromPubSub(this.connectionEvents),
    );
  }
}
