import { useState } from "react"
import { Sparkles, ChevronRight } from "lucide-react"
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
  const showOpen = open || (streaming && text.length > 0)
  return (
    <div>
      <button
        onClick={() => setOpen((o) => !o)}
        className="flex items-center gap-1.5 text-[13px] italic text-muted-foreground transition-colors hover:text-foreground"
      >
        <Sparkles className={cn("h-3.5 w-3.5 text-accent/70", streaming && "animate-pulse")} />
        <span>{streaming && !text ? "Thinking…" : "Thought for a few seconds"}</span>
        <ChevronRight className={cn("h-3.5 w-3.5 transition-transform", showOpen && "rotate-90")} />
      </button>
      {showOpen && text && (
        <div className="mt-2 border-l-2 border-border pl-3">
          <MarkdownContent
            id={id}
            text={text}
            className="text-[13px] italic text-muted-foreground"
            streaming={streaming}
          />
        </div>
      )}
    </div>
  )
}
