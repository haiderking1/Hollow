// PORT: mirrors backend/session/compaction_utils.go

import type { message, usage } from "../opencode/types";
import { string_content, content_string, content_blocks } from "../opencode/types";
import type { compaction_settings } from "../config/config";
import {
  type_message,
  type_compaction,
  type_branch_summary,
  type_custom_message,
  type file_entry,
} from "./types";

export const CompactionSummaryPrefix =
  "The conversation history before this point was compacted into the following summary:\n\n<summary>\n";
export const CompactionSummarySuffix = "\n</summary>";
export const BranchSummaryPrefix =
  "The following is a summary of a branch that this conversation came back from:\n\n<summary>\n";
export const BranchSummarySuffix = "\n</summary>";

export type file_operations = {
  read: Record<string, boolean>;
  written: Record<string, boolean>;
  edited: Record<string, boolean>;
};

export const new_file_ops = (): file_operations => ({
  read: {},
  written: {},
  edited: {},
});

export type compaction_details = {
  readFiles: string[];
  modifiedFiles: string[];
};

export const extract_file_ops_from_message = (
  msg: message,
  fileOps: file_operations,
): void => {
  if (msg.role !== "assistant") {
    return;
  }
  if (!msg.tool_calls) {
    return;
  }
  for (const tc of msg.tool_calls) {
    let args: any;
    try {
      args = JSON.parse(tc.function.arguments);
    } catch {
      continue;
    }
    if (!args || typeof args !== "object") {
      continue;
    }
    const pathVal = args["path"];
    if (typeof pathVal !== "string" || pathVal === "") {
      continue;
    }
    switch (tc.function.name) {
      case "read_file":
        fileOps.read[pathVal] = true;
        break;
      case "write_file":
        fileOps.written[pathVal] = true;
        break;
      case "edit_file":
        fileOps.edited[pathVal] = true;
        break;
    }
  }
};

export const extract_file_operations = (
  messages: message[],
  entries: file_entry[],
  prevCompactionIndex: number,
): file_operations => {
  const fileOps = new_file_ops();
  if (prevCompactionIndex >= 0) {
    const prevComp = entries[prevCompactionIndex];
    if (!prevComp.fromHook && prevComp.details) {
      const details = prevComp.details;
      if (details && typeof details === "object") {
        if (Array.isArray(details.readFiles)) {
          for (const f of details.readFiles) {
            if (typeof f === "string") {
              fileOps.read[f] = true;
            }
          }
        }
        if (Array.isArray(details.modifiedFiles)) {
          for (const f of details.modifiedFiles) {
            if (typeof f === "string") {
              fileOps.edited[f] = true;
            }
          }
        }
      }
    }
  }
  for (const msg of messages) {
    extract_file_ops_from_message(msg, fileOps);
  }
  return fileOps;
};

export const compute_file_lists = (fileOps: file_operations): [string[], string[]] => {
  const modified: Record<string, boolean> = {};
  for (const f of Object.keys(fileOps.edited)) {
    modified[f] = true;
  }
  for (const f of Object.keys(fileOps.written)) {
    modified[f] = true;
  }

  const readOnly: string[] = [];
  for (const f of Object.keys(fileOps.read)) {
    if (!modified[f]) {
      readOnly.push(f);
    }
  }
  readOnly.sort();

  const modifiedFiles = Object.keys(modified);
  modifiedFiles.sort();

  return [readOnly, modifiedFiles];
};

export const format_file_operations = (
  readFiles: string[],
  modifiedFiles: string[],
): string => {
  const sections: string[] = [];
  if (readFiles.length > 0) {
    sections.push(`<read-files>\n${readFiles.join("\n")}\n</read-files>`);
  }
  if (modifiedFiles.length > 0) {
    sections.push(`<modified-files>\n${modifiedFiles.join("\n")}\n</modified-files>`);
  }
  if (sections.length === 0) {
    return "";
  }
  return "\n\n" + sections.join("\n\n");
};

const safe_json_stringify = (value: unknown): string => {
  try {
    return JSON.stringify(value) ?? "undefined";
  } catch {
    return "[unserializable]";
  }
};

const truncate_for_summary = (text: string, maxChars: number): string => {
  if (text.length <= maxChars) {
    return text;
  }
  const truncatedChars = text.length - maxChars;
  return `${text.slice(0, maxChars)}\n\n[... ${truncatedChars} more characters truncated]`;
};

export const serialize_conversation = (messages: message[]): string => {
  const parts: string[] = [];

  for (const msg of messages) {
    if (msg.role === "user") {
      const content = content_string(msg);
      if (content !== "") {
        parts.push(`[User]: ${content}`);
      }
    } else if (msg.role === "assistant") {
      const textParts: string[] = [];
      const thinkingParts: string[] = [];
      const toolCalls: string[] = [];

      if (msg.reasoning_content !== undefined && msg.reasoning_content !== "") {
        thinkingParts.push(msg.reasoning_content);
      } else if (msg.reasoning_details !== undefined && msg.reasoning_details !== "") {
        thinkingParts.push(msg.reasoning_details);
      } else if (msg.reasoning !== undefined && msg.reasoning !== "") {
        thinkingParts.push(msg.reasoning);
      }

      const content = content_string(msg);
      if (content !== "") {
        textParts.push(content);
      }

      for (const tc of msg.tool_calls ?? []) {
        let args: any = null;
        try {
          args = JSON.parse(tc.function.arguments);
        } catch {}

        let argsStr = "";
        if (args && typeof args === "object" && !Array.isArray(args)) {
          const kv: string[] = [];
          const keys = Object.keys(args).sort();
          for (const k of keys) {
            kv.push(`${k}=${safe_json_stringify(args[k])}`);
          }
          argsStr = kv.join(", ");
        } else {
          argsStr = tc.function.arguments;
        }
        toolCalls.push(`${tc.function.name}(${argsStr})`);
      }

      if (thinkingParts.length > 0) {
        parts.push(`[Assistant thinking]: ${thinkingParts.join("\n")}`);
      }
      if (textParts.length > 0) {
        parts.push(`[Assistant]: ${textParts.join("\n")}`);
      }
      if (toolCalls.length > 0) {
        parts.push(`[Assistant tool calls]: ${toolCalls.join("; ")}`);
      }
    } else if (msg.role === "tool" || msg.role === "toolResult") {
      const content = content_string(msg);
      if (content !== "") {
        parts.push(`[Tool result]: ${truncate_for_summary(content, 2000)}`);
      }
    }
  }

  return parts.join("\n\n");
};

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

export const calculate_context_tokens = (u: usage): number => {
  if (u.totalTokens !== undefined && u.totalTokens > 0) {
    return u.totalTokens;
  }
  return (u.input ?? 0) + (u.output ?? 0) + (u.cacheRead ?? 0) + (u.cacheWrite ?? 0);
};

export const get_last_assistant_usage = (entries: file_entry[]): usage | null => {
  for (let i = entries.length - 1; i >= 0; i--) {
    const entry = entries[i];
    if (entry.type === type_message && entry.message && entry.message.role === "assistant") {
      if (entry.message.usage) {
        return entry.message.usage;
      }
    }
  }
  return null;
};

export type context_usage_estimate = {
  tokens: number;
  usageTokens: number;
  trailingTokens: number;
  lastUsageIndex: number;
};

const ESTIMATED_IMAGE_CHARS = 4800;

export const estimate_text_and_image_content_chars = (msg: message): number => {
  const blocks = content_blocks(msg);
  if (!blocks || blocks.length === 0) {
    return content_string(msg).length;
  }
  let chars = 0;
  for (const block of blocks) {
    if (block.type === "text" && block.text) {
      chars += block.text.length;
    } else if (block.type === "image" || block.type === "image_url") {
      chars += ESTIMATED_IMAGE_CHARS;
    }
  }
  return chars;
};

export const estimate_message_tokens = (msg: message): number => {
  let chars = 0;

  switch (msg.role) {
    case "user": {
      chars = estimate_text_and_image_content_chars(msg);
      return Math.ceil(chars / 4);
    }
    case "assistant": {
      const text = content_string(msg);
      chars += text.length;
      if (msg.reasoning_content !== undefined) {
        chars += msg.reasoning_content.length;
      } else if (msg.reasoning_details !== undefined) {
        chars += msg.reasoning_details.length;
      } else if (msg.reasoning !== undefined) {
        chars += msg.reasoning.length;
      }
      for (const tc of msg.tool_calls ?? []) {
        chars += tc.function.name.length + safe_json_stringify(tc.function.arguments).length;
      }
      return Math.ceil(chars / 4);
    }
    case "custom":
    case "tool":
    case "toolResult": {
      chars = estimate_text_and_image_content_chars(msg);
      return Math.ceil(chars / 4);
    }
    case "bashExecution": {
      const anyMsg = msg as any;
      const cmd = typeof anyMsg.command === "string" ? anyMsg.command : "";
      const out = typeof anyMsg.output === "string" ? anyMsg.output : "";
      chars = cmd.length + out.length;
      return Math.ceil(chars / 4);
    }
    case "branchSummary":
    case "compactionSummary": {
      chars = content_string(msg).length;
      return Math.ceil(chars / 4);
    }
  }

  return 0;
};

export const estimate_tokens = (entry: file_entry): number => {
  if (entry.type === type_compaction || entry.type === type_branch_summary) {
    return Math.ceil((entry.summary ?? "").length / 4);
  }
  if (entry.type !== type_message || !entry.message) {
    return 0;
  }
  return estimate_message_tokens(entry.message);
};

export const estimate_context_tokens = (messages: message[]): context_usage_estimate => {
  let lastUsageIdx = -1;
  let lastUsage: usage | undefined;
  for (let i = messages.length - 1; i >= 0; i--) {
    if (messages[i].role === "assistant" && messages[i].usage) {
      lastUsageIdx = i;
      lastUsage = messages[i].usage;
      break;
    }
  }

  if (lastUsageIdx === -1) {
    let estimated = 0;
    for (const msg of messages) {
      estimated += estimate_message_tokens(msg);
    }
    return {
      tokens: estimated,
      usageTokens: 0,
      trailingTokens: estimated,
      lastUsageIndex: -1,
    };
  }

  const usageTokens = calculate_context_tokens(lastUsage!);
  let trailingTokens = 0;
  for (let i = lastUsageIdx + 1; i < messages.length; i++) {
    trailingTokens += estimate_message_tokens(messages[i]);
  }

  return {
    tokens: usageTokens + trailingTokens,
    usageTokens,
    trailingTokens,
    lastUsageIndex: lastUsageIdx,
  };
};

export const should_compact = (
  contextTokens: number,
  contextWindow: number,
  settings: compaction_settings,
): boolean => {
  if (!settings.enabled) {
    return false;
  }
  return contextTokens > contextWindow - settings.reserve_tokens;
};

const find_valid_cut_points = (
  entries: file_entry[],
  startIndex: number,
  endIndex: number,
): number[] => {
  const cutPoints: number[] = [];
  for (let i = startIndex; i < endIndex; i++) {
    const entry = entries[i];
    if (entry.type === type_message && entry.message) {
      const role = entry.message.role;
      if (
        role === "user" ||
        role === "assistant" ||
        role === "bashExecution" ||
        role === "custom" ||
        role === "branchSummary" ||
        role === "compactionSummary"
      ) {
        cutPoints.push(i);
      }
    } else if (entry.type === type_branch_summary || entry.type === type_custom_message) {
      cutPoints.push(i);
    }
  }
  return cutPoints;
};

const find_turn_start_index = (
  entries: file_entry[],
  entryIndex: number,
  startIndex: number,
): number => {
  for (let i = entryIndex; i >= startIndex; i--) {
    const entry = entries[i];
    if (entry.type === type_branch_summary || entry.type === type_custom_message) {
      return i;
    }
    if (entry.type === type_message && entry.message) {
      const role = entry.message.role;
      if (role === "user" || role === "bashExecution") {
        return i;
      }
    }
  }
  return -1;
};

export type cut_point_result = {
  firstKeptEntryIndex: number;
  turnStartIndex: number;
  isSplitTurn: boolean;
};

export const find_cut_point = (
  entries: file_entry[],
  startIndex: number,
  endIndex: number,
  keepRecentTokens: number,
): cut_point_result => {
  const cutPoints = find_valid_cut_points(entries, startIndex, endIndex);
  if (cutPoints.length === 0) {
    return { firstKeptEntryIndex: startIndex, turnStartIndex: -1, isSplitTurn: false };
  }

  let accumulatedTokens = 0;
  let cutIndex = cutPoints[0];

  for (let i = endIndex - 1; i >= startIndex; i--) {
    const entry = entries[i];
    if (entry.type !== type_message) {
      continue;
    }
    accumulatedTokens += estimate_tokens(entry);
    if (accumulatedTokens >= keepRecentTokens) {
      for (const cp of cutPoints) {
        if (cp >= i) {
          cutIndex = cp;
          break;
        }
      }
      break;
    }
  }

  while (cutIndex > startIndex) {
    const prevEntry = entries[cutIndex - 1];
    if (prevEntry.type === type_compaction || prevEntry.type === type_message) {
      break;
    }
    cutIndex--;
  }

  const cutEntry = entries[cutIndex];
  const isUserMessage =
    cutEntry.type === type_message &&
    cutEntry.message &&
    cutEntry.message.role === "user";
  let turnStartIndex = -1;
  if (!isUserMessage) {
    turnStartIndex = find_turn_start_index(entries, cutIndex, startIndex);
  }

  return {
    firstKeptEntryIndex: cutIndex,
    turnStartIndex,
    isSplitTurn: !isUserMessage && turnStartIndex !== -1,
  };
};

export const get_latest_compaction_entry = (branch: file_entry[]): file_entry | null => {
  for (let i = branch.length - 1; i >= 0; i--) {
    if (branch[i].type === type_compaction) {
      return branch[i];
    }
  }
  return null;
};

/*
PORT STATUS
source path: backend/session/compaction_utils.go
source lines: 410
confidence: high
status: phase_b_compile
*/
