import { useState } from "react"
import { Network, Check, X, CircleSlash, Loader2, ChevronRight } from "lucide-react"
import type { Block } from "../types"
import { cn } from "../lib/utils"

type ToolBlockType = Extract<Block, { type: "tool" }>

type SwarmAgent = {
  id: string
  status: "ok" | "error" | "aborted"
  turns: string
  worktree?: string
  body: string
}

type DoneState = {
  ok: number
  error: number
  aborted: number
  total: number
  goal: string
  agents: SwarmAgent[]
}

// While running, output is streamed delta lines like:
//   agent_swarm: planning subtasks…
//   agent_swarm: 1/2 agents finished
function parseProgress(output: string): { completed: number; total: number; planning: boolean } | null {
  let completed = 0
  let total = 0
  for (const l of output.split("\n")) {
    const m = l.match(/agent_swarm:\s*(\d+)\/(\d+)\s+agents finished/)
    if (m) {
      completed = parseInt(m[1], 10)
      total = parseInt(m[2], 10)
    }
  }
  const planning = /agent_swarm:\s*planning subtasks/.test(output)
  if (total > 0) return { completed, total, planning }
  if (planning) return { completed: 0, total: 0, planning: true }
  return null
}

// When done, output is the aggregated report:
//   Ran N agent(s) at concurrency C — X ok[, Y error][, Z aborted].
//   ## <id> [status] (turns[ ×N]) (worktree: … · branch: …)
//   <body>
function parseDone(output: string): DoneState {
  const res: DoneState = { ok: 0, error: 0, aborted: 0, total: 0, goal: "", agents: [] }
  const lines = output.split("\n")

  const goalLine = lines.find((l) => l.startsWith("Goal: "))
  if (goalLine) res.goal = goalLine.slice("Goal: ".length).trim()

  const header = lines.find((l) => l.startsWith("Ran ") && l.includes("agent(s)"))
  if (header) {
    const m = header.match(/Ran (\d+) agent\(s\).*—\s*(\d+)\s*ok(?:,\s*(\d+)\s*error)?(?:,\s*(\d+)\s*aborted)?/)
    if (m) {
      res.total = parseInt(m[1], 10)
      res.ok = parseInt(m[2], 10)
      res.error = parseInt(m[3] || "0", 10)
      res.aborted = parseInt(m[4] || "0", 10)
    }
  }

  const agents: SwarmAgent[] = []
  let cur: SwarmAgent | null = null
  const body: string[] = []
  for (const l of lines) {
    const m = l.match(/^## (.+?) \[(ok|error|aborted)\] \(([^)]+)\)(.*)$/)
    if (m) {
      if (cur) {
        cur.body = body.join("\n").trim()
        agents.push(cur)
      }
      const extra = m[4] || ""
      const wt = extra.match(/worktree:\s*(\S+)\s*·\s*branch:\s*([^\s)]+)/)
      cur = {
        id: m[1],
        status: m[2] as SwarmAgent["status"],
        turns: m[3],
        worktree: wt ? `${wt[1]} · ${wt[2]}` : undefined,
        body: "",
      }
      body.length = 0
    } else if (cur) {
      body.push(l)
    }
  }
  if (cur) {
    cur.body = body.join("\n").trim()
    agents.push(cur)
  }
  res.agents = agents
  if (res.total === 0 && agents.length > 0) res.total = agents.length
  return res
}

function StatusGlyph({ status }: { status: SwarmAgent["status"] }) {
  if (status === "ok") return <Check className="h-3.5 w-3.5 shrink-0 text-success" strokeWidth={2.5} />
  if (status === "error") return <X className="h-3.5 w-3.5 shrink-0 text-danger" strokeWidth={2.5} />
  return <CircleSlash className="h-3.5 w-3.5 shrink-0 text-muted-foreground" strokeWidth={2.5} />
}

function chipClass(status: SwarmAgent["status"] | "running" | "planning"): string {
  switch (status) {
    case "ok":
      return "border-success/40 bg-success/10 text-success"
    case "error":
      return "border-danger/40 bg-danger/10 text-danger"
    case "aborted":
      return "border-border bg-muted text-muted-foreground"
    case "running":
      return "border-warning/40 bg-warning/10 text-warning animate-pulse-dot"
    case "planning":
      return "border-info/40 bg-info/10 text-info animate-pulse-dot"
  }
}

export function SwarmAgentBlock({ block }: { block: ToolBlockType }) {
  const running = block.status === "running"
  const output = block.output ?? ""

  const [open, setOpen] = useState(running)

  const progress = running ? parseProgress(output) : null
  const done = !running ? parseDone(output) : null

  const total = progress?.total ?? done?.total ?? 0
  const completed = progress?.completed ?? done?.ok ?? 0
  const planning = progress?.planning ?? false

  const hasDetail = output.trim() !== ""
  const pct = total > 0 ? Math.min(100, (completed / total) * 100) : 0

  return (
    <div className="overflow-hidden rounded-lg border border-border bg-surface">
      {/* header */}
      <button
        type="button"
        disabled={!hasDetail}
        onClick={() => hasDetail && setOpen((o) => !o)}
        className={cn(
          "flex w-full items-center gap-2 px-3 py-2 text-left",
          hasDetail && "transition-colors hover:bg-surface-hover",
          !hasDetail && "cursor-default",
        )}
      >
        <Network
          className={cn(
            "h-4 w-4 shrink-0",
            running ? "text-info animate-pulse-dot" : "text-accent",
          )}
          strokeWidth={2.25}
        />
        <span className="shrink-0 text-[13px] font-semibold text-foreground">Swarm Agent</span>
        {block.title && block.title !== "agent_swarm" && (
          <span className="min-w-0 truncate font-mono text-[12px] text-muted-foreground" title={block.title}>
            {block.title}
          </span>
        )}
        <span className="ml-auto flex items-center gap-2">
          <span className="font-mono text-[11px] text-muted-foreground">
            {running
              ? total > 0
                ? `${completed}/${total}`
                : planning
                  ? "planning"
                  : "starting"
              : done
                ? `${done.ok}/${done.total} ok${done.error > 0 ? ` · ${done.error} err` : ""}${done.aborted > 0 ? ` · ${done.aborted} abort` : ""}`
                : ""}
          </span>
          {running ? (
            <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin text-muted-foreground" strokeWidth={2} />
          ) : done && done.error > 0 ? (
            <X className="h-3.5 w-3.5 shrink-0 text-danger" strokeWidth={2.5} />
          ) : (
            <Check className="h-3.5 w-3.5 shrink-0 text-success" strokeWidth={2.5} />
          )}
          {hasDetail && (
            <ChevronRight
              className={cn(
                "h-3.5 w-3.5 shrink-0 text-muted-foreground/45 transition-transform",
                open && "rotate-90",
              )}
              strokeWidth={2}
            />
          )}
        </span>
      </button>

      {/* progress bar while running */}
      {running && total > 0 && (
        <div className="h-[3px] w-full bg-muted">
          <div
            className="h-full bg-info transition-[width] duration-300 ease-out"
            style={{ width: `${pct}%` }}
          />
        </div>
      )}

      {/* agent chips */}
      {total > 0 && (
        <div className="flex flex-wrap gap-1.5 px-3 py-2">
          {done
            ? done.agents.map((a, i) => (
                <span
                  key={i}
                  className={cn(
                    "inline-flex items-center gap-1 rounded-md border px-1.5 py-0.5 font-mono text-[11px]",
                    chipClass(a.status),
                  )}
                >
                  <StatusGlyph status={a.status} />
                  {a.id}
                </span>
              ))
            : Array.from({ length: total }).map((_, i) => (
                <span
                  key={i}
                  className={cn(
                    "inline-flex items-center gap-1 rounded-md border px-1.5 py-0.5 font-mono text-[11px]",
                    chipClass(i < completed ? "ok" : "running"),
                  )}
                >
                  {i < completed ? (
                    <Check className="h-3 w-3 shrink-0" strokeWidth={2.5} />
                  ) : (
                    <Loader2 className="h-3 w-3 shrink-0 animate-spin" strokeWidth={2} />
                  )}
                  {`agent-${i + 1}`}
                </span>
              ))}
        </div>
      )}
      {running && total === 0 && planning && (
        <div className="flex flex-wrap gap-1.5 px-3 py-2">
          <span className={cn("inline-flex items-center gap-1 rounded-md border px-1.5 py-0.5 font-mono text-[11px]", chipClass("planning"))}>
            <Loader2 className="h-3 w-3 shrink-0 animate-spin" strokeWidth={2} />
            planning subtasks…
          </span>
        </div>
      )}

      {/* expandable detail */}
      {open && hasDetail && (
        <div className="border-t border-border/60 px-3 py-2">
          {done && done.goal !== "" && (
            <div className="mb-2 text-[12px] text-muted-foreground">
              <span className="text-foreground/80">Goal:</span> {done.goal}
            </div>
          )}
          {done && done.agents.length > 0 ? (
            <div className="space-y-2">
              {done.agents.map((a, i) => (
                <div key={i} className="rounded-md border border-border/60 bg-background/40">
                  <div className="flex items-center gap-2 px-2.5 py-1.5">
                    <StatusGlyph status={a.status} />
                    <span className="font-mono text-[12px] font-medium text-foreground">{a.id}</span>
                    <span className="font-mono text-[11px] text-muted-foreground">{a.turns}</span>
                    {a.worktree && (
                      <span className="min-w-0 truncate font-mono text-[11px] text-muted-foreground/80" title={a.worktree}>
                        {a.worktree}
                      </span>
                    )}
                  </div>
                  {a.body !== "" && (
                    <pre
                      className={cn(
                        "overflow-x-auto whitespace-pre-wrap border-t border-border/40 px-2.5 py-1.5 font-mono text-[12px] leading-relaxed",
                        a.status === "error" ? "text-danger/90" : "text-muted-foreground",
                      )}
                    >
                      {a.body}
                    </pre>
                  )}
                </div>
              ))}
            </div>
          ) : (
            <pre className="overflow-x-auto whitespace-pre-wrap font-mono text-[12px] leading-relaxed text-muted-foreground">
              {output}
            </pre>
          )}
        </div>
      )}
    </div>
  )
}