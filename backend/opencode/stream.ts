import { Effect } from "effect";
import { client, client_error, stream_status_error } from "./client";
import { prepare_request_messages } from "./messages";
import { chat_responses_stream_once } from "./codex_stream";
import { drain_sse_buffer, consume_sse_block, err_sse_done, type sse_block } from "./sse";
import { string_content, marshal_chat_request, type chat_request, type message, type tool_call, type usage } from "./types";
import { sanitize_embedded_thinking, think_stream_splitter } from "./think_tags";

export type stream_callbacks = {
  on_text?: (text: string) => void;
  on_thinking?: (text: string) => void;
};

type tool_call_partial = {
  index: number;
  id?: string;
  type?: string;
  function?: {
    name?: string;
    arguments?: string;
  };
};

type stream_delta = {
  role?: string;
  content?: string;
  reasoning_content?: string;
  reasoning_details?: unknown;
  reasoning?: string;
  reasoning_text?: string;
  tool_calls?: tool_call_partial[];
};

type stream_chunk = {
  choices?: {
    delta: stream_delta;
    finish_reason?: string | null;
  }[];
  error?: {
    message?: string;
    type?: string;
  } | null;
  usage?: {
    prompt_tokens: number;
    completion_tokens: number;
    total_tokens: number;
  } | null;
};

const reasoning_text_from_delta = (value: unknown): string => {
  if (value === undefined || value === null || value === "") return "";
  if (typeof value === "string") return value;
  if (Array.isArray(value)) {
    const parts: string[] = [];
    for (const item of value) {
      if (typeof item === "string") {
        parts.push(item);
      } else if (item && typeof item === "object" && "text" in item) {
        const text = (item as { text?: unknown }).text;
        if (typeof text === "string" && text !== "") parts.push(text);
      }
    }
    return parts.join("");
  }
  return "";
};

/** MiniMax with reasoning_split sends cumulative text in reasoning_details — slice to incremental. */
const reasoning_increment = (
  d: stream_delta,
  cumulative: { details: string; content: string },
): string => {
  const details_text = reasoning_text_from_delta(d.reasoning_details);
  if (details_text !== "") {
    const inc = details_text.startsWith(cumulative.details)
      ? details_text.slice(cumulative.details.length)
      : details_text;
    cumulative.details = details_text;
    if (inc !== "") return inc;
  }

  const content_text = reasoning_text_from_delta(d.reasoning_content);
  if (content_text !== "") {
    const inc = content_text.startsWith(cumulative.content)
      ? content_text.slice(cumulative.content.length)
      : content_text;
    cumulative.content = content_text;
    if (inc !== "") return inc;
  }

  for (const value of [d.reasoning, d.reasoning_text]) {
    const text = reasoning_text_from_delta(value);
    if (text !== "") return text;
  }
  return "";
};

const delay_ms = (ms: number, signal?: AbortSignal) =>
  new Promise<void>((resolve, reject) => {
    if (signal?.aborted) {
      return reject(signal.reason || new Error("aborted"));
    }
    const on_abort = () => {
      clearTimeout(timeout);
      reject(signal?.reason || new Error("aborted"));
    };
    const timeout = setTimeout(() => {
      signal?.removeEventListener("abort", on_abort);
      resolve();
    }, ms);
    signal?.addEventListener("abort", on_abort);
  });

export const is_retriable_stream_error = (err: unknown): boolean => {
  if (!err) {
    return false;
  }
  if (err instanceof stream_status_error) {
    return err.status === 429 || err.status === 502 || err.status === 503;
  }
  if (err instanceof Error) {
    const text = err.message.toLowerCase();
    return (
      text.includes("connection reset") ||
      text.includes("unexpected eof") ||
      text.includes("empty sse") ||
      text.includes("stream reset")
    );
  }
  return false;
};

export const chat_stream_impl = (
  c: client,
  ctx: AbortSignal,
  req: chat_request,
  cb: stream_callbacks
): Effect.Effect<message, client_error> => {
  return chat_stream_once(c, ctx, req, cb);
};

export const chat_stream_retry_impl = (
  c: client,
  ctx: AbortSignal,
  req: chat_request,
  cb: stream_callbacks
): Effect.Effect<message, client_error> => {
  const worker_client = c.without_timeout();
  return Effect.tryPromise({
    try: async () => {
      let last_err: unknown = null;
      for (let attempt = 0; attempt < 4; attempt++) {
        try {
          if (ctx.aborted) {
            throw ctx.reason || new Error("aborted");
          }
          const msg = await Effect.runPromise(chat_stream_once(worker_client, ctx, req, cb));
          return msg;
        } catch (err) {
          last_err = err;
          if (ctx.aborted || !is_retriable_stream_error(err) || attempt === 3) {
            throw err;
          }
          let delay = 250 << attempt;
          if (delay > 5000) {
            delay = 5000;
          }
          const jitter = Math.floor(Math.random() * (delay / 4));
          await delay_ms(delay + jitter, ctx);
        }
      }
      throw last_err;
    },
    catch: (cause) => client_error("chat_stream_retry", cause),
  });
};

export const chat_stream_once = (
  c: client,
  ctx: AbortSignal,
  req: chat_request,
  cb: stream_callbacks
): Effect.Effect<message, client_error> => {
  if (c.codex) {
    return chat_responses_stream_once(c, ctx, req, cb);
  }
  return Effect.tryPromise({
    try: async () => {
      if (req.model === "") {
        req.model = c.model;
      }
      req.stream = true;
      req.stream_options = { include_usage: true };
      req.messages = prepare_request_messages(req.messages, req.model);

      const body = marshal_chat_request(req);
      const url = `${c.base_url}/chat/completions`;
      const resp = await fetch(url, {
        method: "POST",
        signal: ctx,
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${c.api_key}`,
          Accept: "text/event-stream",
        },
        body,
      });

      if (resp.status >= 400) {
        const raw = await resp.text();
        let api_err: { error?: { message?: string } } | null = null;
        try {
          api_err = JSON.parse(raw) as { error?: { message?: string } };
        } catch {}
        let msg = "";
        if (api_err?.error?.message !== undefined && api_err.error.message !== "") {
          msg = `opencode ${resp.status}: ${api_err.error.message}`;
        } else {
          msg = `opencode ${resp.status}: ${raw.trim()}`;
        }
        throw new stream_status_error(resp.status, msg);
      }

      if (!resp.body) {
        throw new Error("opencode: empty SSE body");
      }

      let content = "";
      let reasoning = "";
      const think_split = new think_stream_splitter();
      const reasoning_cumulative = { details: "", content: "" };
      let last_usage: usage | undefined = undefined;
      const tool_parts: Record<number, tool_call> = {};
      let saw_data = false;
      let done_flag = false;

      const emit_text = (t: string) => {
        content += t;
        if (cb.on_text) {
          cb.on_text(t);
        }
      };

      const emit_think = (t: string) => {
        reasoning += t;
        if (cb.on_thinking) {
          cb.on_thinking(t);
        }
      };

      const process_block = (block: sse_block): void | Error => {
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
        saw_data = true;

        let chunk: stream_chunk;
        try {
          chunk = JSON.parse(data) as stream_chunk;
        } catch {
          return;
        }

        if (chunk.error?.message !== undefined && chunk.error.message !== "") {
          return new Error(`opencode: ${chunk.error.message}`);
        }

        if (chunk.usage !== undefined && chunk.usage !== null) {
          last_usage = {
            input: chunk.usage.prompt_tokens,
            output: chunk.usage.completion_tokens,
            totalTokens: chunk.usage.total_tokens,
          };
        }

        if (chunk.choices === undefined || chunk.choices.length === 0) {
          return;
        }

        const delta = chunk.choices[0].delta;
        if (delta.content !== undefined && delta.content !== null && delta.content !== "") {
          think_split.feed(delta.content, emit_text, emit_think);
        }

        const r = reasoning_increment(delta, reasoning_cumulative);
        if (r !== "") {
          emit_think(r);
        }

        if (delta.tool_calls !== undefined && delta.tool_calls.length > 0) {
          for (const tc of delta.tool_calls) {
            let part = tool_parts[tc.index];
            if (part === undefined) {
              part = { id: "", type: "function", function: { name: "", arguments: "" } };
              tool_parts[tc.index] = part;
            }
            if (tc.id !== undefined && tc.id !== "") {
              part.id = tc.id;
            }
            if (tc.type !== undefined && tc.type !== "") {
              part.type = tc.type;
            }
            if (tc.function?.name !== undefined && tc.function.name !== "") {
              part.function.name = tc.function.name;
            }
            if (tc.function?.arguments !== undefined && tc.function.arguments !== "") {
              part.function.arguments += tc.function.arguments;
            }
          }
        }
      };

      let buf = "";
      const dec = new TextDecoder();
      const reader = resp.body.getReader();
      try {
        while (true) {
          const { done, value } = await reader.read();
          if (done) {
            break;
          }
          buf += dec.decode(value, { stream: true });
          buf = drain_sse_buffer(buf, process_block);
          if (done_flag) {
            break;
          }
        }
        if (buf.length > 0 && !done_flag) {
          consume_sse_block(buf, process_block);
        }
      } finally {
        reader.releaseLock();
      }

      if (!saw_data) {
        throw new Error("opencode: empty SSE body");
      }

      think_split.flush(emit_text, emit_think);

      const msg: message = {
        role: "assistant",
        content: new Uint8Array(),
        usage: last_usage,
      };

      if (content !== "") {
        msg.content = string_content(content);
      }
      if (reasoning !== "") {
        msg.reasoning_content = reasoning;
      }

      const tool_keys = Object.keys(tool_parts).map(Number);
      if (tool_keys.length > 0) {
        const max_idx = Math.max(...tool_keys);
        msg.tool_calls = [];
        for (let i = 0; i <= max_idx; i++) {
          const tc = tool_parts[i];
          if (tc !== undefined) {
            if (tc.id === "") {
              tc.id = `stream_call_${i}`;
            }
            if (tc.type === "") {
              tc.type = "function";
            }
            msg.tool_calls.push(tc);
          }
        }
      }

      sanitize_embedded_thinking(msg);
      return msg;
    },
    catch: (cause) => client_error("chat_stream", cause),
  });
};

