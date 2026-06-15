import { Laptop, Folder } from "lucide-react"
import { PromptInput } from "./prompt-input"
import { StatusBar } from "./status-bar"

interface EmptyStateProps {
  cwd: string
  model?: string
  onSend?: (text: string) => void
  isStreaming?: boolean
  onAbort?: () => void
  onToggleTasks?: () => void
}

export function EmptyState({ cwd, model, onSend, isStreaming, onAbort, onToggleTasks }: EmptyStateProps) {
  return (
    <div className="relative flex min-h-0 flex-1 flex-col">
      <div className="flex-1" />

      <div className="w-full px-6">
        {/* context chips */}
        <div className="mb-3 flex flex-wrap items-center gap-2">
          <Chip icon={<Laptop className="h-4 w-4" strokeWidth={1.75} />} label="Local" />
          <Chip icon={<Folder className="h-4 w-4" strokeWidth={1.75} />} label={cwd} />
        </div>

        <PromptInput onSend={onSend} isStreaming={isStreaming} onAbort={onAbort} />
      </div>
      <div className="pt-3">
        <StatusBar model={model} isStreaming={isStreaming} onToggleTasks={onToggleTasks} />
      </div>
    </div>
  )
}

function Chip({ icon, label }: { icon: React.ReactNode; label: string }) {
  return (
    <button className="flex items-center gap-2 rounded-full border border-border-strong bg-surface px-3.5 py-2 text-[13.5px] text-foreground/90 transition-colors hover:bg-surface-hover">
      <span className="text-muted-foreground">{icon}</span>
      {label}
    </button>
  )
}
