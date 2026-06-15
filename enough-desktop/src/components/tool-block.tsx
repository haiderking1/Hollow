import { useState } from "react"
import {
  ChevronRight,
  Terminal,
  FileText,
  FilePen,
  FilePlus,
  Search,
  Check,
  CircleDot,
  X,
} from "lucide-react"
import type { Block } from "../types"
import { DiffView } from "./diff-view"
import { cn } from "../lib/utils"

type ToolBlockType = Extract<Block, { type: "tool" }>

const toolIcons: Record<string, typeof Terminal> = {
  Bash: Terminal,
  Read: FileText,
  Edit: FilePen,
  Write: FilePlus,
  Grep: Search,
  Search: Search,
}

export function ToolBlock({ block }: { block: ToolBlockType }) {
  const hasBody = Boolean(block.output || block.diff)
  const [open, setOpen] = useState(block.tool === "Edit" || block.tool === "Write")
  const Icon = toolIcons[block.tool] ?? Terminal

  return (
    <div className="overflow-hidden rounded-lg border border-border bg-surface">
      <button
        onClick={() => hasBody && setOpen((o) => !o)}
        className={cn(
          "flex w-full items-center gap-2.5 px-3 py-2 text-left",
          hasBody && "transition-colors hover:bg-surface-hover",
        )}
      >
        {hasBody ? (
          <ChevronRight
            className={cn("h-3.5 w-3.5 shrink-0 text-muted-foreground transition-transform", open && "rotate-90")}
          />
        ) : (
          <span className="w-3.5" />
        )}
        <Icon className="h-3.5 w-3.5 shrink-0 text-accent" />
        <span className="font-mono text-[12px] font-medium text-foreground">{block.tool}</span>
        <span className="truncate font-mono text-[12px] text-muted-foreground">{block.title}</span>
        <span className="ml-auto flex shrink-0 items-center gap-2">
          {block.meta && <span className="font-mono text-[11px] text-muted-foreground">{block.meta}</span>}
          <StatusBadge status={block.status} />
        </span>
      </button>

      {open && hasBody && (
        <div className="border-t border-border px-3 pb-2">
          {block.output && (
            <pre className="overflow-x-auto whitespace-pre-wrap py-2 font-mono text-[12px] leading-[1.6] text-foreground/75">
              {block.output}
            </pre>
          )}
          {block.diff && <DiffView diff={block.diff} />}
        </div>
      )}
    </div>
  )
}

function StatusBadge({ status }: { status: ToolBlockType["status"] }) {
  if (status === "running") {
    return <CircleDot className="h-3.5 w-3.5 animate-pulse-dot text-warning" />
  }
  if (status === "error") {
    return <X className="h-3.5 w-3.5 text-danger" />
  }
  return <Check className="h-3.5 w-3.5 text-success" />
}
