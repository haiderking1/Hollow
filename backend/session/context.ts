// PORT: backend/session/context.go

import {
  type_session,
  type_message,
  type_thinking_level_change,
  type_model_change,
  type_compaction,
  type_custom_message,
  type_branch_summary,
  type file_entry,
} from "./types";
import { type message, string_content } from "../opencode/types";

// SessionContext matches Flame's resolved context.
export type session_context = {
  messages: message[] | null;
  thinking_level: string;
  model: model_info | null;
};

export type model_info = {
  provider: string;
  model_id: string;
};

// BuildSessionContext resolves the messages and settings on the branch of leafID.
// If leafID is nil, it uses the last entry.
export const build_session_context = (
  entries: file_entry[],
  leaf_id?: string | null,
): session_context => {
  // Build map by ID
  const by_id = new Map<string, file_entry>();
  for (const entry of entries) {
    if (entry.id !== "") {
      by_id.set(entry.id, entry);
    }
  }

  let leaf: file_entry | null = null;
  if (leaf_id !== undefined && leaf_id !== null) {
    const val = by_id.get(leaf_id);
    if (val !== undefined) {
      leaf = val;
    }
  } else if (entries.length > 0) {
    // Default to the last entry
    for (let i = entries.length - 1; i >= 0; i--) {
      if (entries[i].type !== type_session) {
        leaf = entries[i];
        break;
      }
    }
  }

  if (leaf === null) {
    return { messages: null, thinking_level: "off", model: null };
  }

  // Walk from leaf to root to collect active branch path
  const path_list: file_entry[] = [];
  let current: file_entry | null = leaf;
  while (current !== null) {
    path_list.unshift(current);
    if (current.parentId !== undefined && current.parentId !== null) {
      const val = by_id.get(current.parentId);
      current = val !== undefined ? val : null;
    } else {
      current = null;
    }
  }

  // Resolve active settings
  let thinking_level = "off";
  let model: model_info | null = null;
  let compaction: file_entry | null = null;

  for (const entry of path_list) {
    switch (entry.type) {
      case type_thinking_level_change:
        if (entry.thinkingLevel !== undefined) {
          thinking_level = entry.thinkingLevel;
        }
        break;
      case type_model_change:
        model = {
          provider: entry.provider || "",
          model_id: entry.modelId || "",
        };
        break;
      case type_message:
        if (entry.message !== undefined && entry.message !== null && entry.message.role === "assistant") {
          // We can derive model/provider from assistant if available in Message
        }
        break;
      case type_compaction:
        compaction = entry;
        break;
    }
  }

  const messages: message[] = [];

  const append_message = (entry: file_entry) => {
    switch (entry.type) {
      case type_message:
        if (entry.message !== undefined && entry.message !== null) {
          messages.push(entry.message);
        }
        break;
      case type_custom_message:
        if (entry.content !== undefined && entry.content !== null) {
          let content_str = "";
          if (typeof entry.content === "string") {
            content_str = entry.content;
          }
          messages.push({
            role: "user",
            content: string_content(content_str),
          });
        }
        break;
      case type_branch_summary:
        if (entry.summary !== undefined && entry.summary !== "") {
          messages.push({
            role: "branchSummary",
            content: string_content(entry.summary),
            tool_call_id: entry.fromId || "", // Reuse ToolCallID or other fields if needed, but Content is key
          });
        }
        break;
    }
  };

  if (compaction !== null) {
    // Emit summary first
    messages.push({
      role: "compactionSummary",
      content: string_content(compaction.summary || ""),
    });

    // Find compaction index in path
    let compaction_idx = -1;
    for (let i = 0; i < path_list.length; i++) {
      if (path_list[i].type === type_compaction && path_list[i].id === compaction.id) {
        compaction_idx = i;
        break;
      }
    }

    // Emit kept messages (before compaction, starting from firstKeptEntryId)
    let found_first_kept = false;
    for (let i = 0; i < compaction_idx; i++) {
      const entry = path_list[i];
      if (entry.id === compaction.firstKeptEntryId) {
        found_first_kept = true;
      }
      if (found_first_kept) {
        append_message(entry);
      }
    }

    // Emit messages after compaction
    for (let i = compaction_idx + 1; i < path_list.length; i++) {
      const entry = path_list[i];
      append_message(entry);
    }
  } else {
    // No compaction - emit all messages
    for (const entry of path_list) {
      append_message(entry);
    }
  }

  return {
    messages,
    thinking_level,
    model,
  };
};

// GetBranch returns all entries from root to leafID in path order.
export const get_branch = (entries: file_entry[], leaf_id?: string | null): file_entry[] => {
  const by_id = new Map<string, file_entry>();
  for (const entry of entries) {
    if (entry.id !== "") {
      by_id.set(entry.id, entry);
    }
  }

  let leaf: file_entry | null = null;
  if (leaf_id !== undefined && leaf_id !== null) {
    const val = by_id.get(leaf_id);
    if (val !== undefined) {
      leaf = val;
    }
  } else if (entries.length > 0) {
    for (let i = entries.length - 1; i >= 0; i--) {
      if (entries[i].type !== type_session) {
        leaf = entries[i];
        break;
      }
    }
  }

  if (leaf === null) {
    return [];
  }

  const path_list: file_entry[] = [];
  let current: file_entry | null = leaf;
  while (current !== null) {
    path_list.unshift(current);
    if (current.parentId !== undefined && current.parentId !== null) {
      const val = by_id.get(current.parentId);
      current = val !== undefined ? val : null;
    } else {
      current = null;
    }
  }

  return path_list;
};

/*
PORT STATUS
source path: backend/session/context.go
source lines: 206
draft lines: 213
confidence: high
status: phase_b_compile
*/
