// PORT: backend/session/types.go

import type { message } from "../opencode/types";

export type entry_type = string;

export const type_session: entry_type = "session";
export const type_message: entry_type = "message";
export const type_thinking_level_change: entry_type = "thinking_level_change";
export const type_model_change: entry_type = "model_change";
export const type_compaction: entry_type = "compaction";
export const type_branch_summary: entry_type = "branch_summary";
export const type_custom: entry_type = "custom";
export const type_custom_message: entry_type = "custom_message";
export const type_label: entry_type = "label";
export const type_session_info: entry_type = "session_info";
// TypeSystemPrompt stores the session's cached system prompt so resumed
// sessions replay the byte-identical prompt (prefix-cache invariant).
export const type_system_prompt: entry_type = "system_prompt";

export type session_entry = {
  type: entry_type;
  id: string;
  parentId?: string | null;
  timestamp: string;

  // message fields
  message?: message | null;
  toolDetails?: string;

  // thinking_level_change
  thinkingLevel?: string;

  // model_change
  provider?: string;
  modelId?: string;

  // compaction
  summary?: string;
  firstKeptEntryId?: string;
  tokensBefore?: number;
  details?: any;
  fromHook?: boolean;

  // branch_summary
  fromId?: string;

  // custom, custom_message
  customType?: string;
  data?: any;
  content?: any;
  display?: boolean | null;

  // label
  targetId?: string;
  label?: string;

  // session_info
  name?: string;
};

export type file_entry = session_entry & {
  // Header fields (for type: "session")
  version?: number;
  cwd?: string;
};

export type header = {
  type: string;
  version?: number;
  id: string;
  timestamp: string;
  cwd: string;
};

export type message_entry = {
  type: string;
  id: string;
  parentId?: string | null;
  timestamp: string;
  message: message;
};

export type chat_image = {
  url: string; // data URL
  mime_type: string;
  width: number;
  height: number;
};

// ChatLine is a TUI-friendly view of a persisted message or compaction summary.
export type chat_line = {
  role: string;
  text: string;
  thinking: string;
  tool_name: string;
  tool_args: string;
  tool_result: string;
  tool_details: string;
  tool_error: boolean;
  tokens_before: number;
  images: chat_image[];
};

export const now_iso = (): string => {
  return new Date().toISOString();
};

export const file_timestamp = (t: Date): string => {
  return t.toISOString().replace(/:/g, "-").replace(/\./g, "-");
};

// Info summarizes a session JSONL file for listing and resume pickers.
export type info = {
  path: string;
  id: string;
  cwd: string;
  modified: Date;
  created: Date;
  message_count: number;
  first_message: string;
};

/*
PORT STATUS
source path: backend/session/types.go
source lines: 121
draft lines: 121
confidence: high
status: phase_b_compile
*/
