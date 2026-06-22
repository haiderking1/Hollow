// PORT: mirrors backend/session/compaction.go

import { Effect } from "effect";
import type { message } from "../opencode/types";
import { string_content, content_string } from "../opencode/types";
import type { client } from "../opencode/client";
import type { compaction_settings } from "../config/config";
import type { branch_summary_result } from "./branch_summarization";
import {
  type_message,
  type_compaction,
  type_branch_summary,
  type_custom_message,
  type file_entry,
} from "./types";
import { build_session_context } from "./context";
import type { file_operations, compaction_details } from "./compaction_utils";
import {
  extract_file_ops_from_message,
  compute_file_lists,
  format_file_operations,
  convert_to_llm,
  serialize_conversation,
  estimate_context_tokens,
  find_cut_point,
  extract_file_operations,
} from "./compaction_utils";

export const SummarizationSystemPrompt =
  `You are a context summarization assistant. Your task is to read a conversation between a user and an AI coding assistant, then produce a structured summary following the exact format specified.

Do NOT continue the conversation. Do NOT respond to any questions in the conversation. ONLY output the structured summary.`;

export const SummarizationPrompt =
  `The messages above are a conversation to summarize. Create a structured context checkpoint summary that another LLM will use to continue the work.

Use this EXACT format:

## Goal
[What is the user trying to accomplish? Can be multiple items if the session covers different tasks.]

## Constraints & Preferences
- [Any constraints, preferences, or requirements mentioned by user]
- [Or "(none)" if none were mentioned]

## Progress
### Done
- [x] [Completed tasks/changes]

### In Progress
- [ ] [Current work]

### Blocked
- [Issues preventing progress, if any]

## Key Decisions
- **[Decision]**: [Brief rationale]

## Next Steps
1. [Ordered list of what should happen next]

## Critical Context
- [Any data, examples, or references needed to continue]
- [Or "(none)" if not applicable]

Keep each section concise. Preserve exact file paths, function names, and error messages.`;

export const UpdateSummarizationPrompt =
  `The messages above are NEW conversation messages to incorporate into the existing summary provided in <previous-summary> tags.

Update the existing structured summary with new information. RULES:
- PRESERVE all existing information from the previous summary
- ADD new progress, decisions, and context from the new messages
- UPDATE the Progress section: move items from "In Progress" to "Done" when completed
- UPDATE "Next Steps" based on what was accomplished
- PRESERVE exact file paths, function names, and error messages
- If something is no longer relevant, you may remove it

Use this EXACT format:

## Goal
[Preserve existing goals, add new ones if the task expanded]

## Constraints & Preferences
- [Preserve existing, add new ones discovered]

## Progress
### Done
- [x] [Include previously done items AND newly completed items]

### In Progress
- [ ] [Current work - update based on progress]

### Blocked
- [Current blockers - remove if resolved]

## Key Decisions
- **[Decision]**: [Brief rationale] (preserve all previous, add new)

## Next Steps
1. [Update based on current state]

## Critical Context
- [Preserve important context, add new if needed]

Keep each section concise. Preserve exact file paths, function names, and error messages.`;

export const TurnPrefixSummarizationPrompt =
  `This is the PREFIX of a turn that was too large to keep. The SUFFIX (recent work) is retained.

Summarize the prefix to provide context for the retained suffix:

## Original Request
[What did the user ask for in this turn?]

## Early Progress
- [Key decisions and work done in the prefix]

## Context for Suffix
- [Information needed to understand the retained recent work]

Be concise. Focus on what's needed to understand the kept suffix.`;

export type compaction_preparation = {
  firstKeptEntryId: string;
  messagesToSummarize: message[];
  turnPrefixMessages: message[];
  isSplitTurn: boolean;
  tokensBefore: number;
  previousSummary: string;
  fileOps: file_operations;
  settings: compaction_settings;
};

export type compaction_result = {
  summary: string;
  firstKeptEntryId: string;
  tokensBefore: number;
  details?: compaction_details | null;
};

export type before_compact_event = {
  preparation: compaction_preparation | null;
  branchEntries: file_entry[];
  customInstructions: string;
  context: AbortSignal;
};

export type before_compact_result = {
  cancel: boolean;
  compaction: compaction_result | null;
};

export type compact_event = {
  compactionEntry: file_entry;
  fromExtension: boolean;
};

export type tree_preparation = {
  targetId: string;
  oldLeafId: string | null;
  commonAncestorId: string | null;
  entriesToSummarize: file_entry[];
  userWantsSummary: boolean;
  customInstructions: string;
  replaceInstructions: boolean;
  label: string;
};

export type before_tree_event = {
  preparation: tree_preparation;
  context: AbortSignal;
};

export type before_tree_result = {
  cancel: boolean;
  summary: branch_summary_result | null;
  customInstructions: string | null;
  replaceInstructions: boolean | null;
  label: string | null;
};

export type tree_event = {
  newLeafId: string;
  oldLeafId: string | null;
  summaryEntry: file_entry | null;
  fromExtension: boolean;
};

export interface extension_hook {
  before_compact(evt: before_compact_event): Effect.Effect<before_compact_result, Error>;
  on_compact(evt: compact_event): Effect.Effect<void, Error>;
  before_tree(evt: before_tree_event): Effect.Effect<before_tree_result, Error>;
  on_tree(evt: tree_event): Effect.Effect<void, Error>;
}

export const extension_hooks: extension_hook[] = [];

export const generate_summary = (
  ctx: AbortSignal,
  client: client,
  currentMessages: message[],
  reserveTokens: number,
  customInstructions: string,
  previousSummary: string,
): Effect.Effect<string, Error> => {
  let basePrompt = previousSummary !== "" ? UpdateSummarizationPrompt : SummarizationPrompt;
  if (customInstructions !== "") {
    basePrompt = basePrompt + "\n\nAdditional focus: " + customInstructions;
  }

  const llmMessages = convert_to_llm(currentMessages);
  const conversationText = serialize_conversation(llmMessages);

  let promptText = `<conversation>\n${conversationText}\n</conversation>\n\n`;
  if (previousSummary !== "") {
    promptText += `<previous-summary>\n${previousSummary}\n</previous-summary>\n\n`;
  }
  promptText += basePrompt;

  const req = {
    model: "",
    messages: [
      { role: "system", content: string_content(SummarizationSystemPrompt) },
      { role: "user", content: string_content(promptText) },
    ],
  };

  return client.chat(ctx, req).pipe(
    Effect.flatMap((resp) => {
      if (!resp.choices || resp.choices.length === 0) {
        return Effect.fail(new Error("empty choices from chat"));
      }
      return Effect.succeed(content_string(resp.choices[0].message));
    }),
    Effect.mapError((err) => {
      if (err instanceof Error) return err;
      return new Error(err.reason || String(err));
    }),
  );
};

export const generate_turn_prefix_summary = (
  ctx: AbortSignal,
  client: client,
  messages: message[],
  reserveTokens: number,
): Effect.Effect<string, Error> => {
  const llmMessages = convert_to_llm(messages);
  const conversationText = serialize_conversation(llmMessages);
  const promptText =
    `<conversation>\n${conversationText}\n</conversation>\n\n${TurnPrefixSummarizationPrompt}`;

  const req = {
    model: "",
    messages: [
      { role: "system", content: string_content(SummarizationSystemPrompt) },
      { role: "user", content: string_content(promptText) },
    ],
  };

  return client.chat(ctx, req).pipe(
    Effect.flatMap((resp) => {
      if (!resp.choices || resp.choices.length === 0) {
        return Effect.fail(new Error("empty choices from chat"));
      }
      return Effect.succeed(content_string(resp.choices[0].message));
    }),
    Effect.mapError((err) => {
      if (err instanceof Error) return err;
      return new Error(err.reason || String(err));
    }),
  );
};

const get_message_from_entry = (entry: file_entry): message | null => {
  switch (entry.type) {
    case type_message:
      if (!entry.message) {
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

const get_message_from_entry_for_compaction = (entry: file_entry): message | null => {
  if (entry.type === type_compaction) {
    return null;
  }
  return get_message_from_entry(entry);
};

export const prepare_compaction = (
  pathEntries: file_entry[],
  settings: compaction_settings,
): compaction_preparation | null => {
  if (pathEntries.length > 0 && pathEntries[pathEntries.length - 1].type === type_compaction) {
    return null;
  }

  let prevCompactionIndex = -1;
  for (let i = pathEntries.length - 1; i >= 0; i--) {
    if (pathEntries[i].type === type_compaction) {
      prevCompactionIndex = i;
      break;
    }
  }

  let previousSummary = "";
  let boundaryStart = 0;
  if (prevCompactionIndex >= 0) {
    const prevCompaction = pathEntries[prevCompactionIndex];
    previousSummary = prevCompaction.summary || "";
    let firstKeptIndex = -1;
    for (let i = 0; i < pathEntries.length; i++) {
      if (pathEntries[i].id === prevCompaction.firstKeptEntryId) {
        firstKeptIndex = i;
        break;
      }
    }
    if (firstKeptIndex >= 0) {
      boundaryStart = firstKeptIndex;
    } else {
      boundaryStart = prevCompactionIndex + 1;
    }
  }
  const boundaryEnd = pathEntries.length;

  const contextObj = build_session_context(pathEntries, null);
  const tokensBefore = estimate_context_tokens(contextObj.messages ?? []).tokens;

  const cutPoint = find_cut_point(
    pathEntries,
    boundaryStart,
    boundaryEnd,
    settings.keep_recent_tokens,
  );

  if (cutPoint.firstKeptEntryIndex >= pathEntries.length) {
    return null;
  }
  const firstKeptEntry = pathEntries[cutPoint.firstKeptEntryIndex];
  if (!firstKeptEntry.id) {
    return null;
  }
  const firstKeptEntryId = firstKeptEntry.id;

  let historyEnd = cutPoint.firstKeptEntryIndex;
  if (cutPoint.isSplitTurn) {
    historyEnd = cutPoint.turnStartIndex;
  }

  // Messages to summarize (will be discarded after summary)
  const messagesToSummarize: message[] = [];
  for (let i = boundaryStart; i < historyEnd; i++) {
    const msg = get_message_from_entry_for_compaction(pathEntries[i]);
    if (msg) {
      messagesToSummarize.push(msg);
    }
  }

  // Messages for turn prefix summary (if splitting a turn)
  const turnPrefixMessages: message[] = [];
  if (cutPoint.isSplitTurn) {
    for (let i = cutPoint.turnStartIndex; i < cutPoint.firstKeptEntryIndex; i++) {
      const msg = get_message_from_entry_for_compaction(pathEntries[i]);
      if (msg) {
        turnPrefixMessages.push(msg);
      }
    }
  }

  // Extract file operations from messages and previous compaction
  const fileOps = extract_file_operations(
    messagesToSummarize,
    pathEntries,
    prevCompactionIndex,
  );

  // Also extract file ops from turn prefix if splitting
  if (cutPoint.isSplitTurn) {
    for (const msg of turnPrefixMessages) {
      extract_file_ops_from_message(msg, fileOps);
    }
  }

  return {
    firstKeptEntryId,
    messagesToSummarize,
    turnPrefixMessages,
    isSplitTurn: cutPoint.isSplitTurn,
    tokensBefore,
    previousSummary,
    fileOps,
    settings,
  };
};

export const prepare_manual_compaction = (
  pathEntries: file_entry[],
  settings: compaction_settings,
): compaction_preparation | null => {
  const manual = { ...settings, keep_recent_tokens: 0 };
  return prepare_compaction(pathEntries, manual);
};

export const compact = (
  ctx: AbortSignal,
  client: client,
  prep: compaction_preparation,
  customInstructions: string,
): Effect.Effect<compaction_result, Error> => {
  if (prep.isSplitTurn && prep.turnPrefixMessages.length > 0) {
    const historyEff =
      prep.messagesToSummarize.length > 0
        ? generate_summary(
            ctx,
            client,
            prep.messagesToSummarize,
            prep.settings.reserve_tokens,
            customInstructions,
            prep.previousSummary,
          )
        : Effect.succeed("No prior history.");

    const prefixEff = generate_turn_prefix_summary(
      ctx,
      client,
      prep.turnPrefixMessages,
      prep.settings.reserve_tokens,
    );

    return Effect.all([historyEff, prefixEff], { concurrency: "unbounded" }).pipe(
      Effect.map(([historySummary, prefixSummary]) => {
        let summary =
          `${historySummary}\n\n---\n\n**Turn Context (split turn):**\n\n${prefixSummary}`;
        const [readFiles, modifiedFiles] = compute_file_lists(prep.fileOps);
        summary += format_file_operations(readFiles, modifiedFiles);

        return {
          summary,
          firstKeptEntryId: prep.firstKeptEntryId,
          tokensBefore: prep.tokensBefore,
          details: {
            readFiles,
            modifiedFiles,
          },
        };
      }),
    );
  } else {
    const summaryEff =
      prep.messagesToSummarize.length === 0
        ? Effect.succeed("No prior history.")
        : generate_summary(
            ctx,
            client,
            prep.messagesToSummarize,
            prep.settings.reserve_tokens,
            customInstructions,
            prep.previousSummary,
          );

    return summaryEff.pipe(
      Effect.map((historySummary) => {
        let summary = historySummary;
        const [readFiles, modifiedFiles] = compute_file_lists(prep.fileOps);
        summary += format_file_operations(readFiles, modifiedFiles);

        return {
          summary,
          firstKeptEntryId: prep.firstKeptEntryId,
          tokensBefore: prep.tokensBefore,
          details: {
            readFiles,
            modifiedFiles,
          },
        };
      }),
    );
  }
};

/*
PORT STATUS
source path: backend/session/compaction.go
source lines: 447
confidence: high
status: phase_b_compile
*/
