// PORT STATUS: active
// Maps core agent events + DesktopBridge responses → renderer BackendMessage shapes.
//
// Renderer (hollowClient.ts handleBackendMessage) expects these message types:
//   token / thinking / tool / done / error / session.* / models.catalog / ready

import type { DesktopResponse } from "../desktop_bridge";

// Renderer BackendMessage contract — mirrors hollowClient.ts BackendMessage.
export type BackendMessage =
  | { type: "ready" }
  | { type: "session.list"; sessions: unknown[] | null }
  | { type: "session.history"; sessionId: string; cwd?: string; messages: unknown[] | null }
  | { type: "models.catalog"; providers: unknown[]; models: unknown[]; state: unknown }
  | { type: "connections.list"; connections: unknown[]; catalog: unknown }
  | { type: "connection.changed"; connections: unknown[]; catalog: unknown; error?: string }
  | { type: "codex.login.start"; user_code: string; verify_url: string; poll_interval: number }
  | { type: "codex.login.cancelled" }
  | { type: "repoStatus"; added: number; removed: number; branch: string; contextPct: number }
  | { type: "token"; text?: string }
  | { type: "thinking"; text?: string }
  | {
      type: "tool";
      id: string;
      name: string;
      arguments: string;
      status: "running" | "completed" | "failed";
      result?: string;
      details?: string;
    }
  | { type: "done" }
  | { type: "error"; message: string }
  | { type: "loop.status"; active: boolean; iteration: number; maxIterations: number; task: string };

type CoreEvent = { kind: string; data: unknown };

const event_assistant_start = "assistant_start";
const event_assistant_delta = "assistant_delta";
const event_assistant_thinking_delta = "assistant_thinking_delta";
const event_assistant_message = "assistant_message";
const event_tool_start = "tool_start";
const event_tool_delta = "tool_delta";
const event_tool_result = "tool_result";
const event_error = "error";

const as_string = (v: unknown): string => (typeof v === "string" ? v : "");
const as_record = (v: unknown): Record<string, unknown> =>
  v !== null && typeof v === "object" && !Array.isArray(v)
    ? (v as Record<string, unknown>)
    : {};

const string_field = (obj: Record<string, unknown>, key: string): string =>
  as_string(obj[key]);

/**
 * Map a core agent event { kind, data } → a renderer BackendMessage.
 * Returns null for events the renderer does not render (system, compaction, evidence, …).
 */
export const mapAgentEvent = (event: unknown): BackendMessage | null => {
  if (event === null || typeof event !== "object") return null;
  const { kind, data } = event as CoreEvent;
  switch (kind) {
    case event_assistant_start:
      return null; // renderer treats the first token as stream start; no separate message

    case event_assistant_delta: {
      const text = as_string(data);
      return text === "" ? null : { type: "token", text };
    }

    case event_assistant_thinking_delta: {
      const text = as_string(data);
      return text === "" ? null : { type: "thinking", text };
    }

    case event_assistant_message:
      // A full assistant message + turn completion.
      return { type: "done" };

    case event_tool_start: {
      const d = as_record(data);
      return {
        type: "tool",
        id: string_field(d, "id"),
        name: string_field(d, "name"),
        arguments: string_field(d, "args"),
        status: "running",
      };
    }

    case event_tool_delta: {
      const d = as_record(data);
      const id = string_field(d, "id");
      const name = string_field(d, "name");
      const result = string_field(d, "result");
      if (id === "" && result === "") return null;
      // tool_delta extends the running tool's output; renderer upserts.
      return {
        type: "tool",
        id,
        name,
        arguments: "",
        status: "running",
        result,
      };
    }

    case event_tool_result: {
      const d = as_record(data);
      const err = d.error === true;
      return {
        type: "tool",
        id: string_field(d, "id"),
        name: string_field(d, "name"),
        arguments: string_field(d, "args"),
        status: err ? "failed" : "completed",
        result: string_field(d, "result"),
        details: d.details ? (typeof d.details === "string" ? d.details : JSON.stringify(d.details)) : undefined,
      };
    }

    case event_error: {
      const msg = as_string(data);
      return { type: "error", message: msg === "" ? "unknown error" : msg };
    }

    case "connection.changed": {
      // Bridge-emitted (not a core agent event): Settings connection state changed
      // after a key was saved/removed or the Codex OAuth login completed/failed.
      const d = as_record(data);
      return {
        type: "connection.changed",
        connections: Array.isArray(d.connections) ? d.connections : [],
        catalog: d.catalog,
        error: typeof d.error === "string" && d.error !== "" ? d.error : undefined,
      };
    }

    case "loop_status": {
      const d = as_record(data);
      return {
        type: "loop.status",
        active: d.active === true,
        iteration: Number(d.iteration) || 0,
        maxIterations: Number(d.maxIterations) || 0,
        task: as_string(d.task)
      };
    }

    default:
      // system / compaction / evidence / obligation / workflow / branch_summary / log / phase — not rendered in the chat surface (yet)
      return null;
  }
};

/**
 * Map a DesktopBridge dispatch response → one or more renderer BackendMessages.
 * Dispatch responses are synchronous command results (session list, history, models, acks).
 */
export const mapDispatchResponse = (
  response: DesktopResponse,
): BackendMessage | BackendMessage[] => {
  switch (response.type) {
    case "session.list":
      return { type: "session.list", sessions: response.sessions };

    case "session.history":
      return {
        type: "session.history",
        sessionId: response.sessionId,
        cwd: response.cwd,
        messages: response.messages,
      };

    case "models.catalog":
      return {
        type: "models.catalog",
        providers: response.providers,
        models: response.models,
        state: response.state,
      };

    case "connections.list":
      return {
        type: "connections.list",
        connections: response.connections,
        catalog: response.catalog,
      };

    case "codex.login.start":
      return {
        type: "codex.login.start",
        user_code: response.user_code,
        verify_url: response.verify_url,
        poll_interval: response.poll_interval,
      };

    case "codex.login.cancelled":
      return { type: "codex.login.cancelled" };

    case "repoStatus":
      return {
        type: "repoStatus",
        added: response.added,
        removed: response.removed,
        branch: response.branch,
        contextPct: response.contextPct,
      };

    case "deleteSession.success":
    case "setModel.success":
    case "interrupt.success":
      // The renderer refreshes session/model state on its own after these;
      // no explicit BackendMessage is required.
      return { type: "ready" };

    case "prompt.success":
      // Go serve.go always sends "done" after Prompt returns; mirror that for IPC.
      return { type: "done" };

    default:
      return { type: "ready" };
  }
};
