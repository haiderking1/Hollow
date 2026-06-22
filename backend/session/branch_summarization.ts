// PORT: mirrors backend/session/branch_summarization.go

import { Effect } from "effect";
import type { message } from "../opencode/types";
import { string_content, content_string } from "../opencode/types";
import type { client } from "../opencode/client";
import {
  type_message,
  type_compaction,
  type_branch_summary,
  type_custom_message,
  type file_entry,
} from "./types";
import { get_branch } from "./context";
import {
  new_file_ops,
  extract_file_ops_from_message,
  compute_file_lists,
  format_file_operations,
  convert_to_llm,
  serialize_conversation,
  estimate_message_tokens,
  type file_operations,
} from "./compaction_utils";
import { SummarizationSystemPrompt } from "./compaction";

export type branch_summary_result = {
  summary: string;
  readFiles: string[];
  modifiedFiles: string[];
  aborted: boolean;
  error: string;
};

export type branch_summary_details = {
  readFiles: string[];
  modifiedFiles: string[];
};

export type branch_preparation = {
  messages: message[];
  fileOps: file_operations;
  totalTokens: number;
};

export type collect_entries_result = {
  entries: file_entry[] | null;
  commonAncestorId: string | null;
};

export type generate_branch_summary_options = {
  client: client;
  customInstructions: string;
  replaceInstructions: boolean;
  reserveTokens: number;
  contextWindow: number;
};

export const collect_entries_for_branch_summary = (
  entries: file_entry[],
  oldLeafId: string | null | undefined,
  targetId: string,
): collect_entries_result => {
  if (oldLeafId === null || oldLeafId === undefined) {
    return { entries: null, commonAncestorId: null };
  }

  const oldBranch = get_branch(entries, oldLeafId);
  const oldPathSet = new Set<string>();
  for (const e of oldBranch) {
    oldPathSet.add(e.id);
  }

  const targetBranch = get_branch(entries, targetId);
  let commonAncestorId: string | null = null;
  for (let i = targetBranch.length - 1; i >= 0; i--) {
    if (oldPathSet.has(targetBranch[i].id)) {
      commonAncestorId = targetBranch[i].id;
      break;
    }
  }

  const collected: file_entry[] = [];
  const byId = new Map<string, file_entry>();
  for (const e of entries) {
    if (e.id !== "") {
      byId.set(e.id, e);
    }
  }

  let current: string | null | undefined = oldLeafId;
  while (current !== null && current !== undefined) {
    if (commonAncestorId !== null && current === commonAncestorId) {
      break;
    }
    const entry = byId.get(current);
    if (!entry) {
      break;
    }
    collected.push(entry);
    current = entry.parentId;
  }

  collected.reverse();

  return {
    entries: collected,
    commonAncestorId,
  };
};

const get_message_from_entry = (entry: file_entry): message | null => {
  switch (entry.type) {
    case type_message:
      if (!entry.message || entry.message.role === "tool") {
        return null;
      }
      return entry.message;
    case type_custom_message:
      if (entry.content !== undefined && entry.content !== null) {
        let contentStr = "";
        if (typeof entry.content === "string") {
          contentStr = entry.content;
        } else {
          try {
            contentStr = JSON.stringify(entry.content);
          } catch {}
        }
        return {
          role: "user",
          content: string_content(contentStr),
        };
      }
      return null;
    case type_branch_summary:
      if (entry.summary !== undefined && entry.summary !== "") {
        return {
          role: "branchSummary",
          content: string_content(entry.summary),
          tool_call_id: entry.fromId || "",
        };
      }
      return null;
    case type_compaction:
      if (entry.summary !== undefined && entry.summary !== "") {
        return {
          role: "compactionSummary",
          content: string_content(entry.summary),
        };
      }
      return null;
  }
  return null;
};

const extract_file_ops_from_details = (
  details: any,
  fileOps: file_operations,
): void => {
  if (details === undefined || details === null) {
    return;
  }
  if (typeof details === "object") {
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
};

export const prepare_branch_entries = (
  entries: file_entry[],
  tokenBudget: number,
): branch_preparation => {
  const messages: message[] = [];
  const fileOps = new_file_ops();
  let totalTokens = 0;

  for (const entry of entries) {
    if (entry.type === type_branch_summary && !entry.fromHook && entry.details) {
      extract_file_ops_from_details(entry.details, fileOps);
    }
  }

  for (let i = entries.length - 1; i >= 0; i--) {
    const entry = entries[i];
    const msg = get_message_from_entry(entry);
    if (!msg) {
      continue;
    }

    extract_file_ops_from_message(msg, fileOps);
    const tokens = estimate_message_tokens(msg);

    if (tokenBudget > 0 && totalTokens + tokens > tokenBudget) {
      if (entry.type === type_compaction || entry.type === type_branch_summary) {
        if (totalTokens < tokenBudget * 0.9) {
          messages.unshift(msg);
          totalTokens += tokens;
        }
      }
      break;
    }

    messages.unshift(msg);
    totalTokens += tokens;
  }

  return {
    messages,
    fileOps,
    totalTokens,
  };
};

export const BranchSummaryPreamble =
  "The user explored a different conversation branch before returning here.\nSummary of that exploration:\n\n";

export const BranchSummaryPrompt =
  `Create a structured summary of this conversation branch for context when returning later.

Use this EXACT format:

## Goal
[What was the user trying to accomplish in this branch?]

## Constraints & Preferences
- [Any constraints, preferences, or requirements mentioned]
- [Or "(none)" if none were mentioned]

## Progress
### Done
- [x] [Completed tasks/changes]

### In Progress
- [ ] [Work that was started but not finished]

### Blocked
- [Issues preventing progress, if any]

## Key Decisions
- **[Decision]**: [Brief rationale]

## Next Steps
1. [What should happen next to continue this work]

Keep each section concise. Preserve exact file paths, function names, and error messages.`;

export const generate_branch_summary = (
  ctx: AbortSignal,
  entries: file_entry[],
  options: generate_branch_summary_options,
): Effect.Effect<branch_summary_result, Error> => {
  let reserveTokens = options.reserveTokens;
  if (reserveTokens <= 0) {
    reserveTokens = 16384;
  }

  let contextWindow = options.contextWindow;
  if (contextWindow <= 0) {
    contextWindow = 128000;
  }
  const tokenBudget = contextWindow - reserveTokens;

  const prep = prepare_branch_entries(entries, tokenBudget);
  if (prep.messages.length === 0) {
    return Effect.succeed({
      summary: "No content to summarize",
      readFiles: [],
      modifiedFiles: [],
      aborted: false,
      error: "",
    });
  }

  const llmMessages = convert_to_llm(prep.messages);
  const conversationText = serialize_conversation(llmMessages);

  let instructions = "";
  if (options.replaceInstructions && options.customInstructions !== "") {
    instructions = options.customInstructions;
  } else if (options.customInstructions !== "") {
    instructions = `${BranchSummaryPrompt}\n\nAdditional focus: ${options.customInstructions}`;
  } else {
    instructions = BranchSummaryPrompt;
  }

  const promptText = `<conversation>\n${conversationText}\n</conversation>\n\n${instructions}`;

  const req = {
    model: "",
    messages: [
      { role: "system", content: string_content(SummarizationSystemPrompt) },
      { role: "user", content: string_content(promptText) },
    ],
  };

  return options.client.chat(ctx, req).pipe(
    Effect.flatMap((resp) => {
      if (!resp.choices || resp.choices.length === 0) {
        return Effect.fail(new Error("empty choices from chat"));
      }
      let summary = content_string(resp.choices[0].message);
      summary = BranchSummaryPreamble + summary;

      const [readFiles, modifiedFiles] = compute_file_lists(prep.fileOps);
      summary += format_file_operations(readFiles, modifiedFiles);

      return Effect.succeed({
        summary,
        readFiles,
        modifiedFiles,
        aborted: false,
        error: "",
      });
    }),
    Effect.mapError((err) => {
      if (err instanceof Error) return err;
      return new Error(err.reason || String(err));
    }),
  );
};

/*
PORT STATUS
source path: backend/session/branch_summarization.go
source lines: 305
confidence: high
status: phase_b_compile
*/
