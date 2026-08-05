import { Agent } from "./agent";
import { ModelContextWindow } from "./models";
import { Effect } from "effect";
import {
  event_compaction_start,
  event_compaction_end,
  event_branch_summary_start,
  event_branch_summary_end
} from "../core/events";
import { type message, string_content, type content_block } from "../opencode/types";
import { type file_entry, type_compaction } from "../session/types";
import {
  prepare_compaction,
  prepare_manual_compaction,
  compact,
  extension_hooks,
  type compaction_preparation,
  type compaction_result
} from "../session/compaction";
import { estimate_context_tokens } from "../session/compaction_utils";
import {
  collect_entries_for_branch_summary,
  generate_branch_summary,
  type branch_summary_details
} from "../session/branch_summarization";

export { overflowPatterns, IsContextOverflowError } from "./overflow";

export function GetContextWindow(provider: string, model: string): number {
  return ModelContextWindow(provider, model, 0);
}

export function ReloadMessagesFromSession(this: Agent): void {
  if (this.session === null) {
    return;
  }
  const sessionMsgs = this.session.build_session_context().messages ?? [];
  const systemMsg: message = {
    role: "system",
    content: string_content(this.systemPrompt()),
  };
  this.messages = [systemMsg, ...sessionMsgs];
}

Agent.prototype.ReloadMessagesFromSession = ReloadMessagesFromSession;

export function emitCompactionEnd(
  this: Agent,
  requestId: string,
  operationId: string,
  reason: string,
  result: any,
  aborted: boolean,
  willRetry: boolean,
  errMsg: string
): void {
  if (this.emit !== null) {
    this.emit({
      kind: event_compaction_end,
      data: {
        request_id: requestId,
        operation_id: operationId,
        session_id: this.session?.session_id() ?? "",
        reason: reason,
        result: result,
        aborted: aborted,
        will_retry: willRetry,
        error_message: errMsg,
      },
    });
  }
}

const operationId = (): string =>
  `compact_${Date.now()}_${Math.floor(Math.random() * 1_000_000)}`;

const compactionError = (cause: unknown): Error => {
  if (cause instanceof Error) return cause;
  if (cause && typeof cause === "object") {
    const value = cause as { reason?: unknown; message?: unknown; cause?: unknown };
    const message = String(value.reason ?? value.message ?? "").trim();
    if (message !== "") return new Error(message, { cause: value.cause });
  }
  return new Error(String(cause || "Compaction failed"));
};

const awaitWithAbort = <T>(promise: Promise<T>, signal: AbortSignal): Promise<T> =>
  new Promise<T>((resolve, reject) => {
    if (signal.aborted) {
      reject(signal.reason ?? new DOMException("Compaction cancelled", "AbortError"));
      return;
    }
    const onAbort = () => reject(signal.reason ?? new DOMException("Compaction cancelled", "AbortError"));
    signal.addEventListener("abort", onAbort, { once: true });
    promise.then(
      (value) => {
        signal.removeEventListener("abort", onAbort);
        resolve(value);
      },
      (cause) => {
        signal.removeEventListener("abort", onAbort);
        reject(cause);
      },
    );
  });

const runEffect = async <A>(effect: Effect.Effect<A, Error>): Promise<A> => {
  const result = await Effect.runPromise(Effect.either(effect));
  if (result._tag === "Left") throw result.left;
  return result.right;
};

const validateResult = (result: {
  summary: unknown;
  firstKeptEntryId: unknown;
  tokensBefore: unknown;
}, entries: file_entry[]): void => {
  if (typeof result.summary !== "string" || result.summary.trim() === "") {
    throw new Error("Compaction produced an empty summary");
  }
  if (typeof result.firstKeptEntryId !== "string" ||
      !entries.some((entry) => entry.id === result.firstKeptEntryId)) {
    throw new Error("Compaction produced an invalid retained-history boundary");
  }
  if (!Number.isFinite(result.tokensBefore) || Number(result.tokensBefore) < 0) {
    throw new Error("Compaction produced an invalid token count");
  }
};

// Compact manually runs compaction on the agent's session.
export async function Compact(
  this: Agent,
  ctx: AbortSignal,
  customInstructions: string,
  requestId = operationId(),
): Promise<any> {
  if (this.compactionOperationId !== null) {
    throw new Error("Compaction is already in progress");
  }
  const opId = requestId;
  this.compactionOperationId = opId;
  this.compactionReason = "manual";
  const controller = new AbortController();
  let timedOut = false;
  let ended = false;
  const onAbort = () => controller.abort(ctx.reason);
  if (ctx.aborted) onAbort();
  else ctx.addEventListener("abort", onAbort, { once: true });
  const timeoutMs = this.client.timeout_ms > 0 ? this.client.timeout_ms : 5 * 60 * 1000;
  const timeout = setTimeout(() => {
    timedOut = true;
    controller.abort(new Error(`Compaction timed out after ${timeoutMs}ms`));
  }, timeoutMs);
  this.compactionCancel = () => controller.abort(new DOMException("Compaction cancelled", "AbortError"));

  this.emit?.({
    kind: event_compaction_start,
    data: {
      request_id: requestId,
      operation_id: opId,
      session_id: this.session?.session_id() ?? "",
      reason: "manual",
    },
  });

  try {
    await this.AbortAndWait(controller.signal, false);
    if (controller.signal.aborted) throw controller.signal.reason;
    if (this.session === null) {
      throw new Error("No session manager available");
    }

    const pathEntries = this.session.get_branch(this.session.leaf_id());
    let messageCount = 0;
    for (const entry of pathEntries) {
      if (entry.type === "message") {
        messageCount++;
      }
    }

    if (messageCount < 2) {
      throw new Error("Nothing to compact (no messages yet)");
    }

    const settings = {
      ...this.cfg.compaction,
      context_window: ModelContextWindow(
        this.cfg.provider,
        this.cfg.model,
        this.cfg.compaction.context_window || 0,
      ),
    };
    const prep = prepare_manual_compaction(pathEntries, settings);
    if (prep === null) {
      if (pathEntries.length > 0 && pathEntries[pathEntries.length - 1].type === type_compaction) {
        throw new Error("Already compacted");
      }
      throw new Error("Nothing to compact (session too small)");
    }

    // Support compaction hooks
    let extCompaction: any = null;
    let fromExt = false;
    for (const hook of extension_hooks) {
      if (hook.before_compact) {
        const res = await awaitWithAbort(
          runEffect(hook.before_compact({
            preparation: prep,
            branchEntries: pathEntries,
            customInstructions: customInstructions,
            context: controller.signal,
          })),
          controller.signal,
        );
        if (res) {
          if (res.cancel) {
            throw new DOMException("Compaction cancelled", "AbortError");
          }
          if (res.compaction) {
            extCompaction = res.compaction;
            fromExt = true;
            break;
          }
        }
      }
    }

    let summary = "";
    let firstKeptEntryID = "";
    let tokensBefore = 0;
    let details: any = null;

    if (extCompaction !== null) {
      summary = extCompaction.summary;
      firstKeptEntryID = extCompaction.firstKeptEntryId;
      tokensBefore = extCompaction.tokensBefore;
      details = extCompaction.details;
    } else {
      const res = await runEffect(
        compact(controller.signal, this.client, prep, customInstructions)
      );
      summary = res.summary;
      firstKeptEntryID = res.firstKeptEntryId;
      tokensBefore = res.tokensBefore;
      details = res.details;
    }

    if (controller.signal.aborted) {
      throw controller.signal.reason;
    }

    validateResult({ summary, firstKeptEntryId: firstKeptEntryID, tokensBefore }, pathEntries);

    summary = appendMemoryAuthorityNote.call(this, summary);
    await runEffect(
      this.session.append_compaction(summary, firstKeptEntryID, tokensBefore, details, fromExt)
    );

    this.invalidateSystemPrompt();
    this.ReloadMessagesFromSession();
    const estimatedTokensAfter = estimate_context_tokens(
      this.session.build_session_context().messages ?? [],
    ).tokens;

    // Call OnCompact hooks
    const newEntries = this.session.parsed_entries();
    let savedEntry: file_entry | null = null;
    for (let i = newEntries.length - 1; i >= 0; i--) {
      if (newEntries[i].type === type_compaction && newEntries[i].summary === summary) {
        savedEntry = newEntries[i];
        break;
      }
    }
    if (savedEntry !== null) {
      for (const hook of extension_hooks) {
        if (hook.on_compact) {
          try {
            await awaitWithAbort(
              runEffect(hook.on_compact({
                compactionEntry: savedEntry,
                fromExtension: fromExt,
              })),
              controller.signal,
            );
          } catch {}
        }
      }
    }

    const compactionResult = {
      summary: summary,
      firstKeptEntryId: firstKeptEntryID,
      tokensBefore: tokensBefore,
      estimatedTokensAfter,
    };
    emitCompactionEnd.call(this, requestId, opId, "manual", compactionResult, false, false, "");
    ended = true;
    return compactionResult;
  } catch (cause) {
    const err = compactionError(cause);
    const aborted = !timedOut && (
      ctx.aborted ||
      controller.signal.aborted ||
      err.name === "AbortError" ||
      err.message.toLowerCase().includes("cancelled")
    );
    const message = timedOut
      ? `Compaction timed out after ${timeoutMs}ms`
      : aborted ? "Compaction cancelled" : err.message;
    if (!ended) {
      emitCompactionEnd.call(this, requestId, opId, "manual", null, aborted, false, message);
      ended = true;
    }
    throw new Error(message, { cause: err });
  } finally {
    clearTimeout(timeout);
    ctx.removeEventListener("abort", onAbort);
    if (this.compactionOperationId === opId) {
      this.compactionOperationId = null;
      this.compactionReason = null;
      this.compactionCancel = null;
    }
  }
}

Agent.prototype.Compact = Compact;

export const memoryAuthorityNote =
  "Your persistent memory (MEMORY.md, USER.md) in the system prompt remains fully authoritative regardless of compaction.";

export function appendMemoryAuthorityNote(this: Agent, summary: string): string {
  if (summary === "") {
    return summary;
  }
  if (!this.cfg.memory?.memory_enabled && !this.cfg.memory?.user_profile_enabled) {
    return summary;
  }
  if (summary.includes(memoryAuthorityNote)) {
    return summary;
  }
  return summary + "\n\n" + memoryAuthorityNote;
}

export async function RunAutoCompaction(this: Agent, ctx: AbortSignal, reason: string, willRetry: boolean): Promise<boolean> {
  if (this.compactionOperationId !== null) {
    return false;
  }

  const requestId = this.activeRequestId || operationId();
  const opId = operationId();
  this.compactionOperationId = opId;
  this.compactionReason = reason;
  const controller = new AbortController();
  let timedOut = false;
  let ended = false;
  const onAbort = () => controller.abort(ctx.reason);
  if (ctx.aborted) onAbort();
  else ctx.addEventListener("abort", onAbort, { once: true });
  const timeoutMs = this.client.timeout_ms > 0 ? this.client.timeout_ms : 5 * 60 * 1000;
  const timeout = setTimeout(() => {
    timedOut = true;
    controller.abort(new Error(`Compaction timed out after ${timeoutMs}ms`));
  }, timeoutMs);
  this.compactionCancel = () => controller.abort(new DOMException("Compaction cancelled", "AbortError"));

  this.emit?.({
    kind: event_compaction_start,
    data: {
      request_id: requestId,
      operation_id: opId,
      session_id: this.session?.session_id() ?? "",
      reason,
    },
  });

  try {
    if (controller.signal.aborted) throw controller.signal.reason;
    if (this.session === null) {
      emitCompactionEnd.call(this, requestId, opId, reason, null, false, false, "No session manager available");
      ended = true;
      return false;
    }

    const pathEntries = this.session.get_branch(this.session.leaf_id());
    const settings = {
      ...this.cfg.compaction,
      context_window: ModelContextWindow(
        this.cfg.provider,
        this.cfg.model,
        this.cfg.compaction.context_window || 0,
      ),
    };
    const prep = prepare_compaction(pathEntries, settings);
    if (prep === null) {
      emitCompactionEnd.call(this, requestId, opId, reason, null, false, false, "Nothing to compact");
      ended = true;
      return false;
    }

    // Support compaction hooks
    let extCompaction: any = null;
    let fromExt = false;
    for (const hook of extension_hooks) {
      if (hook.before_compact) {
        const res = await awaitWithAbort(
          runEffect(hook.before_compact({
            preparation: prep,
            branchEntries: pathEntries,
            customInstructions: "",
            context: controller.signal,
          })),
          controller.signal,
        );
        if (res) {
          if (res.cancel) {
            throw new DOMException("Compaction cancelled", "AbortError");
          }
          if (res.compaction) {
            extCompaction = res.compaction;
            fromExt = true;
            break;
          }
        }
      }
    }

    let summary = "";
    let firstKeptEntryID = "";
    let tokensBefore = 0;
    let details: any = null;

    if (extCompaction !== null) {
      summary = extCompaction.summary;
      firstKeptEntryID = extCompaction.firstKeptEntryId;
      tokensBefore = extCompaction.tokensBefore;
      details = extCompaction.details;
    } else {
      const res = await runEffect(
        compact(controller.signal, this.client, prep, "")
      );
      summary = res.summary;
      firstKeptEntryID = res.firstKeptEntryId;
      tokensBefore = res.tokensBefore;
      details = res.details;
    }

    if (controller.signal.aborted) {
      throw controller.signal.reason;
    }

    validateResult({ summary, firstKeptEntryId: firstKeptEntryID, tokensBefore }, pathEntries);

    summary = appendMemoryAuthorityNote.call(this, summary);
    await runEffect(
      this.session.append_compaction(summary, firstKeptEntryID, tokensBefore, details, fromExt)
    );

    this.invalidateSystemPrompt();
    this.ReloadMessagesFromSession();
    const estimatedTokensAfter = estimate_context_tokens(
      this.session.build_session_context().messages ?? [],
    ).tokens;

    // Call OnCompact hooks
    const newEntries = this.session.parsed_entries();
    let savedEntry: file_entry | null = null;
    for (let i = newEntries.length - 1; i >= 0; i--) {
      if (newEntries[i].type === type_compaction && newEntries[i].summary === summary) {
        savedEntry = newEntries[i];
        break;
      }
    }
    if (savedEntry !== null) {
      for (const hook of extension_hooks) {
        if (hook.on_compact) {
          try {
            await awaitWithAbort(
              runEffect(hook.on_compact({
                compactionEntry: savedEntry,
                fromExtension: fromExt,
              })),
              controller.signal,
            );
          } catch {}
        }
      }
    }

    const compactionResult = {
      summary: summary,
      firstKeptEntryId: firstKeptEntryID,
      tokensBefore: tokensBefore,
      estimatedTokensAfter,
    };
    emitCompactionEnd.call(this, requestId, opId, reason, compactionResult, false, willRetry, "");
    ended = true;
    return true;
  } catch (cause) {
    const err = compactionError(cause);
    const aborted = !timedOut && (
      ctx.aborted ||
      controller.signal.aborted ||
      err.name === "AbortError" ||
      err.message.toLowerCase().includes("cancelled")
    );
    const errMsg = timedOut
      ? `Auto-compaction timed out after ${timeoutMs}ms`
      : aborted ? "Compaction cancelled" : `Auto-compaction failed: ${err.message}`;
    if (!ended) {
      emitCompactionEnd.call(this, requestId, opId, reason, null, aborted, false, errMsg);
      ended = true;
    }
    return false;
  } finally {
    clearTimeout(timeout);
    ctx.removeEventListener("abort", onAbort);
    if (this.compactionOperationId === opId) {
      this.compactionOperationId = null;
      this.compactionReason = null;
      this.compactionCancel = null;
    }
  }
}

Agent.prototype.RunAutoCompaction = RunAutoCompaction;

export interface NavigateOptions {
  Summarize: boolean;
  CustomInstructions: string;
}

export async function NavigateToEntry(
  this: Agent,
  ctx: AbortSignal,
  targetID: string,
  opts: NavigateOptions
): Promise<boolean> {
  if (typeof (this as any).AbortAndWait === "function") {
    (this as any).AbortAndWait();
  }

  if (this.session === null) {
    throw new Error("no session manager available");
  }

  const oldLeaf = this.session.leaf_id();
  if (oldLeaf !== null && oldLeaf === targetID) {
    return false; // no-op if already at target
  }

  let targetEntry: file_entry | null = null;
  for (const entry of this.session.parsed_entries()) {
    if (entry.id === targetID) {
      targetEntry = entry;
      break;
    }
  }
  if (targetEntry === null) {
    throw new Error(`entry ${targetID} not found`);
  }

  const prepResult = collect_entries_for_branch_summary(
    this.session.parsed_entries(),
    oldLeaf,
    targetID
  );

  let customInstructions = opts.CustomInstructions;
  let replaceInstructions = false;

  let extSummary: any = null;
  let fromExt = false;

  for (const hook of extension_hooks) {
    if (hook.before_tree) {
      const res = await Effect.runPromise(
        hook.before_tree({
          preparation: {
            targetId: targetID,
            oldLeafId: oldLeaf,
            commonAncestorId: prepResult.commonAncestorId,
            entriesToSummarize: prepResult.entries ?? [],
            userWantsSummary: opts.Summarize,
            customInstructions: customInstructions,
            replaceInstructions: false,
            label: "",
          },
          context: ctx,
        })
      );
      if (res) {
        if (res.cancel) {
          throw new Error("navigation cancelled by extension");
        }
        if (res.summary) {
          extSummary = res.summary;
          fromExt = true;
        }
        if (res.customInstructions !== undefined && res.customInstructions !== null) {
          customInstructions = res.customInstructions;
        }
        if (res.replaceInstructions !== undefined && res.replaceInstructions !== null) {
          replaceInstructions = res.replaceInstructions;
        }
      }
    }
  }

  if (this.emit !== null) {
    this.emit({
      kind: event_branch_summary_start,
      data: { target_id: targetID },
    });
  }

  let summaryText = "";
  let summaryDetails: any = null;

  if (opts.Summarize && prepResult.entries && prepResult.entries.length > 0 && extSummary === null) {
    const contextWindow = ModelContextWindow(
      this.cfg.provider,
      this.cfg.model,
      this.cfg.compaction?.context_window ?? 0
    );
    let reserveTokens = this.cfg.compaction?.reserve_tokens ?? 16384;
    if (reserveTokens <= 0) {
      reserveTokens = 16384;
    }

    const genOpts = {
      client: this.client,
      customInstructions: customInstructions,
      replaceInstructions: replaceInstructions,
      reserveTokens: reserveTokens,
      contextWindow: contextWindow,
    };

    let res: any;
    try {
      res = await Effect.runPromise(
        generate_branch_summary(ctx, this.session.parsed_entries(), genOpts)
      );
    } catch (err: any) {
      if (this.emit !== null) {
        this.emit({
          kind: event_branch_summary_end,
          data: {
            target_id: targetID,
            aborted: ctx.aborted,
            error_message: err.message || String(err),
          },
        });
      }
      throw err;
    }

    if (res.aborted) {
      if (this.emit !== null) {
        this.emit({
          kind: event_branch_summary_end,
          data: {
            target_id: targetID,
            aborted: true,
          },
        });
      }
      throw new Error("branch summarization aborted");
    }

    summaryText = res.summary;
    summaryDetails = {
      readFiles: res.readFiles || [],
      modifiedFiles: res.modifiedFiles || [],
    } as branch_summary_details;
  } else if (extSummary !== null) {
    summaryText = extSummary.summary || "";
    summaryDetails = {
      readFiles: extSummary.readFiles || [],
      modifiedFiles: extSummary.modifiedFiles || [],
    } as branch_summary_details;
  }

  let newLeafID = "";
  if (
    targetEntry.type === "message" &&
    targetEntry.message &&
    targetEntry.message.role === "user"
  ) {
    if (targetEntry.parentId !== undefined && targetEntry.parentId !== null) {
      newLeafID = targetEntry.parentId;
    }
  } else if (targetEntry.type === "custom_message") {
    if (targetEntry.parentId !== undefined && targetEntry.parentId !== null) {
      newLeafID = targetEntry.parentId;
    }
  } else {
    newLeafID = targetID;
  }

  let summaryEntry: file_entry | null = null;
  if (summaryText !== "") {
    const parentPtr = newLeafID !== "" ? newLeafID : null;
    const summaryID = await Effect.runPromise(
      this.session.branch_with_summary(parentPtr, summaryText, summaryDetails, fromExt)
    );

    for (const e of this.session.parsed_entries()) {
      if (e.id === summaryID) {
        summaryEntry = e;
        break;
      }
    }
  } else if (newLeafID === "") {
    this.session.reset_leaf();
  } else {
    this.session.branch(newLeafID);
  }

  this.ReloadMessagesFromSession();

  if (this.emit !== null) {
    let resultSummary: any = null;
    if (summaryText !== "") {
      let rf: string[] = [];
      let mf: string[] = [];
      if (summaryDetails) {
        rf = summaryDetails.readFiles || [];
        mf = summaryDetails.modifiedFiles || [];
      }
      resultSummary = {
        summary: summaryText,
        read_files: rf,
        modified_files: mf,
      };
    }

    this.emit({
      kind: event_branch_summary_end,
      data: {
        target_id: targetID,
        result: resultSummary,
        aborted: false,
      },
    });
  }

  const newLeafStr = this.session.leaf_id() || "";
  for (const hook of extension_hooks) {
    if (hook.on_tree) {
      try {
        await Effect.runPromise(
          hook.on_tree({
            newLeafId: newLeafStr,
            oldLeafId: oldLeaf || "",
            summaryEntry: summaryEntry,
            fromExtension: fromExt,
          })
        );
      } catch {}
    }
  }

  return true;
}

Agent.prototype.NavigateToEntry = NavigateToEntry;
