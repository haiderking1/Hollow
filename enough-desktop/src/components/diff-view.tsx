import type { Diff } from "../types"

export function DiffView({ diff }: { diff: Diff }) {
  return (
    <div className="mt-2 overflow-hidden rounded-lg border border-border bg-background">
      <div className="flex items-center justify-between border-b border-border bg-muted px-3 py-1.5">
        <span className="font-mono text-[12px] text-foreground/80">{diff.file}</span>
        <span className="flex items-center gap-2 font-mono text-[11px]">
          <span className="text-add">+{diff.added}</span>
          <span className="text-del">-{diff.removed}</span>
        </span>
      </div>
      <pre className="overflow-x-auto py-1 font-mono text-[12px] leading-[1.6]">
        {diff.lines.map((line, i) => (
          <div
            key={i}
            className={
              line.type === "add"
                ? "bg-add-bg px-3"
                : line.type === "remove"
                  ? "bg-del-bg px-3"
                  : "px-3"
            }
          >
            <span
              className={
                line.type === "add"
                  ? "select-none text-add"
                  : line.type === "remove"
                    ? "select-none text-del"
                    : "select-none text-muted-foreground"
              }
            >
              {line.type === "add" ? "+ " : line.type === "remove" ? "- " : "  "}
            </span>
            <span
              className={
                line.type === "add"
                  ? "text-add"
                  : line.type === "remove"
                    ? "text-del"
                    : "text-foreground/70"
              }
            >
              {line.text || " "}
            </span>
          </div>
        ))}
      </pre>
    </div>
  )
}
