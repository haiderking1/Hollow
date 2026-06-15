import { useEffect, useState, useCallback } from "react"
import { Folder, ArrowUp, Home, CornerDownLeft } from "lucide-react"
import { cn } from "../lib/utils"

interface DirectoryPickerProps {
  open: boolean
  onClose: () => void
  onSelect: (path: string) => void
}

export function DirectoryPicker({ open, onClose, onSelect }: DirectoryPickerProps) {
  const [listing, setListing] = useState<DirListing | null>(null)
  const [loading, setLoading] = useState(false)

  const load = useCallback(async (path?: string) => {
    if (!window.flame?.listDir) return
    setLoading(true)
    const result = await window.flame.listDir(path)
    setListing(result)
    setLoading(false)
  }, [])

  // Load home when opened.
  useEffect(() => {
    if (open) load(undefined)
  }, [open, load])

  // Esc to close.
  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose()
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [open, onClose])

  if (!open) return null

  const current = listing?.path ?? ""

  return (
    <div className="fixed inset-0 z-[60] flex items-start justify-center pt-[12vh]">
      <div className="absolute inset-0 bg-black/50 backdrop-blur-[3px]" onClick={onClose} />
      <div className="relative z-10 flex max-h-[68vh] w-[560px] flex-col overflow-hidden rounded-xl border border-border bg-elevated shadow-2xl">
        {/* Header */}
        <div className="flex items-center justify-between border-b border-border px-4 py-3">
          <span className="text-[14px] font-semibold text-foreground">Add project</span>
          <div className="flex items-center gap-1">
            <button
              onClick={() => load(listing?.home)}
              className="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-surface-hover hover:text-foreground"
              aria-label="Home"
              title="Home"
            >
              <Home className="h-[16px] w-[16px]" strokeWidth={1.75} />
            </button>
            <button
              onClick={() => listing?.parent && load(listing.parent)}
              disabled={!listing?.parent}
              className="flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-surface-hover hover:text-foreground disabled:opacity-30"
              aria-label="Up"
              title="Parent folder"
            >
              <ArrowUp className="h-[16px] w-[16px]" strokeWidth={1.75} />
            </button>
          </div>
        </div>

        {/* Current path */}
        <div className="border-b border-border px-4 py-2">
          <div className="truncate rounded-md bg-surface px-2.5 py-1.5 font-mono text-[12px] text-muted-foreground">
            {current || "…"}
          </div>
        </div>

        {/* Folder list */}
        <div className="min-h-0 flex-1 overflow-y-auto p-2">
          {loading && (
            <div className="px-2 py-3 text-[13px] text-muted-foreground/60">Loading…</div>
          )}
          {!loading && listing?.error && (
            <div className="px-2 py-3 text-[13px] text-danger/80">Cannot open: {listing.error}</div>
          )}
          {!loading && listing && !listing.error && listing.entries.length === 0 && (
            <div className="px-2 py-3 text-[13px] text-muted-foreground/60">No subfolders here</div>
          )}
          {!loading &&
            listing?.entries.map((entry) => (
              <button
                key={entry.path}
                onDoubleClick={() => load(entry.path)}
                onClick={() => load(entry.path)}
                className={cn(
                  "flex w-full items-center gap-2.5 rounded-lg px-2.5 py-1.5 text-left text-[14px] text-foreground/90 transition-colors hover:bg-surface-hover",
                )}
              >
                <Folder
                  className="h-4 w-4 shrink-0 text-muted-foreground/75 fill-muted-foreground/5"
                  strokeWidth={2}
                />
                <span className="truncate">{entry.name}</span>
              </button>
            ))}
        </div>

        {/* Footer */}
        <div className="flex items-center justify-between gap-2 border-t border-border px-4 py-3">
          <span className="truncate text-[12px] text-muted-foreground/70">
            Open a folder, then add it as a project.
          </span>
          <div className="flex items-center gap-2">
            <button
              onClick={onClose}
              className="rounded-lg border border-border bg-surface px-3 py-1.5 text-[13px] text-muted-foreground transition-colors hover:bg-surface-hover hover:text-foreground"
            >
              Cancel
            </button>
            <button
              onClick={() => current && onSelect(current)}
              disabled={!current}
              className="flex items-center gap-1.5 rounded-lg bg-accent px-3 py-1.5 text-[13px] font-medium text-accent-foreground transition-opacity hover:opacity-90 disabled:opacity-40"
            >
              <CornerDownLeft className="h-3.5 w-3.5" strokeWidth={2.25} />
              Add this folder
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
