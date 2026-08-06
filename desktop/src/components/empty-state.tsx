import { FolderPlus } from "lucide-react"

interface EmptyStateProps {
  composer: React.ReactNode
  hasSelectedProject?: boolean
  onAddProject?: () => void
}

export function EmptyState({
  composer,
  hasSelectedProject = true,
  onAddProject,
}: EmptyStateProps) {
  return (
    <div className="relative flex min-h-0 flex-1 flex-col items-center justify-center">
      <div className="flex min-h-0 flex-1 flex-col items-center justify-center px-4 text-center">
        {!hasSelectedProject ? (
          <div className="flex flex-col items-center max-w-sm gap-3 select-none">
            <div className="flex h-12 w-12 items-center justify-center rounded-2xl border border-border-strong/40 bg-surface/60 text-muted-foreground shadow-sm">
              <FolderPlus className="h-6 w-6 stroke-[1.75]" />
            </div>
            <h2 className="text-lg font-semibold text-foreground">Select a project to start</h2>
            <p className="text-xs text-muted-foreground leading-relaxed">
              Choose an existing project folder or add a new one to start working with Hollow.
            </p>
            {onAddProject && (
              <button
                type="button"
                onClick={onAddProject}
                className="mt-1 inline-flex items-center gap-2 rounded-lg bg-accent px-3.5 py-1.5 text-xs font-medium text-accent-foreground transition-opacity hover:opacity-90 shadow-sm"
              >
                <FolderPlus className="h-3.5 w-3.5" strokeWidth={2} />
                <span>Add project folder</span>
              </button>
            )}
          </div>
        ) : null}
      </div>
      <div className="w-full max-w-[720px] px-6 pb-4">{composer}</div>
    </div>
  )
}
