import { useState, useCallback, useEffect, useRef, useMemo } from "react"
import { PanelLeft } from "lucide-react"
import { Sidebar } from "./components/sidebar"
import { ChatView } from "./components/chat-view"
import { EmptyState } from "./components/empty-state"
import { PromptInput } from "./components/prompt-input"
import { StatusBar } from "./components/status-bar"
import TaskSidebar from "./components/TaskSidebar"
import TerminalPanel from "./components/TerminalPanel"
import SettingsPanel from "./components/SettingsPanel"
import { SearchModal } from "./components/SearchModal"
import { DirectoryPicker } from "./components/DirectoryPicker"
import type { Message, Block } from "./types"
import {
  type AgentEvent,
  type AgentModel,
  type AgentSessionInfo,
  mapAssistantContent,
  assistantContentFromEvent,
  mapMessages,
} from "./agent/rpc"

const applyZoom = (level: number) => {
  try {
    window.flame?.setZoom(level)
  } catch (err) {
    console.error("Failed to set zoom:", err)
  }
}

const send = (command: Record<string, unknown>) => window.flame?.agent.send(command)

function loadJSON<T>(key: string, fallback: T): T {
  try {
    const v = localStorage.getItem(key)
    return v ? (JSON.parse(v) as T) : fallback
  } catch {
    return fallback
  }
}

export default function App() {
  const [collapsed, setCollapsed] = useState(false)
  const [messages, setMessages] = useState<Message[]>([])
  const [isStreaming, setIsStreaming] = useState(false)
  const [terminalOpen] = useState(false)
  const [taskSidebarOpen, setTaskSidebarOpen] = useState(false)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [searchOpen, setSearchOpen] = useState(false)
  const [pickerOpen, setPickerOpen] = useState(false)
  const [loadingThread, setLoadingThread] = useState(false)
  /** True while the agent is catching up after we already showed cached messages. */
  const [syncingThread, setSyncingThread] = useState(false)

  // Live agent state
  const [model, setModel] = useState<AgentModel | null>(null)
  const [availableModels, setAvailableModels] = useState<AgentModel[]>([])
  const [sessionList, setSessionList] = useState<AgentSessionInfo[]>(() =>
    loadJSON<AgentSessionInfo[]>("flame-session-cache", []),
  )
  const [currentSessionId, setCurrentSessionId] = useState<string | null>(null)
  const [projectCwd, setProjectCwd] = useState<string | null>(null)
  const [addedProjects, setAddedProjects] = useState<string[]>(() =>
    loadJSON<string[]>("flame-projects", []),
  )
  // App-level (never on disk): display names + hidden threads.
  const [projectAliases, setProjectAliases] = useState<Record<string, string>>(() =>
    loadJSON("flame-project-names", {}),
  )
  const [threadAliases, setThreadAliases] = useState<Record<string, string>>(() =>
    loadJSON("flame-thread-names", {}),
  )
  const [hiddenThreads, setHiddenThreads] = useState<string[]>(() =>
    loadJSON<string[]>("flame-hidden-threads", []),
  )

  const [zoom, setZoom] = useState(() => {
    const saved = localStorage.getItem("flame-zoom")
    return saved ? parseFloat(saved) : 1.0
  })

  // Persist zoom changes
  useEffect(() => {
    localStorage.setItem("flame-zoom", zoom.toString())
    applyZoom(zoom)
  }, [zoom])

  // Global Ctrl/Cmd shortcuts (search & zoom)
  useEffect(() => {
    const canZoom = window.flame?.isElectron === true

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.ctrlKey || e.metaKey) {
        const key = e.key.toLowerCase()
        if (key === "k") {
          e.preventDefault()
          setSearchOpen((prev) => !prev)
        } else if (canZoom) {
          if (e.key === "=" || e.key === "+") {
            e.preventDefault()
            setZoom((prev) => Math.min(prev + 0.05, 2.5))
          } else if (e.key === "-") {
            e.preventDefault()
            setZoom((prev) => Math.max(prev - 0.05, 0.5))
          } else if (e.key === "0") {
            e.preventDefault()
            setZoom(1.0)
          }
        }
      }
    }

    const handleWheel = (e: WheelEvent) => {
      if (canZoom && (e.ctrlKey || e.metaKey)) {
        e.preventDefault()
        const zoomDelta = e.deltaY < 0 ? 0.05 : -0.05
        setZoom((prev) => Math.min(Math.max(prev + zoomDelta, 0.5), 2.5))
      }
    }

    window.addEventListener("keydown", handleKeyDown)
    window.addEventListener("wheel", handleWheel, { passive: false })

    return () => {
      window.removeEventListener("keydown", handleKeyDown)
      window.removeEventListener("wheel", handleWheel)
    }
  }, [])

  // Persist added projects + app-level aliases / hidden threads.
  useEffect(() => {
    localStorage.setItem("flame-projects", JSON.stringify(addedProjects))
  }, [addedProjects])

  useEffect(() => {
    localStorage.setItem("flame-project-names", JSON.stringify(projectAliases))
  }, [projectAliases])
  useEffect(() => {
    localStorage.setItem("flame-thread-names", JSON.stringify(threadAliases))
  }, [threadAliases])
  useEffect(() => {
    localStorage.setItem("flame-hidden-threads", JSON.stringify(hiddenThreads))
  }, [hiddenThreads])

  // Id of the assistant message currently being streamed by the agent.
  const streamingIdRef = useRef<string | null>(null)
  const messagesRef = useRef<Message[]>([])
  messagesRef.current = messages
  const currentSessionIdRef = useRef<string | null>(null)
  currentSessionIdRef.current = currentSessionId
  const messagesCacheRef = useRef(new Map<string, Message[]>())

  const addedProjectsRef = useRef(addedProjects)
  addedProjectsRef.current = addedProjects
  const projectCwdRef = useRef(projectCwd)
  projectCwdRef.current = projectCwd

  const stashMessagesInCache = useCallback((sessionId: string | null) => {
    if (sessionId && messagesRef.current.length > 0) {
      messagesCacheRef.current.set(sessionId, messagesRef.current)
    }
  }, [])

  const refreshSessionList = useCallback(() => {
    const cwds = new Set<string>(addedProjectsRef.current)
    if (projectCwdRef.current) cwds.add(projectCwdRef.current)
    if (cwds.size === 0) return
    send({ type: "list_sessions", cwds: Array.from(cwds) })
  }, [])

  // Refresh sidebar when projects are added/removed (not on every thread switch).
  useEffect(() => {
    if (!window.flame?.agent) return
    refreshSessionList()
  }, [addedProjects, refreshSessionList])

  const applyAssistantContent = useCallback((blocks: Block[]) => {
    const id = streamingIdRef.current
    if (!id) return
    setMessages((prev) =>
      prev.map((m) => (m.id === id && m.role === "assistant" ? { ...m, blocks } : m)),
    )
  }, [])

  // Subscribe to agent events + responses once.
  useEffect(() => {
    if (!window.flame?.agent) return
    const unsubscribe = window.flame.agent.onEvent((event: AgentEvent) => {
      switch (event.type) {
        case "bridge_ready": {
          // Pipe is open (possibly after respawning in a new cwd).
          if (event.cwd) {
            const cwd = event.cwd
            setProjectCwd(cwd)
            // Keep the active project registered so other projects don't vanish
            // when we switch into a new one.
            setAddedProjects((prev) => (prev.includes(cwd) ? prev : [...prev, cwd]))
          }
          send({ type: "get_available_models" })
          send({ type: "get_state" })
          send({ type: "get_messages" })
          // Defer sidebar refresh so the active thread can render first.
          window.setTimeout(() => refreshSessionList(), 50)
          break
        }
        case "message_update":
        case "turn_end": {
          const blocks = mapAssistantContent(assistantContentFromEvent(event))
          if (blocks.length) applyAssistantContent(blocks)
          break
        }
        case "agent_end": {
          const id = streamingIdRef.current
          if (id) {
            setMessages((prev) =>
              prev.map((m) =>
                m.id === id && m.role === "assistant" ? { ...m, streaming: false } : m,
              ),
            )
          }
          if (!event.willRetry) {
            streamingIdRef.current = null
            setIsStreaming(false)
            refreshSessionList()
          }
          break
        }
        case "session_info_changed": {
          refreshSessionList()
          break
        }
        case "response": {
          if (!event.success) break
          switch (event.command) {
            case "get_state": {
              setModel(event.data.model ?? null)
              setCurrentSessionId(event.data.sessionId)
              setIsStreaming(event.data.isStreaming)
              break
            }
            case "get_available_models":
              setAvailableModels(event.data.models)
              break
            case "list_sessions": {
              const sessions = event.data.sessions
              setSessionList(sessions)
              localStorage.setItem("flame-session-cache", JSON.stringify(sessions))
              break
            }
            case "get_messages": {
              const mapped = mapMessages(event.data.messages)
              setMessages(mapped)
              const sid = currentSessionIdRef.current
              if (sid) messagesCacheRef.current.set(sid, mapped)
              setLoadingThread(false)
              setSyncingThread(false)
              break
            }
            case "set_model":
              setModel(event.data)
              break
            case "switch_session":
            case "new_session": {
              // Session is live — fetch transcript. Skip list_sessions here; it
              // scans every project and was adding seconds to each switch.
              send({ type: "get_state" })
              send({ type: "get_messages" })
              break
            }
          }
          break
        }
        case "bridge_error":
        case "bridge_exit": {
          const msg =
            event.type === "bridge_error"
              ? `Agent error: ${event.error}`
              : `Agent process exited (code ${event.code ?? "unknown"}).`
          const id = streamingIdRef.current
          setMessages((prev) =>
            id
              ? prev.map((m) =>
                  m.id === id && m.role === "assistant"
                    ? { ...m, blocks: [{ type: "text", text: msg }], streaming: false }
                    : m,
                )
              : prev,
          )
          streamingIdRef.current = null
          setIsStreaming(false)
          break
        }
      }
    })
    return unsubscribe
  }, [applyAssistantContent, refreshSessionList])

  const handleSend = useCallback((content: string) => {
    const now = Date.now()
    const assistantId = `msg-${now + 1}`
    streamingIdRef.current = assistantId

    setMessages((prev) => [
      ...prev,
      { id: `msg-${now}`, role: "user", text: content },
      { id: assistantId, role: "assistant", blocks: [{ type: "text", text: "" }], streaming: true },
    ])
    setIsStreaming(true)
    send({ type: "prompt", message: content })
  }, [])

  const handleAbort = useCallback(() => {
    send({ type: "abort" })
    setIsStreaming(false)
    setMessages((prev) =>
      prev.map((m) => (m.role === "assistant" && m.streaming ? { ...m, streaming: false } : m)),
    )
  }, [])

  // switch_session rebinds the agent's cwd to the session's own cwd, so a
  // single warm agent handles every project — no slow respawn needed.
  const handleSelectSession = useCallback(
    (info: AgentSessionInfo) => {
      if (info.id === currentSessionId) return
      stashMessagesInCache(currentSessionId)
      streamingIdRef.current = null
      setCurrentSessionId(info.id)
      setProjectCwd(info.cwd)
      const cached = messagesCacheRef.current.get(info.id)
      if (cached) {
        setMessages(cached)
        setLoadingThread(false)
        setSyncingThread(true)
      } else {
        setMessages([])
        setLoadingThread(true)
        setSyncingThread(false)
      }
      if (!info.path) return
      send({ type: "switch_session", sessionPath: info.path })
    },
    [currentSessionId, stashMessagesInCache],
  )

  // Start a fresh thread inside a given project (new_session takes the cwd).
  const handleNewThread = useCallback((cwd: string) => {
    stashMessagesInCache(currentSessionId)
    streamingIdRef.current = null
    setCurrentSessionId(null)
    setProjectCwd(cwd)
    setMessages([])
    setLoadingThread(true)
    setSyncingThread(false)
    send({ type: "new_session", cwd })
  }, [currentSessionId, stashMessagesInCache])

  const handleRenameProject = useCallback((cwd: string, name: string) => {
    setProjectAliases((prev) => {
      const next = { ...prev }
      if (name.trim()) next[cwd] = name.trim()
      else delete next[cwd]
      return next
    })
  }, [])

  // Remove a project from the app only — never deletes the folder on disk.
  const handleDeleteProject = useCallback((cwd: string) => {
    setAddedProjects((prev) => prev.filter((p) => p !== cwd))
    setProjectAliases((prev) => {
      const next = { ...prev }
      delete next[cwd]
      return next
    })
  }, [])

  const handleRenameThread = useCallback((id: string, name: string) => {
    setThreadAliases((prev) => {
      const next = { ...prev }
      if (name.trim()) next[id] = name.trim()
      else delete next[id]
      return next
    })
  }, [])

  // Hide a thread from the app only — never deletes the session file on disk.
  const handleDeleteThread = useCallback(
    (id: string) => {
      setHiddenThreads((prev) => (prev.includes(id) ? prev : [...prev, id]))
      if (id === currentSessionId) {
        streamingIdRef.current = null
        setCurrentSessionId(null)
        setMessages([])
        setLoadingThread(true)
        send({ type: "new_session", cwd: projectCwd ?? undefined })
      }
    },
    [currentSessionId, projectCwd],
  )

  const handleAddProject = useCallback(() => setPickerOpen(true), [])

  // A directory was chosen: register it as a project and start a fresh thread
  // inside it (the agent respawns in that cwd).
  const handleProjectChosen = useCallback((dir: string) => {
    setPickerOpen(false)
    setAddedProjects((prev) => (prev.includes(dir) ? prev : [dir, ...prev]))
    streamingIdRef.current = null
    setCurrentSessionId(null)
    setProjectCwd(dir)
    setMessages([])
    setLoadingThread(true)
    send({ type: "new_session", cwd: dir })
  }, [])

  const handleSelectModel = useCallback((m: AgentModel) => {
    send({ type: "set_model", provider: m.provider, modelId: m.id })
  }, [])

  const current = sessionList.find((s) => s.id === currentSessionId)
  const currentCwd = current?.cwd ?? projectCwd ?? "~"
  const modelLabel = model?.name ?? "…"
  const showEmpty = messages.length === 0

  // Only projects the user explicitly added (plus the active one so the
  // current thread is always visible) — NOT every directory that happens to
  // have a past session.
  const projects = useMemo(() => {
    const set = new Set<string>()
    for (const p of addedProjects) set.add(p)
    if (projectCwd) set.add(projectCwd)
    return Array.from(set)
  }, [projectCwd, addedProjects])

  // Inject the active session as a synthetic entry while it has no on-disk
  // record yet (brand-new thread), so it shows under its project immediately.
  const sidebarSessions = useMemo(() => {
    const visible = sessionList.filter((s) => !hiddenThreads.includes(s.id))
    if (currentSessionId && projectCwd && !visible.some((s) => s.id === currentSessionId)) {
      const now = new Date().toISOString()
      const synthetic: AgentSessionInfo = {
        path: "",
        id: currentSessionId,
        cwd: projectCwd,
        name: "New session",
        created: now,
        modified: now,
        messageCount: 0,
        firstMessage: "",
      }
      return [synthetic, ...visible]
    }
    return visible
  }, [currentSessionId, projectCwd, sessionList, hiddenThreads])

  return (
    <div className="flex h-screen w-screen overflow-hidden bg-background text-foreground">
      {!collapsed && (
        <div className="flex shrink-0 flex-col bg-sidebar">
          <header className="app-drag flex h-11 shrink-0 items-center gap-3 bg-sidebar px-4 select-none">
            <div className="flex items-center gap-2">
              <span className="h-3 w-3 rounded-full bg-[#ff5f57]" />
              <span className="h-3 w-3 rounded-full bg-[#febc2e]" />
              <span className="h-3 w-3 rounded-full bg-[#28c840]" />
            </div>
            <div className="flex items-center gap-1 pl-2 text-muted-foreground">
              <button
                onClick={() => setCollapsed(true)}
                className="app-no-drag flex h-7 w-7 items-center justify-center rounded-md transition-colors hover:bg-surface-hover hover:text-foreground"
                aria-label="Collapse sidebar"
              >
                <PanelLeft className="h-[18px] w-[18px]" strokeWidth={1.75} />
              </button>
            </div>
          </header>
          <Sidebar
            sessions={sidebarSessions}
            projects={projects}
            activeId={currentSessionId}
            projectAliases={projectAliases}
            threadAliases={threadAliases}
            onSelect={handleSelectSession}
            onAddProject={handleAddProject}
            onNewThread={handleNewThread}
            onRenameProject={handleRenameProject}
            onDeleteProject={handleDeleteProject}
            onRenameThread={handleRenameThread}
            onDeleteThread={handleDeleteThread}
            onOpenSearch={() => setSearchOpen(true)}
          />
        </div>
      )}
      {collapsed && (
        <div className="app-drag fixed left-4 top-[8px] z-50 flex items-center gap-2">
          <span className="h-3 w-3 rounded-full bg-[#ff5f57]" />
          <span className="h-3 w-3 rounded-full bg-[#febc2e]" />
          <span className="h-3 w-3 rounded-full bg-[#28c840]" />
          <button
            onClick={() => setCollapsed(false)}
            className="app-no-drag ml-1 flex h-7 w-7 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-surface-hover hover:text-foreground"
            aria-label="Expand sidebar"
          >
            <PanelLeft className="h-[18px] w-[18px]" strokeWidth={1.75} />
          </button>
        </div>
      )}
      <main className="flex min-w-0 flex-1 overflow-hidden bg-background">
        <div className="flex min-w-0 flex-1 flex-col">
          <div className="relative flex min-h-0 flex-1 flex-col">
            {loadingThread ? (
              <div className="flex min-h-0 flex-1 items-center justify-center">
                <span className="block h-5 w-5 rounded-full border-2 border-muted-foreground/30 border-t-foreground animate-spin [animation-duration:0.9s]" />
              </div>
            ) : showEmpty ? (
              <EmptyState
                cwd={currentCwd}
                model={modelLabel}
                isStreaming={isStreaming}
                onSend={handleSend}
                onAbort={handleAbort}
                onToggleTasks={() => setTaskSidebarOpen((o) => !o)}
              />
            ) : (
              <>
                <ChatView messages={messages} />
                <div className="absolute bottom-0 left-0 right-0 pointer-events-none bg-gradient-to-t from-background via-background/95 to-transparent pt-10 pb-4">
                  <div className="w-full px-6 pb-2 pointer-events-auto">
                    <PromptInput onSend={handleSend} isStreaming={isStreaming} onAbort={handleAbort} />
                  </div>
                  <StatusBar
                    model={modelLabel}
                    isStreaming={isStreaming || syncingThread}
                    onToggleTasks={() => setTaskSidebarOpen((o) => !o)}
                  />
                </div>
              </>
            )}
          </div>
          <TerminalPanel open={terminalOpen} />
        </div>
        <TaskSidebar open={taskSidebarOpen} onClose={() => setTaskSidebarOpen(false)} />
      </main>

      <SettingsPanel
        open={settingsOpen}
        onClose={() => setSettingsOpen(false)}
        models={availableModels}
        currentModelId={model?.id ?? null}
        onSelectModel={handleSelectModel}
      />

      <SearchModal
        open={searchOpen}
        onClose={() => setSearchOpen(false)}
        onOpenSettings={() => setSettingsOpen(true)}
      />

      <DirectoryPicker
        open={pickerOpen}
        onClose={() => setPickerOpen(false)}
        onSelect={handleProjectChosen}
      />
    </div>
  )
}
