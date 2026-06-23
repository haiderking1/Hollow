// PORT: backend/opencode/client.go

import { Effect } from "effect";
import { prepare_request_messages } from "./messages";
import type { chat_request, chat_response, message } from "./types";
import { marshal_chat_request } from "./types";
import { sanitize_embedded_thinking } from "./think_tags";
import { chat_stream_impl, chat_stream_retry_impl, type stream_callbacks } from "./stream";
import { chat_responses_once as codex_chat_responses_once } from "./codex";

export type client_error = { readonly _tag: "ClientError"; readonly reason: string; readonly cause: unknown };
export const client_error = (reason: string, cause: unknown): client_error => ({ _tag: "ClientError", reason, cause });

export class stream_status_error extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
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
          const msg = await Effect.runPromise(self.chat_responses_once(ctx, req));
          return { choices: [{ message: msg, finish_reason: "stop" }] };
        }
        if (req.model === "") req.model = self.model;
        req.messages = prepare_request_messages(req.messages, req.model);
        const body = marshal_chat_request(req);
        const resp = await fetch(`${self.base_url}/chat/completions`, {
          method: "POST",
          signal: ctx,
          headers: { "Content-Type": "application/json", Authorization: `Bearer ${self.api_key}` },
          body,
        });
        const raw = await resp.text();
        if (resp.status >= 400) {
          try {
            const api_err = JSON.parse(raw) as chat_response;
            if (api_err.error?.message !== undefined && api_err.error.message !== "") throw new Error(`opencode ${resp.status}: ${api_err.error.message}`);
          } catch (err) {
            if (err instanceof Error && err.message.startsWith("opencode")) throw err;
          }
          throw new Error(`opencode ${resp.status}: ${raw.trim()}`);
        }
        const out = JSON.parse(raw) as chat_response;
        if (out.error?.message !== undefined && out.error.message !== "") throw new Error(`opencode: ${out.error.message}`);
        if (out.choices.length === 0) throw new Error("opencode: empty response");
        for (const choice of out.choices) sanitize_embedded_thinking(choice.message);
        return out;
      },
      catch: (cause) => client_error("chat", cause),
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

/*
PORT STATUS
source path: backend/opencode/client.go
source lines: 113
draft lines: 98
confidence: high
status: phase_b_compile
*/

