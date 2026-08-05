import { Effect } from "effect";
import { prepare_request_messages } from "./messages";
import type { chat_request, chat_response, message } from "./types";
import { marshal_chat_request } from "./types";
import { sanitize_embedded_thinking } from "./think_tags";
import { chat_stream_impl, chat_stream_retry_impl, type stream_callbacks } from "./stream";
import { chat_responses_once as codex_chat_responses_once } from "./codex";

export type client_error = {
  readonly _tag: "ClientError";
  readonly reason: string;
  readonly operation: string;
  readonly cause: unknown;
  readonly status?: number;
  readonly code?: string;
  readonly timeout?: boolean;
  readonly aborted?: boolean;
};

export class provider_error extends Error {
  readonly status?: number;
  readonly code?: string;
  readonly timeout: boolean;

  constructor(
    message: string,
    options: { status?: number; code?: string; cause?: unknown; timeout?: boolean } = {},
  ) {
    super(message, options.cause === undefined ? undefined : { cause: options.cause });
    this.name = "ProviderError";
    this.status = options.status;
    this.code = options.code;
    this.timeout = options.timeout ?? false;
  }
}

export class request_timeout_error extends provider_error {
  constructor(timeout_ms: number, cause?: unknown) {
    super(`provider request timed out after ${timeout_ms}ms`, { cause, timeout: true });
    this.name = "RequestTimeoutError";
  }
}

const error_message = (cause: unknown, fallback: string): string => {
  if (cause instanceof Error && cause.message.trim() !== "") return cause.message;
  if (typeof cause === "string" && cause.trim() !== "") return cause;
  return fallback;
};

const error_metadata = (cause: unknown): Pick<client_error, "status" | "code" | "timeout"> => {
  if (cause instanceof provider_error) {
    return { status: cause.status, code: cause.code, timeout: cause.timeout || undefined };
  }
  if (typeof cause === "object" && cause !== null) {
    const value = cause as { status?: unknown; code?: unknown; timeout?: unknown };
    return {
      status: typeof value.status === "number" ? value.status : undefined,
      code: typeof value.code === "string" ? value.code : undefined,
      timeout: value.timeout === true ? true : undefined,
    };
  }
  return {};
};

export const client_error = (
  operation: string,
  cause: unknown,
  caller?: AbortSignal,
): client_error => {
  if (typeof cause === "object" && cause !== null && (cause as { _tag?: unknown })._tag === "ClientError") {
    return cause as client_error;
  }
  return {
    _tag: "ClientError",
    reason: error_message(cause, operation),
    operation,
    cause,
    ...error_metadata(cause),
    aborted: caller?.aborted ? true : undefined,
  };
};

export type request_abort_context = {
  signal: AbortSignal;
  normalize_error: (cause: unknown) => unknown;
  close: () => void;
};

export const request_abort_context = (
  caller: AbortSignal,
  timeout_ms: number,
): request_abort_context => {
  if (!Number.isFinite(timeout_ms) || timeout_ms <= 0) {
    return {
      signal: caller,
      normalize_error: (cause) => caller.aborted ? caller.reason ?? cause : cause,
      close: () => {},
    };
  }

  const controller = new AbortController();
  let caller_reason: unknown;
  let timeout_error: request_timeout_error | undefined;
  let timer: ReturnType<typeof setTimeout> | undefined;

  const on_abort = () => {
    caller_reason = caller.reason ?? new DOMException("The operation was aborted", "AbortError");
    if (timer !== undefined) clearTimeout(timer);
    if (!controller.signal.aborted) controller.abort(caller_reason);
  };

  if (caller.aborted) {
    on_abort();
  } else {
    caller.addEventListener("abort", on_abort, { once: true });
    timer = setTimeout(() => {
      caller.removeEventListener("abort", on_abort);
      timeout_error = new request_timeout_error(timeout_ms);
      controller.abort(timeout_error);
    }, timeout_ms);
  }

  return {
    signal: controller.signal,
    normalize_error: (cause) => caller_reason ?? timeout_error ?? cause,
    close: () => {
      if (timer !== undefined) clearTimeout(timer);
      caller.removeEventListener("abort", on_abort);
    },
  };
};

export const with_request_abort_context = <T>(
  caller: AbortSignal,
  timeout_ms: number,
  run: (signal: AbortSignal) => Promise<T>,
): Promise<T> => {
  const request = request_abort_context(caller, timeout_ms);
  return run(request.signal)
    .catch((cause) => {
      throw request.normalize_error(cause);
    })
    .finally(request.close);
};

export class stream_status_error extends provider_error {
  constructor(status: number, message: string, code?: string, cause?: unknown) {
    super(message, { status, code, cause });
    this.name = "StreamStatusError";
  }
  toJSON() {
    return {
      name: this.name,
      status: this.status,
      message: this.message,
      stack: this.stack,
    };
  }
}

export class client {
  base_url: string;
  api_key: string;
  model: string;
  codex = false;
  timeout_ms = 5 * 60 * 1000;

  constructor(base_url: string, api_key: string, model: string) {
    this.base_url = base_url.replace(/\/+$/, "");
    this.api_key = api_key;
    this.model = model;
  }

  without_timeout(): client {
    const cp = new client(this.base_url, this.api_key, this.model);
    cp.codex = this.codex;
    cp.timeout_ms = 0;
    return cp;
  }

  chat(ctx: AbortSignal, req: chat_request): Effect.Effect<chat_response, client_error> {
    const self = this;
    return Effect.tryPromise({
      try: async () => {
        if (self.codex) {
          const result = await Effect.runPromise(Effect.either(self.chat_responses_once(ctx, req)));
          if (result._tag === "Left") throw result.left;
          return { choices: [{ message: result.right, finish_reason: "stop" }] };
        }
        if (req.model === "") req.model = self.model;
        req.messages = prepare_request_messages(req.messages, req.model);
        const body = marshal_chat_request(req);
        return with_request_abort_context(ctx, self.timeout_ms, async (signal) => {
        const resp = await fetch(`${self.base_url}/chat/completions`, {
          method: "POST",
          signal,
          headers: { "Content-Type": "application/json", Authorization: `Bearer ${self.api_key}` },
          body,
        });
        const raw = await resp.text();
        if (resp.status >= 400) {
          let api_err: chat_response | undefined;
          try {
            api_err = JSON.parse(raw) as chat_response;
          } catch {}
          const message = api_err?.error?.message?.trim() || raw.trim() || resp.statusText;
          throw new stream_status_error(resp.status, `opencode ${resp.status}: ${message}`, api_err?.error?.type);
        }
        const out = JSON.parse(raw) as chat_response;
        if (out.error?.message !== undefined && out.error.message !== "") {
          throw new provider_error(`opencode: ${out.error.message}`, { code: out.error.type });
        }
        if (out.choices.length === 0) throw new provider_error("opencode: empty response");
        for (const choice of out.choices) sanitize_embedded_thinking(choice.message);
        return out;
        });
      },
      catch: (cause) => client_error("chat", cause, ctx),
    });
  }

  chat_responses_once(ctx: AbortSignal, req: chat_request): Effect.Effect<message, client_error> {
    return codex_chat_responses_once(this, ctx, req);
  }

  chat_stream(ctx: AbortSignal, req: chat_request, cb: stream_callbacks): Effect.Effect<message, client_error> {
    return chat_stream_impl(this, ctx, req, cb);
  }

  chat_stream_retry(ctx: AbortSignal, req: chat_request, cb: stream_callbacks): Effect.Effect<message, client_error> {
    return chat_stream_retry_impl(this, ctx, req, cb);
  }
}

export const new_client = (base_url: string, api_key: string, model: string): client => new client(base_url, api_key, model);
export const new_codex_client = (base_url: string, access_token: string, model: string): client => {
  const c = new_client(base_url, access_token, model);
  c.codex = true;
  return c;
};
