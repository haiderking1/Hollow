import { Archive as ArchiveIcon, RotateCcw, Trash2 } from "lucide-react"
import type { AgentSessionInfo } from "../../../agent/rpc"
import { EmptyState, SettingsCard } from "../controls"

export function Archive({
  hiddenThreads,
  sessions,
  threadAliases,
  onUnhide,
  onDelete,
}: {
  hiddenThreads: string[]
  sessions: AgentSessionInfo[]
  threadAliases: Record<string, string>
  onUnhide: (id: string) => void
  onDelete: (id: string) => void
}) {
  const archived = sessions.filter((s) => hiddenThreads.includes(s.id))

  if (archived.length === 0) {
    return (
      <EmptyState icon={<ArchiveIcon className="h-8 w-8" strokeWidth={1.5} />} title="No archived threads">
        Threads you hide from the sidebar will show up here.
      </EmptyState>
    )
  }

  return (
    <SettingsCard>
      {archived.map((s, i) => (
        <div
          key={s.id}
          className={`flex items-center justify-between gap-4 py-4 ${i < archived.length - 1 ? "border-b border-white/[0.06]" : ""}`}
        >
          <div className="min-w-0">
            <div className="truncate text-[14px] font-semibold text-white">
              {threadAliases[s.id] || s.name || s.firstMessage || "Untitled session"}
            </div>
            <div className="mt-0.5 truncate text-[12px] text-[#8E8E93]">{s.cwd}</div>
          </div>
          <div className="flex shrink-0 items-center gap-1.5">
            <button
              onClick={() => onUnhide(s.id)}
              title="Unhide"
              className="flex h-8 w-8 items-center justify-center rounded-lg border border-white/10 bg-[#1c1c1f] text-[#8E8E93] transition-colors hover:text-white"
            >
              <RotateCcw className="h-4 w-4" strokeWidth={2} />
            </button>
            <button
              onClick={() => onDelete(s.id)}
              title="Delete permanently"
              className="flex h-8 w-8 items-center justify-center rounded-lg border border-white/10 bg-[#1c1c1f] text-[#8E8E93] transition-colors hover:text-red-400"
            >
              <Trash2 className="h-4 w-4" strokeWidth={2} />
            </button>
          </div>
        </div>
      ))}
    </SettingsCard>
  )
}