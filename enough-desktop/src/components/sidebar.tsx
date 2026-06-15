import { useMemo, useState } from "react"
import {
  FolderPlus,
  ChevronDown,
  ChevronRight,
  Folder,
  Search,
  Plus,
  Pencil,
  Trash2,
} from "lucide-react"
import type { AgentSessionInfo } from "../agent/rpc"

interface SidebarProps {
  sessions: AgentSessionInfo[]
  projects: string[]
  activeId: string | null
  projectAliases: Record<string, string>
  threadAliases: Record<string, string>
  onSelect: (session: AgentSessionInfo) => void
  onAddProject: () => void
  onNewThread: (cwd: string) => void
  onRenameProject: (cwd: string, name: string) => void
  onDeleteProject: (cwd: string) => void
  onRenameThread: (id: string, name: string) => void
  onDeleteThread: (id: string) => void
  onOpenSearch: () => void
}

function projectName(cwd: string): string {
  if (!cwd) return "(unknown)"
  const parts = cwd.replace(/\/+$/, "").split("/")
  return parts[parts.length - 1] || cwd
}

function relTime(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime()
  const m = Math.floor(diff / 60000)
  if (m < 1) return "now"
  if (m < 60) return `${m}m ago`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h ago`
  const d = Math.floor(h / 24)
  if (d < 7) return `${d}d ago`
  return new Date(iso).toLocaleDateString()
}

interface Group {
  cwd: string
  name: string
  sessions: AgentSessionInfo[]
  latest: number
}

function IconBtn({ children, ...props }: React.ComponentProps<"button">) {
  return (
    <button
      {...props}
      className="flex h-5 w-5 items-center justify-center rounded text-muted-foreground/70 transition-colors hover:bg-surface-hover hover:text-foreground"
    >
      {children}
    </button>
  )
}

export function Sidebar({
  sessions,
  projects,
  activeId,
  projectAliases,
  threadAliases,
  onSelect,
  onAddProject,
  onNewThread,
  onRenameProject,
  onDeleteProject,
  onRenameThread,
  onDeleteThread,
  onOpenSearch,
}: SidebarProps) {
  const groups = useMemo<Group[]>(() => {
    const byCwd = new Map<string, AgentSessionInfo[]>()
    for (const p of projects) byCwd.set(p, [])
    for (const s of sessions) {
      const arr = byCwd.get(s.cwd)
      if (!arr) continue
      arr.push(s)
    }
    const result: Group[] = []
    // Preserve the given project order (stable) — only sort threads within.
    for (const [cwd, arr] of byCwd) {
      arr.sort((a, b) => new Date(b.modified).getTime() - new Date(a.modified).getTime())
      result.push({
        cwd,
        name: projectAliases[cwd] ?? projectName(cwd),
        sessions: arr,
        latest: arr.length ? new Date(arr[0].modified).getTime() : 0,
      })
    }
    return result
  }, [sessions, projects, projectAliases])

  const activeCwd = sessions.find((s) => s.id === activeId)?.cwd
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({})
  const isOpen = (cwd: string, index: number) =>
    collapsed[cwd] === undefined ? cwd === activeCwd || index === 0 : !collapsed[cwd]

  // Inline rename state.
  const [editing, setEditing] = useState<{ kind: "project" | "thread"; key: string } | null>(null)
  const [draft, setDraft] = useState("")

  const startEdit = (kind: "project" | "thread", key: string, current: string) => {
    setEditing({ kind, key })
    setDraft(current)
  }
  const commitEdit = () => {
    if (!editing) return
    if (editing.kind === "project") onRenameProject(editing.key, draft)
    else onRenameThread(editing.key, draft)
    setEditing(null)
  }

  const renameInput = (
    <input
      autoFocus
      value={draft}
      onChange={(e) => setDraft(e.target.value)}
      onClick={(e) => e.stopPropagation()}
      onKeyDown={(e) => {
        if (e.key === "Enter") commitEdit()
        else if (e.key === "Escape") setEditing(null)
      }}
      onBlur={commitEdit}
      className="min-w-0 flex-1 rounded border border-accent/60 bg-background px-1.5 py-0.5 text-[13.5px] text-foreground outline-none"
    />
  )

  return (
    <aside className="flex w-[280px] shrink-0 flex-col bg-sidebar px-2.5 pt-4">
      {/* Search */}
      <div className="pb-4">
        <button
          onClick={onOpenSearch}
          className="flex w-full items-center justify-between rounded-lg border border-border-strong/40 bg-surface/40 px-2.5 py-1.5 text-left text-muted-foreground transition-all hover:bg-surface-hover hover:text-foreground"
        >
          <div className="flex items-center gap-2">
            <Search className="h-4 w-4 text-muted-foreground/80" strokeWidth={2.25} />
            <span className="text-[13px] font-medium">Search</span>
          </div>
          <kbd className="pointer-events-none inline-flex h-4.5 select-none items-center rounded border border-border-strong/85 bg-surface px-1.5 font-mono text-[9px] font-semibold text-muted-foreground/75">
            Ctrl+K
          </kbd>
        </button>
      </div>

      {/* Projects header */}
      <div className="flex items-center justify-between px-2.5 pb-3">
        <span className="text-[11px] font-semibold tracking-wider text-muted-foreground/80 uppercase select-none">
          Projects
        </span>
        <button
          onClick={onAddProject}
          className="flex h-5.5 w-5.5 items-center justify-center rounded text-muted-foreground/60 transition-colors hover:bg-surface-hover hover:text-foreground"
          aria-label="Add project"
          title="Add project"
        >
          <FolderPlus className="h-3.5 w-3.5" strokeWidth={2} />
        </button>
      </div>

      {/* Grouped sessions */}
      <div className="flex flex-col gap-0.5 overflow-y-auto pb-4">
        {groups.length === 0 && (
          <div className="px-2.5 py-2 text-[13px] text-muted-foreground/60">
            No projects yet — click + to add one
          </div>
        )}
        {groups.map((group, i) => {
          const open = isOpen(group.cwd, i)
          const editingProject = editing?.kind === "project" && editing.key === group.cwd
          return (
            <div key={group.cwd} className="flex flex-col">
              <div className="group flex items-center gap-1 rounded-lg px-2 py-1.5 text-[14px] font-medium text-foreground hover:bg-surface-hover/50">
                <button
                  onClick={() => !editingProject && setCollapsed((c) => ({ ...c, [group.cwd]: open }))}
                  className="flex min-w-0 flex-1 items-center gap-2 text-left select-none"
                  title={group.cwd}
                >
                  {open ? (
                    <ChevronDown className="h-4 w-4 shrink-0 text-muted-foreground/75" strokeWidth={2} />
                  ) : (
                    <ChevronRight className="h-4 w-4 shrink-0 text-muted-foreground/75" strokeWidth={2} />
                  )}
                  <Folder
                    className="h-4 w-4 shrink-0 text-muted-foreground/75 fill-muted-foreground/5"
                    strokeWidth={2}
                  />
                  {editingProject ? renameInput : <span className="truncate">{group.name}</span>}
                </button>
                {!editingProject && (
                  <div className="flex items-center gap-0.5 opacity-0 group-hover:opacity-100">
                    <IconBtn onClick={() => onNewThread(group.cwd)} aria-label="New thread" title="New thread">
                      <Plus className="h-3.5 w-3.5" strokeWidth={2} />
                    </IconBtn>
                    <IconBtn
                      onClick={() => startEdit("project", group.cwd, group.name)}
                      aria-label="Rename project"
                      title="Rename"
                    >
                      <Pencil className="h-3.5 w-3.5" strokeWidth={2} />
                    </IconBtn>
                    <IconBtn
                      onClick={() => onDeleteProject(group.cwd)}
                      aria-label="Remove project"
                      title="Remove project (deletes its threads)"
                    >
                      <Trash2 className="h-3.5 w-3.5" strokeWidth={2} />
                    </IconBtn>
                  </div>
                )}
              </div>

              {open && (
                <div className="relative ml-[15px] pl-[17px] border-l border-border/40 my-0.5 flex flex-col gap-0.5">
                  {group.sessions.length === 0 && (
                    <div className="px-2.5 py-1.5 text-[12.5px] text-muted-foreground/40 select-none">
                      No threads
                    </div>
                  )}
                  {group.sessions.map((s) => {
                    const label = threadAliases[s.id] || s.name || s.firstMessage || "Untitled session"
                    const isActive = s.id === activeId
                    const editingThread = editing?.kind === "thread" && editing.key === s.id
                    return (
                      <div
                        key={s.id}
                        className={`group flex items-center justify-between rounded-lg py-1.5 px-2.5 text-[14px] ${
                          isActive ? "bg-surface-hover text-foreground" : "text-foreground hover:bg-surface-hover/30"
                        }`}
                      >
                        <button
                          onClick={() => !editingThread && onSelect(s)}
                          className="flex min-w-0 flex-1 items-center text-left"
                        >
                          {editingThread ? (
                            renameInput
                          ) : (
                            <span className="truncate text-foreground/90 group-hover:text-foreground">
                              {label}
                            </span>
                          )}
                        </button>
                        {!editingThread && (
                          <div className="ml-2 flex shrink-0 items-center">
                            <span className="text-[11.5px] text-muted-foreground/50 group-hover:hidden">
                              {relTime(s.modified)}
                            </span>
                            <div className="hidden items-center gap-0.5 group-hover:flex">
                              <IconBtn
                                onClick={() => startEdit("thread", s.id, label)}
                                aria-label="Rename thread"
                                title="Rename"
                              >
                                <Pencil className="h-3.5 w-3.5" strokeWidth={2} />
                              </IconBtn>
                              <IconBtn
                                onClick={() => onDeleteThread(s.id)}
                                aria-label="Delete thread"
                                title="Delete thread"
                              >
                                <Trash2 className="h-3.5 w-3.5" strokeWidth={2} />
                              </IconBtn>
                            </div>
                          </div>
                        )}
                      </div>
                    )
                  })}
                </div>
              )}
            </div>
          )
        })}
      </div>
    </aside>
  )
}
