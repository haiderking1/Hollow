// PORT: backend/opencode/codex_stream.go

import { Effect } from "effect";
import { codex_cloudflare_headers } from "../auth/codex_headers";
import { client, client_error, stream_status_error } from "./client";
import { prepare_request_messages } from "./messages";
import { for_each_sse_block, err_sse_done, type sse_block, drain_sse_buffer, consume_sse_block } from "./sse";
import {
  blocks_content,
  content_blocks,
  content_string,
  get_reasoning,
  string_content,
  type chat_request,
  type message,
  type tool,
  type tool_call,
  type usage
} from "./types";
import type { stream_callbacks } from "./stream";

import {
  codex_request_headers,
  responses_url,
  build_responses_request,
  parse_responses_message,
  type responses_item,
  type responses_response,
  type responses_usage,
  type responses_error
} from "./codex";

export type codex_stream_state = {
  collected_items: responses_item[];
  text_deltas: string[];
  reasoning_deltas: string[];
  has_tool_calls: boolean;
  terminal_status: string;
  terminal_usage?: responses_usage;
  terminal_error?: responses_error;
  saw_terminal: boolean;
  saw_data: boolean;
};

export const chat_responses_stream_once = (
  c: client,
  ctx: AbortSignal,
  req: chat_request,
  cb: stream_callbacks
): Effect.Effect<message, client_error> => {
  return Effect.tryPromise({
    try: async () => {
      const body_req = build_responses_request(c, req);
      body_req.stream = true;

      const body = JSON.stringify(body_req);
      const headers: Record<string, string> = {
        "Content-Type": "application/json",
        Authorization: `Bearer ${c.api_key}`,
        Accept: "text/event-stream",
        ...codex_request_headers(c.api_key),
      };

      const resp = await fetch(responses_url(c), {
        method: "POST",
        signal: ctx,
        headers,
        body,
      });

      if (resp.status >= 400) {
        const raw = await resp.text();
        throw new stream_status_error(
          resp.status,
          `codex ${resp.status}: ${raw.trim()}`
        );
      }

      if (!resp.body) {
        throw new Error("codex: empty SSE body");
      }

      const state = await consume_codex_responses_sse(resp.body, ctx, cb);
      if (!state.saw_data) {
        throw new Error("codex: empty SSE body");
      }

      return message_from_codex_stream_state(state);
    },
    catch: (cause) => {
      return client_error("chat_responses_stream_once", cause);
    },
  });
};

export const consume_codex_responses_sse = async (
  stream: ReadableStream<Uint8Array>,
  ctx: AbortSignal,
  cb: stream_callbacks
): Promise<codex_stream_state> => {
  const state: codex_stream_state = {
    collected_items: [],
    text_deltas: [],
    reasoning_deltas: [],
    has_tool_calls: false,
    terminal_status: "completed",
    saw_terminal: false,
    saw_data: false,
  };

  let done_flag = false;
  let process_err: Error | null = null;

  const fn = (block: sse_block): void | Error => {
    if (ctx.aborted) {
      return ctx.reason || new Error("aborted");
    }
    if (block.done) {
      done_flag = true;
      return err_sse_done;
    }
    const data = block.data;
    if (data === "") {
      return;
    }
    state.saw_data = true;

    let event: Record<string, unknown>;
    try {
      event = JSON.parse(data) as Record<string, unknown>;
    } catch {
      return;
    }

    let event_type = block.event_type;
    if (typeof event.type === "string" && event.type !== "") {
      event_type = event.type;
    }

    try {
      apply_codex_stream_event(state, event_type, event, cb);
    } catch (err) {
      if (err instanceof Error) {
        process_err = err;
        return err;
      }
      process_err = new Error(String(err));
      return process_err;
    }

    if (state.saw_terminal) {
      done_flag = true;
      return err_sse_done;
    }
  };

  let buf = "";
  const dec = new TextDecoder();
  const reader = stream.getReader();
  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) {
        break;
      }
      buf += dec.decode(value, { stream: true });
      buf = drain_sse_buffer(buf, fn);
      if (done_flag || process_err) {
        break;
      }
    }
    if (buf.length > 0 && !done_flag && !process_err) {
      consume_sse_block(buf, fn);
    }
  } finally {
    reader.releaseLock();
  }

  if (process_err) {
    throw process_err;
  }

  if (!state.saw_terminal && state.collected_items.length === 0 && state.text_deltas.length === 0) {
    throw new Error("codex: stream ended without terminal response or content");
  }

  return state;
};

export const apply_codex_stream_event = (
  state: codex_stream_state,
  event_type: string,
  event: Record<string, unknown>,
  cb: stream_callbacks
): void => {
  if (event_type === "error") {
    let msg = "codex stream error";
    if (typeof event.message === "string" && event.message !== "") {
      msg = event.message;
    }
    throw new Error(`codex: ${msg}`);
  }

  if (event_type.includes("output_text.delta") || event_type === "response.output_text.delta") {
    const delta = codex_event_string(event, "delta");
    if (delta !== "") {
      state.text_deltas.push(delta);
      if (!state.has_tool_calls && cb.on_text) {
        cb.on_text(delta);
      }
    }
    return;
  }

  if (event_type.includes("reasoning") && event_type.includes("delta")) {
    const delta = codex_event_string(event, "delta");
    if (delta !== "") {
      state.reasoning_deltas.push(delta);
      if (cb.on_thinking) {
        cb.on_thinking(delta);
      }
    }
    return;
  }

  if (event_type.includes("function_call")) {
    state.has_tool_calls = true;
  }

  if (event_type === "response.output_item.done") {
    const raw_item = event.item;
    if (raw_item !== undefined && raw_item !== null) {
      const item = raw_item as responses_item;
      state.collected_items.push(item);
      if (item.type === "function_call") {
        state.has_tool_calls = true;
      }
    }
    return;
  }

  switch (event_type) {
    case "response.completed":
    case "response.incomplete":
    case "response.failed": {
      state.saw_terminal = true;
      const raw_response = event.response;
      if (raw_response !== undefined && raw_response !== null) {
        const terminal = raw_response as {
          status?: string;
          usage?: responses_usage;
          error?: responses_error;
        };
        if (terminal.status !== undefined && terminal.status !== "") {
          state.terminal_status = terminal.status;
        }
        state.terminal_usage = terminal.usage;
        state.terminal_error = terminal.error;
      }
      switch (event_type) {
        case "response.completed":
          if (state.terminal_status === "") {
            state.terminal_status = "completed";
          }
          break;
        case "response.incomplete":
          if (state.terminal_status === "") {
            state.terminal_status = "incomplete";
          }
          break;
        case "response.failed":
          if (state.terminal_status === "") {
            state.terminal_status = "failed";
          }
          break;
      }
      break;
    }
  }
};

export const codex_event_string = (event: Record<string, unknown>, key: string): string => {
  const val = event[key];
  if (typeof val === "string") {
    return val;
  }
  return "";
};

export const message_from_codex_stream_state = (state: codex_stream_state): message => {
  const resp: responses_response = {
    status: state.terminal_status,
    output: state.collected_items,
    error: state.terminal_error,
    usage: state.terminal_usage,
  };
  if (state.text_deltas.length > 0) {
    resp.output_text = state.text_deltas.join("");
  }
  const msg = parse_responses_message(resp);
  if (msg.reasoning_content === undefined && state.reasoning_deltas.length > 0) {
    msg.reasoning_content = state.reasoning_deltas.join("");
  }
  return msg;
};

/*
PORT STATUS
source path: backend/opencode/codex_stream.go
source lines: 238
draft lines: 472
confidence: high
status: phase_b_compile
*/
