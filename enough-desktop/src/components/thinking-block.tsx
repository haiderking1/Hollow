import { useState } from "react"
import { Sparkles, ChevronRight } from "lucide-react"
import { cn } from "../lib/utils"

export function ThinkingBlock({ text }: { text: string }) {
  const [open, setOpen] = useState(false)
  return (
    <div>
      <button
        onClick={() => setOpen((o) => !o)}
        className="flex items-center gap-1.5 text-[13px] italic text-muted-foreground transition-colors hover:text-foreground"
      >
        <Sparkles className="h-3.5 w-3.5 text-accent/70" />
        <span>Thought for a few seconds</span>
        <ChevronRight className={cn("h-3.5 w-3.5 transition-transform", open && "rotate-90")} />
      </button>
      {open && (
        <p className="mt-2 border-l-2 border-border pl-3 text-[13px] italic leading-relaxed text-muted-foreground">
          {text}
        </p>
      )}
    </div>
  )
}
