import { useState } from "react"
import { ChevronRight, Loader2 } from "lucide-react"
import { cn } from "../lib/utils"
import { MarkdownContent } from "./markdown-content"

export function ThinkingBlock({
  id,
  text,
  streaming,
}: {
  id: string
  text: string
  streaming?: boolean
}) {
  const [open, setOpen] = useState(false)
  const hasText = text.length > 0
  const waiting = Boolean(streaming && !hasText)

  return (
    <div className="min-w-0 text-[14px] leading-relaxed">
      <button
        type="button"
        disabled={!hasText}
        onClick={() => hasText && setOpen((o) => !o)}
        className={cn(
          "inline-flex items-center gap-2 text-left",
          hasText && "transition-colors hover:opacity-90",
          !hasText && "cursor-default",
        )}
      >
        <span className="text-foreground/90">Thinking</span>
        {waiting && (
          <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin text-muted-foreground/50" strokeWidth={2} />
        )}
        {hasText && (
          <ChevronRight
            className={cn(
              "h-3.5 w-3.5 shrink-0 text-muted-foreground/45 transition-transform",
              open && "rotate-90",
            )}
            strokeWidth={2}
          />
        )}
      </button>

      {open && hasText && (
        <div className="mt-1.5 border-l border-border/60 pl-3">
          <MarkdownContent
            id={id}
            text={text}
            className="text-[14px] leading-relaxed text-muted-foreground"
            streaming={streaming}
          />
        </div>
      )}
    </div>
  )
}
