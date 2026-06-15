import { ListChecks, Plus, ChevronDown } from "lucide-react"

interface StatusBarProps {
  model?: string
  isStreaming?: boolean
  onToggleTasks?: () => void
}

export function StatusBar({ model, isStreaming = false, onToggleTasks }: StatusBarProps) {
  return (
    <footer className="flex w-full items-center justify-between px-6 pb-4 select-none">
      <div className="flex items-center gap-1 text-muted-foreground">
        <IconButton aria-label="Tasks" onClick={onToggleTasks}><ListChecks className="h-[18px] w-[18px]" strokeWidth={1.75} /></IconButton>
        <IconButton aria-label="Attach"><Plus className="h-[18px] w-[18px]" strokeWidth={1.75} /></IconButton>
        <IconButton aria-label="More"><ChevronDown className="h-4 w-4" strokeWidth={1.75} /></IconButton>
      </div>
      <div className="flex items-center gap-2.5">
        <span className="text-[14px] text-foreground/90">{model}</span>
        {isStreaming && (
          <span className="block h-3.5 w-3.5 rounded-full border-[2px] border-muted-foreground/30 border-t-foreground animate-spin [animation-duration:1.2s]" />
        )}
      </div>
    </footer>
  )
}

function IconButton({ children, ...props }: React.ComponentProps<"button">) {
  return (
    <button
      className="flex h-7 w-7 items-center justify-center rounded-md transition-colors hover:bg-surface-hover hover:text-foreground"
      {...props}
    >
      {children}
    </button>
  )
}
