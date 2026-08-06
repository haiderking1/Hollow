import { blocks_content, content_blocks, string_content, content_string, type content_block, type message, type tool_call } from "./types";
import { normalize_messages, is_minimax_model } from "./thinking";
import { supports_images } from "./models";

export const CompactionSummaryPrefix =
  "The conversation history before this point was compacted into the following summary:\n\n<summary>\n";
export const CompactionSummarySuffix = "\n</summary>";
export const BranchSummaryPrefix =
  "The following is a summary of a branch that this conversation came back from:\n\n<summary>\n";
export const BranchSummarySuffix = "\n</summary>";

export const bash_execution_to_text = (msg: any): string => {
  let text = `Ran \`${msg.command || ""}\`\n`;
  if (msg.output) {
    text += `\`\`\`\n${msg.output}\n\`\`\``;
  } else {
    text += "(no output)";
  }
  if (msg.cancelled) {
    text += "\n\n(command cancelled)";
  } else if (msg.exitCode !== null && msg.exitCode !== undefined && msg.exitCode !== 0) {
    text += `\n\nCommand exited with code ${msg.exitCode}`;
  }
  if (msg.truncated && msg.fullOutputPath) {
    text += `\n\n[Output truncated. Full output: ${msg.fullOutputPath}]`;
  }
  return text;
};

export const convert_to_llm = (messages: message[]): message[] => {
  const out: message[] = [];
  for (const m of messages) {
    switch (m.role) {
      case "bashExecution": {
        const anyMsg = m as any;
        if (anyMsg.excludeFromContext) {
          break;
        }
        out.push({
          role: "user",
          content: string_content(bash_execution_to_text(m)),
        });
        break;
      }
      case "custom": {
        out.push({
          role: "user",
          content: m.content,
        });
        break;
      }
      case "compactionSummary": {
        const content = CompactionSummaryPrefix + content_string(m) + CompactionSummarySuffix;
        out.push({
          role: "user",
          content: string_content(content),
        });
        break;
      }
      case "branchSummary": {
        const content = BranchSummaryPrefix + content_string(m) + BranchSummarySuffix;
        out.push({
          role: "user",
          content: string_content(content),
        });
        break;
      }
      default:
        out.push(m);
        break;
    }
  }
  return out;
};

export const tool_incomplete_msg = "Error: tool call was not completed";

export const strip_response_fields = (msgs: message[]): message[] => {
  if (msgs.length === 0) return msgs;
  return msgs.map((msg) => ({ ...msg, usage: undefined }));
};

export const prepare_request_messages = (msgs: message[], model: string): message[] =>
  strip_images_if_needed(
    repair_tool_messages(
      sanitize_tool_call_arguments(
        strip_minimax_reasoning_details(normalize_messages(strip_response_fields(convert_to_llm(msgs)), model), model),
      ),
    ),
    model,
  );

/** MiniMax expects reasoning_details as structured objects, not strings — never round-trip our split copy. */
const strip_minimax_reasoning_details = (msgs: message[], model: string): message[] => {
  if (!is_minimax_model(model)) return msgs;
  return msgs.map((msg) => {
    if (msg.role !== "assistant") return msg;
    const next = { ...msg };
    delete next.reasoning_details;
    return next;
  });
};

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
    const note = `[${image_count} image(s) omitted — this session does not support images.]`;
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

