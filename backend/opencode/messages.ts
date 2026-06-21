// PORT: mirrors backend/opencode/messages.go

import { blocks_content, content_blocks, string_content, type content_block, type message, type tool_call } from "./types";
import { normalize_messages } from "./thinking";
import { supports_images } from "./models";


export const tool_incomplete_msg = "Error: tool call was not completed";

export const strip_response_fields = (msgs: message[]): message[] => {
  if (msgs.length === 0) return msgs;
  return msgs.map((msg) => ({ ...msg, usage: undefined }));
};

export const prepare_request_messages = (msgs: message[], model: string): message[] =>
  strip_images_if_needed(
    repair_tool_messages(sanitize_tool_call_arguments(normalize_messages(strip_response_fields(msgs), model))),
    model,
  );

export const sanitize_tool_call_arguments = (msgs: message[]): message[] => {
  if (msgs.length === 0) return msgs;
  return msgs.map((msg) => {
    if (msg.role !== "assistant" || msg.tool_calls === undefined || msg.tool_calls.length === 0) return msg;
    return {
      ...msg,
      tool_calls: msg.tool_calls.map((tc) => ({ ...tc, function: { ...tc.function, arguments: valid_tool_arguments_json(tc.function.arguments) } })),
    };
  });
};

export const valid_tool_arguments_json = (raw: string): string => {
  raw = raw.trim();
  if (raw === "") return "{}";
  try {
    const obj = JSON.parse(raw) as unknown;
    if (obj === null || typeof obj !== "object" || Array.isArray(obj)) return "{}";
    return JSON.stringify(obj);
  } catch { return "{}"; }
};

const strip_images_if_needed = (msgs: message[], model: string): message[] => {
  if (supports_images(model)) return msgs;
  return msgs.map((msg) => {
    const blocks = content_blocks(msg);
    if (blocks === null || blocks.length === 0) return msg;
    const filtered: content_block[] = [];
    let image_count = 0;
    for (const b of blocks) {
      if (is_image_content_block(b)) image_count++;
      else filtered.push(b);
    }
    if (image_count === 0) return msg;
    const note = `[${image_count} image(s) omitted — current model does not support images.]`;
    if (filtered.length > 0 && filtered[0].type === "text") filtered[0].text = `${filtered[0].text ?? ""}\n${note}`;
    else filtered.unshift({ type: "text", text: note });
    const out = { ...msg };
    if (filtered.length === 0) out.content = new Uint8Array();
    else if (filtered.length === 1 && filtered[0].type === "text") out.content = string_content(filtered[0].text ?? "");
    else out.content = blocks_content(filtered);
    return out;
  });
};

const is_image_content_block = (b: content_block): boolean => b.type === "image_url" || b.type === "image";

export const repair_tool_messages = (msgs: message[]): message[] => {
  const out: message[] = [];
  for (let i = 0; i < msgs.length; i++) {
    const msg = { ...msgs[i] };
    // Orphan tool result with no preceding assistant tool_calls (e.g. the
    // issuing turn was dropped by trimming/compaction). Providers reject this
    // ("Messages with role 'tool' must be a response to a preceding message
    // with 'tool_calls'"), so drop it. Legitimate tool results are consumed in
    // the block right after their assistant tool_calls below.
    if (msg.role === "tool") continue;
    if (msg.role === "assistant" && msg.tool_calls !== undefined && msg.tool_calls.length > 0) {
      msg.tool_calls = msg.tool_calls.map((tc, j) => ({ ...tc, id: tc.id === "" ? `call_${i}_${j}` : tc.id }));
    }
    out.push(msg);
    if (msg.role !== "assistant" || msg.tool_calls === undefined || msg.tool_calls.length === 0) continue;
    const required: tool_call[] = [...msg.tool_calls];
    const valid_ids = new Set(required.map((tc) => tc.id));
    const answered = new Set<string>();
    i++;
    while (i < msgs.length && msgs[i].role === "tool") {
      const tm = { ...msgs[i] };
      let id = tm.tool_call_id;
      if (id === undefined || id === "") id = required[0].id;
      // Drop tool results whose id doesn't match this assistant's tool_calls
      // (mismatched orphan) or that we've already included (duplicate).
      if (!valid_ids.has(id) || answered.has(id)) {
        i++;
        continue;
      }
      tm.tool_call_id = id;
      out.push(tm);
      answered.add(id);
      i++;
    }
    i--;
    for (const tc of required) {
      if (answered.has(tc.id)) continue;
      out.push({ role: "tool", tool_call_id: tc.id, name: tc.function.name, content: string_content(tool_incomplete_msg) });
    }
  }
  return out;
};

export { normalize_messages };

/*
PORT STATUS
source path: backend/opencode/messages.go
source lines: 181
draft lines: 116
confidence: high
status: phase_b_compile
*/
