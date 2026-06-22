import { useEffect, useRef, useState, useMemo, type ReactNode } from "react"
import {
  ArrowUp,
  ChevronDown,
  GitBranch,
  Mic,
  Monitor,
  Plus,
  Square,
} from "lucide-react"
import type { RepoStatus } from "../types"

interface PromptInputProps {
  onSend?: (text: string) => void
  isStreaming?: boolean
  onAbort?: () => void
  repoStatus?: RepoStatus | null
  loopStatus?: { active: boolean; iteration: number; maxIterations: number; task: string } | null
  /** Inline controls rendered inside the input row, left of the send/mic button. */
  footer?: ReactNode
  /** Open Settings → Models from the model picker "Add Models" row. */
  onOpenSettingsModels?: () => void
}

// ── Cursor composer (real screenshot) ─────────────────────────────────────────
// NO outer card. The composer IS one rounded dark pill/row. Status row sits
// directly underneath as plain icon+text.
// Use the app's actual CSS tokens so the composer matches the rest of Hollow.
// Slightly lighter than the page background so the row is visible but still dark.
// color-mix keeps it adaptive to any theme's --background.
const C = {
  rowBg: "color-mix(in srgb, var(--background) 92%, white)",
  rowBorder: "color-mix(in srgb, var(--foreground) 5%, transparent)",
  text: "var(--foreground)",
  muted: "var(--muted-foreground)",
  pillBorder: "var(--border-strong)",
  pillText: "var(--foreground)",
  added: "var(--success)",
  removed: "var(--danger)",
  actionBg: "var(--foreground)",
  actionFg: "var(--background)",
  toolIcon: "var(--icon-inactive)",
  ringTrack: "var(--border-strong)",
  ringFill: "var(--muted-foreground)",
}

const COMMANDS = [
  { name: "/loop", desc: "Run agent in a continuous outer-loop", usage: "/loop <task> [--max N]" },
  { name: "/loop-cancel", desc: "Cancel the active loop run", usage: "/loop-cancel" },
  { name: "/new", desc: "Start a fresh chat session in current directory", usage: "/new" },
  { name: "/skills", desc: "List discovered skills", usage: "/skills" },
  { name: "/workflows", desc: "List dynamic workflow runs", usage: "/workflows" },
]

export function PromptInput({
  onSend,
  isStreaming,
  onAbort,
  repoStatus,
  loopStatus,
  footer,
  onOpenSettingsModels,
}: PromptInputProps) {
  const [value, setValue] = useState("")
  const taRef = useRef<HTMLTextAreaElement>(null)
  const hasText = value.trim().length > 0

  const [selectedIndex, setSelectedIndex] = useState(0)

  // Compute if we should show the slash menu and get filtered commands list
  const showSlashMenu = value.startsWith("/") && !value.includes(" ")
  const filteredCommands = useMemo(() => {
    if (!showSlashMenu) return []
    const query = value.toLowerCase()
    return COMMANDS.filter((cmd) => cmd.name.startsWith(query))
  }, [value, showSlashMenu])

  // Reset selected command index when commands length changes
  useEffect(() => {
    setSelectedIndex(0)
  }, [filteredCommands.length])

  const submit = () => {
    const text = value.trim()
    if (!text || isStreaming) return
    onSend?.(text)
    setValue("")
  }

  useEffect(() => {
    const el = taRef.current
    if (!el) return
    el.style.height = "20px"
  }, [value])

  const inRepo = !!repoStatus && repoStatus.branch !== ""
  const changes = repoStatus ?? { added: 0, removed: 0, contextPct: 0 }

  return (
    <div className="relative w-full">
      {/* Autocomplete slash command popover menu */}
      {showSlashMenu && filteredCommands.length > 0 && (
        <div 
          className="absolute bottom-[54px] left-2 right-2 z-50 overflow-hidden rounded-xl border border-white/[0.08] bg-black/85 backdrop-blur-xl shadow-2xl transition-all"
        >
          <div className="max-h-60 overflow-y-auto p-1.5">
            {filteredCommands.map((cmd, idx) => {
              const active = idx === selectedIndex
              return (
                <button
                  key={cmd.name}
                  type="button"
                  onClick={() => setValue(cmd.name + " ")}
                  className={`flex w-full items-center justify-between rounded-lg px-3 py-2 text-left transition-colors ${
                    active ? "bg-white/[0.08] text-white" : "text-muted-foreground hover:bg-white/[0.03] hover:text-foreground"
                  }`}
                >
                  <div className="flex flex-col">
                    <span className="text-[14px] font-semibold font-mono">{cmd.name}</span>
                    <span className="text-[12px] opacity-70 mt-0.5">{cmd.desc}</span>
                  </div>
                  <span className="text-[11px] font-mono opacity-50 px-2 py-0.5 rounded bg-white/[0.05]">{cmd.usage}</span>
                </button>
              )
            })}
          </div>
        </div>
      )}

      {/* Top: Changes pill + Commit & Push (git repos only). */}
      {inRepo && (
        <div className="flex items-center gap-2 px-1 pb-2">
          <div
            className="flex items-center gap-1.5 rounded-full px-3 py-1.5 text-[13px] font-medium leading-none"
            style={{ border: `1px solid ${C.pillBorder}`, color: C.pillText }}
          >
            <span>Changes</span>
            <span style={{ color: C.added }}>+{changes.added}</span>
            <span style={{ color: C.removed }}>-{changes.removed}</span>
          </div>
          <button
            type="button"
            className="flex items-center gap-1.5 rounded-full px-3 py-1.5 text-[13px] font-medium leading-none transition-colors"
            style={{ border: `1px solid ${C.pillBorder}`, color: C.pillText }}
            onMouseEnter={(e) => {
              (e.currentTarget as HTMLElement).style.background =
                "rgba(255,255,255,0.05)"
            }}
            onMouseLeave={(e) => {
              (e.currentTarget as HTMLElement).style.background = "transparent"
            }}
          >
            <span>Commit &amp; Push</span>
            <ChevronDown size={12} strokeWidth={2} style={{ color: C.muted }} />
          </button>
        </div>
      )}

      {/* The composer IS this single rounded row. No outer card. */}
      <div
        className="flex items-center gap-2 rounded-full px-2"
        style={{ background: C.rowBg, border: `1px solid ${C.rowBorder}`, height: 42 }}
      >
        {/* Plus attachment. */}
        <button
          type="button"
          className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full text-muted-foreground transition-colors hover:text-foreground"
          style={{ color: C.toolIcon }}
          aria-label="Add attachment"
        >
          <Plus size={16} strokeWidth={2} />
        </button>

        <textarea
          ref={taRef}
          value={value}
          onChange={(e) => setValue(e.target.value)}
          onKeyDown={(e) => {
            if (showSlashMenu && filteredCommands.length > 0) {
              if (e.key === "ArrowDown") {
                e.preventDefault()
                setSelectedIndex((prev) => (prev + 1) % filteredCommands.length)
                return
              }
              if (e.key === "ArrowUp") {
                e.preventDefault()
                setSelectedIndex((prev) => (prev - 1 + filteredCommands.length) % filteredCommands.length)
                return
              }
              if (e.key === "Enter" || e.key === "Tab") {
                e.preventDefault()
                const selected = filteredCommands[selectedIndex]
                setValue(selected.name + " ")
                return
              }
              if (e.key === "Escape") {
                e.preventDefault()
                setValue("")
                return
              }
            }

            if (e.key === "Enter" && !e.shiftKey) {
              e.preventDefault()
              submit()
            }
          }}
          rows={1}
          placeholder="Send follow-up"
          className="min-w-0 flex-1 resize-none bg-transparent px-1 text-[15px] leading-tight outline-none"
          style={{ color: C.text, caretColor: C.text, height: 22 }}
        />

        {footer}

        {/* Stop / Send / Mic — always the same light circle. */}
        <button
          type="button"
          onClick={isStreaming ? () => onAbort?.() : submit}
          disabled={!isStreaming && !hasText}
          title={isStreaming ? "Stop" : hasText ? "Send" : "Voice input"}
          className="flex h-7 w-7 shrink-0 items-center justify-center rounded-full transition-all disabled:opacity-60"
          style={{ background: C.actionBg }}
        >
          {isStreaming ? (
            <Square size={11} fill={C.actionFg} stroke="none" />
          ) : hasText ? (
            <ArrowUp size={14} stroke={C.actionFg} strokeWidth={2.5} />
          ) : (
            <Mic size={14} stroke={C.actionFg} strokeWidth={2} />
          )}
        </button>
      </div>

      {/* Status row — plain text under the composer. */}
      <div className="flex items-center justify-between px-3 pt-2">
        <div className="flex items-center gap-4">
          {inRepo && (
            <div className="flex items-center gap-1.5 text-xs" style={{ color: C.muted }}>
              <GitBranch size={12} strokeWidth={1.8} />
              <span className="font-medium">{repoStatus!.branch}</span>
            </div>
          )}
          <div className="flex items-center gap-1 text-xs" style={{ color: C.muted }}>
            <Monitor size={12} strokeWidth={1.8} />
            <span className="font-medium">Local</span>
            <ChevronDown size={11} strokeWidth={2} />
          </div>

          {/* Looping Status Badge */}
          {loopStatus?.active && (
            <div className="flex items-center gap-2 rounded-full px-2 py-0.5 border border-amber-500/20 bg-amber-500/5 text-[11px] font-semibold text-amber-500 animate-pulse">
              <span className="relative flex h-1.5 w-1.5">
                <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-amber-400 opacity-75"></span>
                <span className="relative inline-flex rounded-full h-1.5 w-1.5 bg-amber-500"></span>
              </span>
              <span>
                LOOP ACTIVE (Iter {loopStatus.iteration}
                {loopStatus.maxIterations > 0 ? `/${loopStatus.maxIterations}` : ""})
              </span>
              <button
                type="button"
                onClick={() => onSend?.("/loop-cancel")}
                className="ml-1 text-[10px] font-bold underline hover:text-amber-400 opacity-80"
              >
                Cancel
              </button>
            </div>
          )}
        </div>
        {changes.contextPct > 0 && (
          <div className="flex items-center gap-1.5 text-xs" style={{ color: C.muted }}>
            <ContextRing pct={changes.contextPct} />
            <span className="font-medium">{changes.contextPct}%</span>
          </div>
        )}
      </div>
    </div>
  )
}

function ContextRing({ pct }: { pct: number }) {
  const r = 5.5
  const circ = 2 * Math.PI * r
  const dash = (pct / 100) * circ
  return (
    <svg width="14" height="14" viewBox="0 0 14 14" fill="none">
      <circle
        cx="7"
        cy="7"
        r={r}
        stroke={C.ringTrack}
        strokeWidth="1.6"
        fill="none"
      />
      <circle
        cx="7"
        cy="7"
        r={r}
        stroke={C.ringFill}
        strokeWidth="1.6"
        fill="none"
        strokeDasharray={`${dash} ${circ}`}
        strokeLinecap="round"
        transform="rotate(-90 7 7)"
      />
    </svg>
  )
}
