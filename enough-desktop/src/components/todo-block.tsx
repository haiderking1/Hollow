import { Check, CircleDot, Circle } from "lucide-react"
import type { TodoItem } from "../types"
import { cn } from "../lib/utils"

export function TodoBlock({ items }: { items: TodoItem[] }) {
  const done = items.filter((i) => i.status === "done").length
  return (
    <div className="overflow-hidden rounded-lg border border-border bg-surface">
      <div className="flex items-center justify-between border-b border-border px-3 py-2">
        <span className="text-[12px] font-semibold uppercase tracking-wider text-muted-foreground">
          Todos
        </span>
        <span className="font-mono text-[11px] text-muted-foreground">
          {done}/{items.length}
        </span>
      </div>
      <ul className="divide-y divide-border">
        {items.map((item, i) => (
          <li key={i} className="flex items-center gap-2.5 px-3 py-1.5">
            {item.status === "done" ? (
              <Check className="h-3.5 w-3.5 shrink-0 text-success" />
            ) : item.status === "in_progress" ? (
              <CircleDot className="h-3.5 w-3.5 shrink-0 animate-pulse-dot text-warning" />
            ) : (
              <Circle className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
            )}
            <span
              className={cn(
                "text-[13px]",
                item.status === "done"
                  ? "text-muted-foreground line-through"
                  : item.status === "in_progress"
                    ? "text-foreground"
                    : "text-foreground/70",
              )}
            >
              {item.text}
            </span>
          </li>
        ))}
      </ul>
    </div>
  )
}
