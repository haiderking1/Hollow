import { useEffect, useRef, useState, type ReactNode } from "react"
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

export function PromptInput({
  onSend,
  isStreaming,
  onAbort,
  repoStatus,
  footer,
  onOpenSettingsModels,
}: PromptInputProps) {
  const [value, setValue] = useState("")
  const taRef = useRef<HTMLTextAreaElement>(null)
  const hasText = value.trim().length > 0

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
    <div className="w-full">
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
