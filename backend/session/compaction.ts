import { Effect } from "effect";
import type { chat_request, chat_response, message } from "../opencode/types";
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
  estimate_serialized_tokens,
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

const summary_output_budget = (reserveTokens: number, fraction: number): number | Error => {
  if (!Number.isSafeInteger(reserveTokens) || reserveTokens <= 0) {
    return new Error(`invalid compaction reserve token budget: ${reserveTokens}`);
  }
  return Math.max(1, Math.min(reserveTokens, Math.floor(reserveTokens * fraction)));
};

const validate_compaction_input = (
  promptText: string,
  reserveTokens: number,
  contextWindow: number | undefined,
): Error | null => {
  if (promptText.trim() === "") return new Error("invalid empty compaction input");
  if (contextWindow === undefined || contextWindow === 0) return null;
  if (!Number.isSafeInteger(contextWindow) || contextWindow < 0) {
    return new Error(`invalid compaction context window: ${contextWindow}`);
  }
  if (reserveTokens >= contextWindow) {
    return new Error(
      `invalid compaction input budget: reserve ${reserveTokens} must be smaller than context window ${contextWindow}`,
    );
  }

  // Leave 10% of the nominal input budget for tokenizer variance and provider-added framing.
  const allowance = Math.floor((contextWindow - reserveTokens) * 0.9);
  const estimated = estimate_serialized_tokens(`${SummarizationSystemPrompt}\n\n${promptText}`);
  if (allowance <= 0 || estimated > allowance) {
    return new Error(
      `compaction input exceeds safe context allowance: estimated ${estimated} tokens, allowance ${allowance}`,
    );
  }
  return null;
};

const client_error_as_error = (cause: unknown): Error => {
  if (cause instanceof Error) return cause;
  if (typeof cause === "object" && cause !== null && (cause as { _tag?: unknown })._tag === "ClientError") {
    const value = cause as {
      reason?: string;
      cause?: unknown;
      status?: number;
      code?: string;
      timeout?: boolean;
      aborted?: boolean;
    };
    const out = new Error(value.reason || "provider request failed", { cause: value.cause });
    Object.assign(out, {
      status: value.status,
      code: value.code,
      timeout: value.timeout,
      aborted: value.aborted,
    });
    return out;
  }
  return new Error(String(cause));
};

const compaction_error_text = (cause: unknown): string => {
  if (cause instanceof Error) return cause.message.toLowerCase();
  if (typeof cause === "object" && cause !== null) {
    const value = cause as { reason?: unknown; message?: unknown; cause?: unknown };
    const own = typeof value.reason === "string"
      ? value.reason
      : typeof value.message === "string" ? value.message : "";
    return `${own} ${compaction_error_text(value.cause)}`.toLowerCase();
  }
  return String(cause ?? "").toLowerCase();
};

const compaction_error_value = (cause: unknown, key: "status" | "code" | "timeout" | "aborted"): unknown => {
  if (typeof cause !== "object" || cause === null) return undefined;
  const value = cause as Record<string, unknown> & { cause?: unknown };
  return value[key] ?? compaction_error_value(value.cause, key);
};

const is_transient_compaction_error = (cause: unknown, ctx: AbortSignal): boolean => {
  if (ctx.aborted || compaction_error_value(cause, "aborted") === true) return false;
  const text = compaction_error_text(cause);
  if (/quota|insufficient[_ ]credits|billing/.test(text)) return false;
  if (/context.{0,20}(length|window|limit)|prompt is too long|too many tokens|input is too long/.test(text)) return false;

  const status = compaction_error_value(cause, "status");
  if (status === 401 || status === 403) return false;
  if (status === 429 || status === 502 || status === 503 || status === 504) return true;
  if (compaction_error_value(cause, "timeout") === true) return true;

  const code = String(compaction_error_value(cause, "code") ?? "").toUpperCase();
  if (/^(ECONNRESET|ECONNREFUSED|EPIPE|ETIMEDOUT|ENETUNREACH|EAI_AGAIN|UND_ERR_)/.test(code)) return true;
  return /fetch failed|network error|connection reset|socket hang up|unexpected eof/.test(text);
};

const retry_delay = (ms: number, ctx: AbortSignal): Promise<void> =>
  new Promise((resolve, reject) => {
    if (ctx.aborted) {
      reject(ctx.reason ?? new DOMException("The operation was aborted", "AbortError"));
      return;
    }
    const on_abort = () => {
      clearTimeout(timer);
      reject(ctx.reason ?? new DOMException("The operation was aborted", "AbortError"));
    };
    const timer = setTimeout(() => {
      ctx.removeEventListener("abort", on_abort);
      resolve();
    }, ms);
    ctx.addEventListener("abort", on_abort, { once: true });
  });

const summary_chat_with_retry = (
  ctx: AbortSignal,
  chatClient: client,
  request: () => chat_request,
): Effect.Effect<chat_response, Error> =>
  Effect.tryPromise({
    try: async () => {
      let lastError: unknown;
      for (let attempt = 0; attempt < 3; attempt++) {
        try {
          const result = await Effect.runPromise(Effect.either(chatClient.chat(ctx, request())));
          if (result._tag === "Left") throw result.left;
          return result.right;
        } catch (cause) {
          lastError = cause;
          if (attempt === 2 || !is_transient_compaction_error(cause, ctx)) throw cause;
          await retry_delay(250 * (2 ** attempt), ctx);
        }
      }
      throw lastError;
    },
    catch: client_error_as_error,
  });

const summary_from_response = (resp: chat_response): Effect.Effect<string, Error> => {
  if (!resp.choices || resp.choices.length === 0) {
    return Effect.fail(new Error("empty choices from compaction chat"));
  }
  const choice = resp.choices[0];
  const finishReason = String(choice.finish_reason ?? "").trim().toLowerCase();
  if (finishReason !== "stop") {
    return Effect.fail(new Error(`incomplete compaction summary: finish reason ${finishReason || "missing"}`));
  }
  const summary = content_string(choice.message).trim();
  if (summary === "") return Effect.fail(new Error("empty compaction summary"));
  return Effect.succeed(summary);
};

export const generate_summary = (
  ctx: AbortSignal,
  client: client,
  currentMessages: message[],
  reserveTokens: number,
  customInstructions: string,
  previousSummary: string,
  contextWindow?: number,
): Effect.Effect<string, Error> => {
  let basePrompt = previousSummary !== "" ? UpdateSummarizationPrompt : SummarizationPrompt;
  if (customInstructions !== "") {
    basePrompt = basePrompt + "\n\nAdditional focus: " + customInstructions;
  }

  const llmMessages = convert_to_llm(currentMessages);
  const conversationText = serialize_conversation(llmMessages);
  if (conversationText.trim() === "") return Effect.fail(new Error("invalid empty compaction conversation"));

  let promptText = `<conversation>\n${conversationText}\n</conversation>\n\n`;
  if (previousSummary !== "") {
    promptText += `<previous-summary>\n${previousSummary}\n</previous-summary>\n\n`;
  }
  promptText += basePrompt;

  const outputBudget = summary_output_budget(reserveTokens, 0.8);
  if (outputBudget instanceof Error) return Effect.fail(outputBudget);
  const inputError = validate_compaction_input(promptText, reserveTokens, contextWindow);
  if (inputError !== null) {
    if (
      inputError.message.startsWith("compaction input exceeds safe context allowance") &&
      currentMessages.length > 1
    ) {
      const midpoint = Math.ceil(currentMessages.length / 2);
      return generate_summary(
        ctx,
        client,
        currentMessages.slice(0, midpoint),
        reserveTokens,
        customInstructions,
        previousSummary,
        contextWindow,
      ).pipe(
        Effect.flatMap((partialSummary) =>
          generate_summary(
            ctx,
            client,
            currentMessages.slice(midpoint),
            reserveTokens,
            customInstructions,
            partialSummary,
            contextWindow,
          ),
        ),
      );
    }
    return Effect.fail(inputError);
  }

  const request = (): chat_request => ({
    model: "",
    messages: [
      { role: "system", content: string_content(SummarizationSystemPrompt) },
      { role: "user", content: string_content(promptText) },
    ],
    max_tokens: outputBudget,
  });

  return summary_chat_with_retry(ctx, client, request).pipe(Effect.flatMap(summary_from_response));
};

export const generate_turn_prefix_summary = (
  ctx: AbortSignal,
  client: client,
  messages: message[],
  reserveTokens: number,
  contextWindow?: number,
): Effect.Effect<string, Error> => {
  const llmMessages = convert_to_llm(messages);
  const conversationText = serialize_conversation(llmMessages);
  if (conversationText.trim() === "") return Effect.fail(new Error("invalid empty compaction turn prefix"));
  const promptText =
    `<conversation>\n${conversationText}\n</conversation>\n\n${TurnPrefixSummarizationPrompt}`;

  const outputBudget = summary_output_budget(reserveTokens, 0.5);
  if (outputBudget instanceof Error) return Effect.fail(outputBudget);
  const inputError = validate_compaction_input(promptText, reserveTokens, contextWindow);
  if (inputError !== null) {
    if (
      inputError.message.startsWith("compaction input exceeds safe context allowance") &&
      messages.length > 1
    ) {
      const midpoint = Math.ceil(messages.length / 2);
      return Effect.all([
        generate_turn_prefix_summary(ctx, client, messages.slice(0, midpoint), reserveTokens, contextWindow),
        generate_turn_prefix_summary(ctx, client, messages.slice(midpoint), reserveTokens, contextWindow),
      ], { concurrency: 2 }).pipe(
        Effect.map(([first, second]) => `${first}\n\n${second}`),
      );
    }
    return Effect.fail(inputError);
  }

  const request = (): chat_request => ({
    model: "",
    messages: [
      { role: "system", content: string_content(SummarizationSystemPrompt) },
      { role: "user", content: string_content(promptText) },
    ],
    max_tokens: outputBudget,
  });

  return summary_chat_with_retry(ctx, client, request).pipe(Effect.flatMap(summary_from_response));
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

  if (messagesToSummarize.length === 0 && turnPrefixMessages.length === 0) {
    return null;
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
  return prepare_compaction(pathEntries, settings);
};

export const compact = (
  ctx: AbortSignal,
  client: client,
  prep: compaction_preparation,
  customInstructions: string,
): Effect.Effect<compaction_result, Error> => {
  const reserveError = summary_output_budget(prep.settings.reserve_tokens, 0.8);
  if (reserveError instanceof Error) return Effect.fail(reserveError);
  const contextWindow = prep.settings.context_window;
  if (contextWindow !== undefined && contextWindow !== 0) {
    if (!Number.isSafeInteger(contextWindow) || contextWindow < 0) {
      return Effect.fail(new Error(`invalid compaction context window: ${contextWindow}`));
    }
    if (prep.settings.reserve_tokens >= contextWindow) {
      return Effect.fail(new Error(
        `invalid compaction input budget: reserve ${prep.settings.reserve_tokens} must be smaller than context window ${contextWindow}`,
      ));
    }
  }

  const priorHistory = prep.previousSummary.trim() !== "" ? prep.previousSummary : "No prior history.";
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
            prep.settings.context_window,
          )
        : Effect.succeed(priorHistory);

    const prefixEff = generate_turn_prefix_summary(
      ctx,
      client,
      prep.turnPrefixMessages,
      prep.settings.reserve_tokens,
      prep.settings.context_window,
    );

    return Effect.all([historyEff, prefixEff], { concurrency: 2 }).pipe(
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
        ? Effect.succeed(priorHistory)
        : generate_summary(
            ctx,
            client,
            prep.messagesToSummarize,
            prep.settings.reserve_tokens,
            customInstructions,
            prep.previousSummary,
            prep.settings.context_window,
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
