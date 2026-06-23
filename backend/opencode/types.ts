// PORT: backend/opencode/types.go

export type json_raw_message = Uint8Array;

export type stream_options = { include_usage: boolean };
export type chat_request = {
  model: string;
  messages: message[];
  tools?: tool[];
  stream?: boolean;
  thinking?: unknown;
  reasoning_effort?: string;
  stream_options?: stream_options;
};
export type chat_response = { choices: choice[]; error?: api_error };
export type api_error = { message: string; type: string };
export type choice = { message: message; finish_reason: string };
export type usage = { input: number; output: number; totalTokens?: number; cacheRead?: number; cacheWrite?: number };
export type content_block = { type: string; text?: string; image_url?: content_image_url };
export type content_image_url = { url: string };
export type message = {
  role: string;
  content: json_raw_message;
  reasoning_content?: string;
  reasoning_details?: string;
  reasoning?: string;
  tool_calls?: tool_call[];
  tool_call_id?: string;
  name?: string;
  usage?: usage;
};

export const get_reasoning = (m: message): string => {
  if (m.reasoning_content !== undefined) return m.reasoning_content;
  if (m.reasoning_details !== undefined) return m.reasoning_details;
  if (m.reasoning !== undefined) return m.reasoning;
  return "";
};

export type tool = { type: string; function: tool_function };
export type tool_function = { name: string; description: string; parameters: json_raw_message };
export type tool_call = { id: string; type: string; function: tool_call_function };
export type tool_call_function = { name: string; arguments: string };

const enc = new TextEncoder();
const dec = new TextDecoder();

export const message_content_json = (m: message): unknown => {
  if (!m?.content) return "";
  const buf = ensure_buffer(m.content);
  if (!buf || buf.length === 0) return "";
  try {
    const decoded = dec.decode(buf);
    if (decoded === "null") return "";
    const parsed = JSON.parse(decoded) as unknown;
    if (typeof parsed === "string") return parsed;
    if (Array.isArray(parsed)) return parsed;
    if (parsed !== null && typeof parsed === "object") {
      return JSON.stringify(parsed);
    }
    return "";
  } catch {
    return "";
  }
};

/** Wire-format message for /chat/completions — embeds content JSON like Go json.RawMessage. */
export const message_for_api = (m: message): Record<string, unknown> => {
  const out: Record<string, unknown> = {
    role: m.role,
    content: message_content_json(m),
  };
  if (m.reasoning_content !== undefined && m.reasoning_content !== "") {
    out.reasoning_content = m.reasoning_content;
  }
  if (m.reasoning_details !== undefined && m.reasoning_details !== "") {
    out.reasoning_details = m.reasoning_details;
  }
  if (m.reasoning !== undefined && m.reasoning !== "") {
    out.reasoning = m.reasoning;
  }
  if (m.tool_calls !== undefined && m.tool_calls.length > 0) {
    out.tool_calls = m.tool_calls;
  }
  if (m.tool_call_id !== undefined && m.tool_call_id !== "") {
    out.tool_call_id = m.tool_call_id;
  }
  if (m.name !== undefined && m.name !== "") {
    out.name = m.name;
  }
  return out;
};

const json_raw_for_api = (raw: json_raw_message | undefined): unknown => {
  if (raw === undefined) return {};
  const buf = ensure_buffer(raw);
  if (!buf || buf.length === 0) return {};
  try {
    return JSON.parse(dec.decode(buf)) as unknown;
  } catch {
    return {};
  }
};

/** JSON body for chat/completions — avoids serializing Uint8Array content as numeric objects. */
export const marshal_chat_request = (req: chat_request): string =>
  JSON.stringify({
    ...req,
    messages: req.messages.map(message_for_api),
    tools: req.tools?.map((t) => ({
      ...t,
      function: {
        ...t.function,
        parameters: json_raw_for_api(t.function.parameters),
      },
    })),
  });

export const string_content = (s: string): json_raw_message => enc.encode(JSON.stringify(s));
export const blocks_content = (blocks: content_block[]): json_raw_message => enc.encode(JSON.stringify(blocks));

const ensure_buffer = (val: any): Uint8Array | null => {
  if (!val) return null;
  if (val instanceof Uint8Array || Buffer.isBuffer(val)) {
    return val;
  }
  if (typeof val === "object" && Array.isArray(val.data)) {
    return new Uint8Array(val.data);
  }
  if (typeof val === "object" && val.type === "Buffer" && Array.isArray(val.data)) {
    return new Uint8Array(val.data);
  }
  if (Array.isArray(val)) {
    return new Uint8Array(val);
  }
  if (typeof val === "object" && val !== null && !Array.isArray(val)) {
    const keys = Object.keys(val);
    if (keys.length > 0 && keys.every((k) => /^\d+$/.test(k))) {
      const max = Math.max(...keys.map(Number));
      const out = new Uint8Array(max + 1);
      for (const k of keys) out[Number(k)] = (val as Record<string, number>)[k]!;
      return out;
    }
  }
  if (typeof val === "string") {
    return enc.encode(val);
  }
  return null;
};

export const content_string = (m: message): string => {
  if (!m || !m.content) return "";
  const buf = ensure_buffer(m.content);
  if (!buf || buf.length === 0) return "";
  try {
    const decoded = dec.decode(buf);
    if (decoded === "null") return "";
    try {
      const s = JSON.parse(decoded) as unknown;
      if (typeof s === "string") return s;
    } catch {}
    try {
      const blocks = JSON.parse(decoded) as unknown;
      if (Array.isArray(blocks)) {
        const parts: string[] = [];
        for (const b of blocks as content_block[]) {
          if (b.type === "text" && b.text !== undefined && b.text !== "") parts.push(b.text);
        }
        return parts.join("\n");
      }
    } catch {}
    return decoded;
  } catch {
    return "";
  }
};

export const content_blocks = (m: message): content_block[] | null => {
  if (!m || !m.content) return null;
  const buf = ensure_buffer(m.content);
  if (!buf || buf.length === 0) return null;
  try {
    const decoded = dec.decode(buf);
    if (decoded === "null") return null;
    try {
      const s = JSON.parse(decoded) as unknown;
      if (typeof s === "string") return [{ type: "text", text: s }];
    } catch {}
    try {
      const parsed = JSON.parse(decoded) as unknown;
      if (Array.isArray(parsed)) return parsed as content_block[];
    } catch {}
    return null;
  } catch {
    return null;
  }
};

/*
PORT STATUS
source path: backend/opencode/types.go
source lines: 160
draft lines: 88
confidence: high
status: phase_a_draft
todos:
  - wire ThinkingParams when its Go file is ported
notes:
  - json.RawMessage mapped to Uint8Array with JSON encode/decode helpers.
*/
