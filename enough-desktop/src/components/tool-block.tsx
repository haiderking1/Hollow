import { useState } from "react"
import { ChevronRight, Check, Loader2, X } from "lucide-react"
import type { Block } from "../types"
import { DiffView } from "./diff-view"
import { cn } from "../lib/utils"

type ToolBlockType = Extract<Block, { type: "tool" }>

function displayPath(title: string) {
  const t = title.trim().replace(/\\/g, "/")
  const parts = t.split("/")
  return parts[parts.length - 1] || t
}

function truncate(text: string, max = 72) {
  const t = text.trim()
  if (t.length <= max) return t
  return `${t.slice(0, max)}…`
}

export function ToolBlock({ block }: { block: ToolBlockType }) {
  const hasBody = Boolean(block.output || block.diff)
  const [open, setOpen] = useState(false)
  const isBash = block.tool === "Bash"
  const label = isBash ? truncate(block.title) : displayPath(block.title)

  return (
    <div className="min-w-0">
      <button
        type="button"
        disabled={!hasBody}
        onClick={() => hasBody && setOpen((o) => !o)}
        className={cn(
          "inline-flex max-w-full min-w-0 items-center gap-2 py-0.5 text-left",
          hasBody && "transition-colors hover:opacity-90",
          !hasBody && "cursor-default",
        )}
      >
        <span className="shrink-0 text-[13px] font-medium text-foreground">{block.tool}</span>

        <span className="max-w-[min(100%,28rem)] truncate font-mono text-[13px] text-muted-foreground" title={block.title}>
          {label}
        </span>

        {block.meta && (
          <span className="shrink-0 font-mono text-[13px] tabular-nums text-add">{block.meta}</span>
        )}

        <StatusIcon status={block.status} />

        {hasBody && (
          <ChevronRight
            className={cn(
              "h-3.5 w-3.5 shrink-0 text-muted-foreground/45 transition-transform",
              open && "rotate-90",
            )}
            strokeWidth={2}
          />
        )}
      </button>

      {open && hasBody && (
        <div className="mt-1.5 border-l border-border/60 pl-3">
          {block.output && (
            <pre className="overflow-x-auto whitespace-pre-wrap py-1 font-mono text-[12px] leading-relaxed text-muted-foreground">
              {block.output}
            </pre>
          )}
          {block.diff && <DiffView diff={block.diff} />}
        </div>
      )}
    </div>
  )
}

function StatusIcon({ status }: { status: ToolBlockType["status"] }) {
  if (status === "running") {
    return <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin text-muted-foreground" strokeWidth={2} />
  }
  if (status === "error") {
    return <X className="h-3.5 w-3.5 shrink-0 text-danger" strokeWidth={2} />
  }
  return <Check className="h-3.5 w-3.5 shrink-0 text-success" strokeWidth={2.5} />
}
