import { useEffect, useRef } from "react"
import { Search, SquarePen, FolderPlus, Settings, MessageSquare, ArrowUp, ArrowDown } from "lucide-react"

interface SearchModalProps {
  open: boolean
  onClose: () => void
  onOpenSettings: () => void
}

export function SearchModal({ open, onClose, onOpenSettings }: SearchModalProps) {
  const inputRef = useRef<HTMLInputElement>(null)

  // Focus input on open
  useEffect(() => {
    if (open) {
      setTimeout(() => {
        inputRef.current?.focus()
      }, 50)
    }
  }, [open])

  // Handle escape key
  useEffect(() => {
    if (!open) return
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        onClose()
      }
    }
    window.addEventListener("keydown", handleKeyDown)
    return () => window.removeEventListener("keydown", handleKeyDown)
  }, [open, onClose])

  if (!open) return null

  return (
    <div
      className="fixed inset-0 z-50 flex items-start justify-center bg-background/60 backdrop-blur-sm pt-[15vh]"
      onClick={onClose}
    >
      <div
        className="w-full max-w-[560px] overflow-hidden rounded-xl border border-border-strong bg-[#181816] shadow-2xl"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Search Input Bar */}
        <div className="flex items-center gap-3 border-b border-border/60 px-4 py-3">
          <Search className="h-4.5 w-4.5 text-muted-foreground/85" strokeWidth={2.25} />
          <input
            ref={inputRef}
            type="text"
            placeholder="Search commands, projects, and threads..."
            className="w-full bg-transparent text-[14px] text-foreground placeholder:text-muted-foreground focus:outline-none"
          />
        </div>

        {/* List Content */}
        <div className="max-h-[360px] overflow-y-auto py-2">
          {/* Actions Section */}
          <div className="flex flex-col">
            <div className="px-3.5 py-1.5 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/50">
              Actions
            </div>
            
            <button className="flex items-center justify-between px-3.5 py-2.5 text-left text-[14px] text-foreground hover:bg-surface-hover/60 transition-colors group">
              <div className="flex items-center gap-3">
                <SquarePen className="h-4 w-4 text-muted-foreground/80 group-hover:text-foreground" strokeWidth={2} />
                <span>New thread in...</span>
              </div>
              <span className="text-muted-foreground/40 text-[12px] group-hover:text-foreground/80">&gt;</span>
            </button>

            <button className="flex items-center justify-between px-3.5 py-2.5 text-left text-[14px] text-foreground hover:bg-surface-hover/60 transition-colors group">
              <div className="flex items-center gap-3">
                <FolderPlus className="h-4 w-4 text-muted-foreground/80 group-hover:text-foreground" strokeWidth={2} />
                <span>Add project</span>
              </div>
            </button>

            <button
              onClick={() => {
                onOpenSettings()
                onClose()
              }}
              className="flex items-center justify-between px-3.5 py-2.5 text-left text-[14px] text-foreground bg-surface-hover/40 hover:bg-surface-hover/70 transition-colors group"
            >
              <div className="flex items-center gap-3">
                <Settings className="h-4 w-4 text-muted-foreground/90 group-hover:text-foreground" strokeWidth={2} />
                <span>Open settings</span>
              </div>
            </button>
          </div>

          {/* Divider */}
          <div className="h-px bg-border/40 my-2" />

          {/* Recent Threads Section */}
          <div className="flex flex-col">
            <div className="px-3.5 py-1.5 text-[11px] font-semibold uppercase tracking-wider text-muted-foreground/50">
              Recent Threads
            </div>

            <button className="flex items-center justify-between px-3.5 py-2.5 text-left text-[14px] hover:bg-surface-hover/60 transition-colors group">
              <div className="flex items-center gap-3">
                <MessageSquare className="h-4 w-4 text-muted-foreground/80 group-hover:text-foreground" strokeWidth={2} />
                <div className="flex flex-col">
                  <span className="text-foreground group-hover:text-foreground">Clarify request</span>
                  <span className="text-[12px] text-muted-foreground/60">test &bull; #main</span>
                </div>
              </div>
              <span className="text-[12px] text-muted-foreground/40 shrink-0">4d ago</span>
            </button>
          </div>
        </div>

        {/* Bottom Toolbar */}
        <div className="flex items-center gap-4 border-t border-border/40 bg-sidebar/20 px-4 py-2.5 text-[11px] text-muted-foreground/60 select-none">
          <div className="flex items-center gap-1">
            <kbd className="inline-flex h-4.5 items-center justify-center rounded bg-surface-hover px-1 text-[10px] font-mono text-muted-foreground/90 border border-border/60"><ArrowUp className="h-3 w-3" /></kbd>
            <kbd className="inline-flex h-4.5 items-center justify-center rounded bg-surface-hover px-1 text-[10px] font-mono text-muted-foreground/90 border border-border/60"><ArrowDown className="h-3 w-3" /></kbd>
            <span className="ml-1">Navigate</span>
          </div>

          <div className="flex items-center gap-1">
            <kbd className="inline-flex h-4.5 items-center justify-center rounded bg-surface-hover px-1 text-[10px] font-mono text-muted-foreground/90 border border-border/60 font-semibold">Enter</kbd>
            <span className="ml-1">Select</span>
          </div>

          <div className="flex items-center gap-1">
            <kbd className="inline-flex h-4.5 items-center justify-center rounded bg-surface-hover px-1 text-[10px] font-mono text-muted-foreground/90 border border-border/60 font-semibold">Esc</kbd>
            <span className="ml-1">Close</span>
          </div>
        </div>
      </div>
    </div>
  )
}
