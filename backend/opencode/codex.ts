// PORT: backend/opencode/codex.go

import { Effect } from "effect";
import { codex_cloudflare_headers } from "../auth/codex_headers";
import { client, client_error } from "./client";
import { prepare_request_messages } from "./messages";
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

// OpenAI Responses API Types
export type responses_reasoning = {
  effort?: string;
  summary?: string;
};

export type responses_request = {
  model: string;
  instructions: string;
  input: unknown;
  tools?: unknown;
  store: boolean;
  stream?: boolean;
  reasoning?: responses_reasoning;
};

export type responses_error = {
  code: string;
  message: string;
};

export type responses_usage = {
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
};

export type responses_item = {
  type: string;
  role?: string;
  status?: string;
  name?: string;
  call_id?: string;
  arguments?: string;
  id?: string;
  content?: {
    type: string;
    text: string;
  }[];
  summary?: {
    text: string;
  }[];
};

export type responses_response = {
  status: string;
  output: responses_item[];
  output_text?: string;
  error?: responses_error;
  usage?: responses_usage;
};

export const codex_request_headers = (access_token: string): Record<string, string> => {
  return codex_cloudflare_headers(access_token);
};

export const responses_url = (c: client): string => {
  const base = c.base_url.replace(/\/+$/, "");
  if (base.endsWith("/responses")) {
    return base;
  }
  return base + "/responses";
};

export const split_instructions_messages = (msgs: message[]): [string, message[]] => {
  const parts: string[] = [];
  const rest: message[] = [];
  for (const msg of msgs) {
    if (msg.role === "system" || msg.role === "developer") {
      const text = content_string(msg).trim();
      if (text !== "") {
        parts.push(text);
      }
    } else {
      rest.push(msg);
    }
  }
  return [parts.join("\n\n"), rest];
};

export const reasoning_from_chat_request = (req: chat_request): responses_reasoning | undefined => {
  if (req.thinking !== undefined && req.thinking !== null && (req.thinking as { type?: string }).type === "disabled") {
    return undefined;
  }
  let effort = req.reasoning_effort ?? "";
  if (effort === "") {
    return undefined;
  }
  if (effort === "max") {
    effort = "xhigh";
  }
  return { effort, summary: "auto" };
};

export const messages_to_responses_input = (msgs: message[]): unknown[] => {
  const items: unknown[] = [];
  for (const msg of msgs) {
    switch (msg.role) {
      case "system":
        continue;
      case "user": {
        const blocks = content_blocks(msg);
        let has_image = false;
        if (blocks !== null) {
          for (const b of blocks) {
            if (b.type === "image_url") {
              has_image = true;
              break;
            }
          }
        }
        if (has_image && blocks !== null) {
          const content: unknown[] = [];
          for (const b of blocks) {
            switch (b.type) {
              case "text":
                if (b.text !== undefined && b.text !== "") {
                  content.push({ type: "input_text", text: b.text });
                }
                break;
              case "image_url":
                if (b.image_url !== undefined && b.image_url !== null) {
                  content.push({
                    type: "input_image",
                    image_url: b.image_url.url,
                    detail: "auto",
                  });
                }
                break;
            }
          }
          items.push({ role: "user", content });
        } else {
          const text = content_string(msg);
          items.push({ role: "user", content: text });
        }
        break;
      }
      case "assistant": {
        const text = content_string(msg);
        if (text !== "") {
          items.push({
            role: "assistant",
            content: [{ type: "output_text", text }],
          });
        }
        if (msg.tool_calls !== undefined) {
          for (const tc of msg.tool_calls) {
            let call_id = tc.id;
            if (call_id === "") {
              call_id = `call_${items.length}`;
            }
            items.push({
              type: "function_call",
              call_id,
              name: tc.function.name,
              arguments: tc.function.arguments,
            });
          }
        }
        break;
      }
      case "tool": {
        const call_id = msg.tool_call_id;
        if (call_id === undefined || call_id === "") {
          continue;
        }
        const blocks = content_blocks(msg);
        let has_image = false;
        if (blocks !== null) {
          for (const b of blocks) {
            if (b.type === "image_url") {
              has_image = true;
              break;
            }
          }
        }
        if (has_image && blocks !== null) {
          const output_items: unknown[] = [];
          for (const b of blocks) {
            if (b.type === "image_url" && b.image_url !== undefined && b.image_url !== null) {
              output_items.push({
                type: "input_image",
                image_url: b.image_url.url,
              });
            } else {
              output_items.push({
                type: "input_text",
                text: b.text ?? "",
              });
            }
          }
          items.push({
            type: "function_call_output",
            call_id,
            output: output_items,
          });
        } else {
          items.push({
            type: "function_call_output",
            call_id,
            output: content_string(msg),
          });
        }
        break;
      }
    }
  }
  return items;
};

export const chat_tools_to_responses = (tools?: tool[]): unknown => {
  if (tools === undefined || tools.length === 0) {
    return undefined;
  }
  const out: unknown[] = [];
  for (const t of tools) {
    if (t.function.name === "") {
      continue;
    }
    let params: unknown = { type: "object", properties: {} };
    if (t.function.parameters !== undefined && t.function.parameters.length > 0) {
      try {
        params = JSON.parse(new TextDecoder().decode(t.function.parameters));
      } catch {}
    }
    out.push({
      type: "function",
      name: t.function.name,
      description: t.function.description,
      strict: false,
      parameters: params,
    });
  }
  if (out.length === 0) {
    return undefined;
  }
  return out;
};

export const build_responses_request = (c: client, req: chat_request): responses_request => {
  if (req.model === "") {
    req.model = c.model;
  }
  const msgs = prepare_request_messages(req.messages, req.model);
  let [instructions, conv_msgs] = split_instructions_messages(msgs);
  if (instructions.trim() === "") {
    instructions = "You are a helpful coding assistant.";
  }
  const input = messages_to_responses_input(conv_msgs);
  const tools = chat_tools_to_responses(req.tools);

  return {
    model: req.model,
    instructions,
    input,
    tools,
    store: false,
    reasoning: reasoning_from_chat_request(req),
  };
};

export const parse_responses_message = (resp: responses_response): message => {
  const status = (resp.status ?? "").toLowerCase().trim();
  if (status === "failed" || status === "cancelled") {
    let msg = "codex request failed";
    if (resp.error?.message !== undefined && resp.error.message !== "") {
      msg = resp.error.message;
    }
    throw new Error(`codex: ${msg}`);
  }

  let usage_data: usage | undefined = undefined;
  if (resp.usage !== undefined && resp.usage !== null) {
    usage_data = {
      input: resp.usage.input_tokens,
      output: resp.usage.output_tokens,
      totalTokens: resp.usage.total_tokens,
    };
  }

  const msg: message = {
    role: "assistant",
    content: new Uint8Array(),
    usage: usage_data,
  };

  const output = resp.output ?? [];
  if (output.length === 0 && resp.output_text !== undefined && resp.output_text.trim() !== "") {
    msg.content = string_content(resp.output_text.trim());
    return msg;
  }
  if (output.length === 0) {
    throw new Error("codex: empty response");
  }

  const text_parts: string[] = [];
  const reasoning_parts: string[] = [];
  for (const item of output) {
    switch (item.type) {
      case "message":
        if (item.content !== undefined) {
          for (const part of item.content) {
            if (part.type === "output_text" || part.type === "text") {
              if (part.text !== "") {
                text_parts.push(part.text);
              }
            }
          }
        }
        break;
      case "reasoning":
        if (item.summary !== undefined) {
          for (const part of item.summary) {
            if (part.text !== "") {
              reasoning_parts.push(part.text);
            }
          }
        }
        break;
      case "function_call": {
        if (item.status !== undefined && item.status !== "" && item.status !== "completed") {
          continue;
        }
        let call_id = item.call_id ?? "";
        if (call_id === "" && item.id !== undefined && item.id.startsWith("fc_")) {
          call_id = "call_" + item.id.slice("fc_".length);
        }
        if (call_id === "") {
          const count = msg.tool_calls?.length ?? 0;
          call_id = `call_${count}`;
        }
        let args = item.arguments ?? "";
        if (args === "") {
          args = "{}";
        }
        if (msg.tool_calls === undefined) {
          msg.tool_calls = [];
        }
        msg.tool_calls.push({
          id: call_id,
          type: "function",
          function: {
            name: item.name ?? "",
            arguments: args,
          },
        });
        break;
      }
    }
  }

  if (text_parts.length > 0) {
    msg.content = string_content(text_parts.join(""));
  }
  if (reasoning_parts.length > 0) {
    msg.reasoning_content = reasoning_parts.join("\n");
  }

  if (msg.content.length === 0 && (msg.tool_calls === undefined || msg.tool_calls.length === 0)) {
    throw new Error("codex: no assistant output");
  }

  return msg;
};

export const chat_responses_once = (
  c: client,
  ctx: AbortSignal,
  req: chat_request
): Effect.Effect<message, client_error> => {
  return Effect.tryPromise({
    try: async () => {
      const body_req = build_responses_request(c, req);
      const body = JSON.stringify(body_req);

      const headers: Record<string, string> = {
        "Content-Type": "application/json",
        Authorization: `Bearer ${c.api_key}`,
        ...codex_request_headers(c.api_key),
      };

      const resp = await fetch(responses_url(c), {
        method: "POST",
        signal: ctx,
        headers,
        body,
      });

      const raw = await resp.text();
      if (resp.status >= 400) {
        throw new Error(`codex ${resp.status}: ${raw.trim()}`);
      }

      const out = JSON.parse(raw) as responses_response;
      return parse_responses_message(out);
    },
    catch: (cause) => client_error("chat_responses_once", cause),
  });
};

/*
PORT STATUS
source path: backend/opencode/codex.go
source lines: 398
draft lines: 382
confidence: high
status: phase_b_compile
*/
