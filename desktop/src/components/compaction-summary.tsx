import { useState } from "react"
import { Ban, Check, ChevronRight, CircleAlert, Loader2 } from "lucide-react"
import type { CompactionMessage } from "../types"
import { MarkdownContent } from "./markdown-content"

function formatTokens(value: number) {
  return new Intl.NumberFormat(undefined, { maximumFractionDigits: 0 }).format(value)
}

export function CompactionSummary({ message }: { message: CompactionMessage }) {
  const [expanded, setExpanded] = useState(false)
  const hasSummary = message.status === "success" && Boolean(message.summary?.trim())

  const appearance =
    message.status === "running"
      ? {
          icon: <Loader2 className="size-3.5 animate-spin" strokeWidth={2} />,
          title: message.cancellationPending
            ? "Cancelling compaction"
            : message.reason === "overflow"
              ? "Recovering from context overflow"
              : message.reason === "threshold"
                ? "Auto-compacting context"
                : "Compacting context",
          detail: message.cancellationPending
            ? "Waiting for the current compaction step to stop"
            : "Summarizing older context to make room - use Stop to cancel",
          className: "border-info/20 bg-info/[0.045] text-info",
        }
      : message.status === "success"
        ? {
            icon: <Check className="size-3.5" strokeWidth={2.4} />,
            title: "Context compacted",
            detail:
              typeof message.tokensBefore === "number"
                ? `${formatTokens(message.tokensBefore)} tokens${
                    typeof message.estimatedTokensAfter === "number"
                      ? ` reduced to about ${formatTokens(message.estimatedTokensAfter)}`
                      : " summarized"
                  }`
                : "Older context was summarized",
            className: "border-success/20 bg-success/[0.045] text-success",
          }
        : message.status === "cancelled"
          ? {
              icon: <Ban className="size-3.5" strokeWidth={2} />,
              title: "Compaction cancelled",
              detail: message.errorMessage || "The conversation context was left unchanged",
              className: "border-border-strong bg-surface/45 text-muted-foreground",
            }
          : {
              icon: <CircleAlert className="size-3.5" strokeWidth={2} />,
              title: "Compaction failed",
              detail: message.errorMessage || "The context could not be compacted",
              className: "border-danger/25 bg-danger/[0.045] text-danger",
            }

  return (
    <div className={`overflow-hidden rounded-xl border ${appearance.className}`}>
      <button
        type="button"
        disabled={!hasSummary}
        aria-expanded={hasSummary ? expanded : undefined}
        onClick={() => hasSummary && setExpanded((value) => !value)}
        className="flex w-full items-center gap-3 px-3.5 py-3 text-left disabled:cursor-default"
      >
        <span className="flex size-7 shrink-0 items-center justify-center rounded-lg bg-current/10">
          {appearance.icon}
        </span>
        <span className="min-w-0 flex-1">
          <span className="block text-[13px] font-semibold leading-5 text-foreground">{appearance.title}</span>
          <span className="block break-words text-xs leading-4 text-muted-foreground">{appearance.detail}</span>
        </span>
        {hasSummary && (
          <ChevronRight
            className={`size-4 shrink-0 text-muted-foreground transition-transform ${expanded ? "rotate-90" : ""}`}
            strokeWidth={1.8}
          />
        )}
      </button>
      {hasSummary && expanded && (
        <div className="border-t border-current/10 px-4 py-3 text-foreground">
          <div className="mb-2 text-[10px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
            Compaction summary
          </div>
          <MarkdownContent id={`${message.id}-summary`} text={message.summary!} />
        </div>
      )}
    </div>
  )
}
