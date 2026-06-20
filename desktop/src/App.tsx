import { useState, useCallback, useEffect, useRef, useMemo } from "react"
import { PanelLeft } from "lucide-react"
import { Sidebar } from "./components/sidebar"
import { ChatWorkspace } from "./components/chat-workspace"
import TaskSidebar from "./components/TaskSidebar"
import TerminalPanel from "./components/TerminalPanel"
import SettingsPanel from "./components/SettingsPanel"
import { SearchModal } from "./components/SearchModal"
import { DirectoryPicker } from "./components/DirectoryPicker"
import type { Message, Block } from "./types"
import { hollowAgent } from "./agent/hollowClient"
import {
  type AgentEvent,
  type AgentModel,
  type AgentSessionInfo,
  type CodexLoginState,
  type ConnectionInfo,
  type ModelCatalog,
  mapAssistantContent,
  assistantContentFromEvent,
  mapMessages,
} from "./agent/rpc"

import { bumpZoom, initZoom, resetZoom, ZOOM_STEP } from "./lib/zoom"

const send = (command: Record<string, unknown>) => hollowAgent.send(command)

function loadJSON<T>(key: string, fallback: T): T {
  try {
    const v = localStorage.getItem(key)
    return v ? (JSON.parse(v) as T) : fallback
  } catch {
    return fallback
  }
}

function upsertActiveSession(
  prev: AgentSessionInfo[],
  sessionId: string,
  cwd: string,
): AgentSessionInfo[] {
  if (prev.some((s) => s.id === sessionId || s.path === sessionId)) return prev
  const now = new Date().toISOString()
  return [
    {
      path: "",
      id: sessionId,
      cwd,
      name: "New session",
      created: now,
      modified: now,
      messageCount: 0,
      firstMessage: "",
    },
    ...prev,
  ]
}

function mergeSessionList(
  incoming: AgentSessionInfo[],
  prev: AgentSessionInfo[],
  activeId: string | null,
): AgentSessionInfo[] {
  if (!activeId) return incoming
  if (incoming.some((s) => s.id === activeId || s.path === activeId)) return incoming
  const optimistic = prev.find((s) => s.id === activeId || s.path === activeId)
  return optimistic ? [optimistic, ...incoming] : incoming
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
  // Settings: provider connection state + Codex login flow + in-panel errors.
  const [connections, setConnections] = useState<ConnectionInfo[]>([])
  const [codexLogin, setCodexLogin] = useState<CodexLoginState | null>(null)
  const [settingsError, setSettingsError] = useState<string | null>(null)
  const [loadingThread, setLoadingThread] = useState(false)
  /** True while the agent is catching up after we already showed cached messages. */
  const [syncingThread, setSyncingThread] = useState(false)

  // Live agent state
  const [model, setModel] = useState<AgentModel | null>(null)
  const [modelCatalog, setModelCatalog] = useState<ModelCatalog | null>(null)
  const [availableModels, setAvailableModels] = useState<AgentModel[]>([])
  const [sessionList, setSessionList] = useState<AgentSessionInfo[]>(() =>
    loadJSON<AgentSessionInfo[]>("hollow-session-cache", []),
  )
  const [currentSessionId, setCurrentSessionId] = useState<string | null>(null)
  const [projectCwd, setProjectCwd] = useState<string | null>(null)
  const [addedProjects, setAddedProjects] = useState<string[]>(() =>
    loadJSON<string[]>("hollow-projects", []),
  )
  // App-level (never on disk): display names + hidden threads.
  const [projectAliases, setProjectAliases] = useState<Record<string, string>>(() =>
    ({
      ...Object.fromEntries(
        sessionList.map((session) => [
          session.cwd,
          session.cwd.replace(/\/+$/, "").split("/").pop() || session.cwd,
        ]),
      ),
      ...loadJSON<Record<string, string>>("hollow-project-names", {}),
    }),
  )
  const [threadAliases, setThreadAliases] = useState<Record<string, string>>(() =>
    loadJSON("hollow-thread-names", {}),
  )
  const [hiddenThreads, setHiddenThreads] = useState<string[]>(() =>
    loadJSON<string[]>("hollow-hidden-threads-v2", []),
  )

  // Zoom is applied via webFrame directly — not React state — so ctrl+scroll
  // doesn't re-render (and re-parse) the entire chat transcript.
  useEffect(() => {
    initZoom()
  }, [])

  // Global Ctrl/Cmd shortcuts (search & zoom)
  useEffect(() => {
    const canZoom = window.hollowDesktop?.isElectron === true

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.ctrlKey || e.metaKey) {
        const key = e.key.toLowerCase()
        if (key === "k") {
          e.preventDefault()
          setSearchOpen((prev) => !prev)
        } else if (canZoom) {
          if (e.key === "=" || e.key === "+") {
            e.preventDefault()
            bumpZoom(ZOOM_STEP)
          } else if (e.key === "-") {
            e.preventDefault()
            bumpZoom(-ZOOM_STEP)
          } else if (e.key === "0") {
            e.preventDefault()
            resetZoom()
          }
        }
      }
    }

    const handleWheel = (e: WheelEvent) => {
      if (canZoom && (e.ctrlKey || e.metaKey)) {
        e.preventDefault()
        bumpZoom(e.deltaY < 0 ? ZOOM_STEP : -ZOOM_STEP)
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
    localStorage.setItem("hollow-projects", JSON.stringify(addedProjects))
  }, [addedProjects])

  useEffect(() => {
    localStorage.setItem("hollow-project-names", JSON.stringify(projectAliases))
  }, [projectAliases])
  useEffect(() => {
    localStorage.setItem("hollow-thread-names", JSON.stringify(threadAliases))
  }, [threadAliases])
  useEffect(() => {
    localStorage.setItem("hollow-hidden-threads-v2", JSON.stringify(hiddenThreads))
  }, [hiddenThreads])

  // Id of the assistant message currently being streamed by the agent.
  const streamingIdRef = useRef<string | null>(null)
  const messagesRef = useRef<Message[]>([])
  messagesRef.current = messages
  const currentSessionIdRef = useRef<string | null>(null)
  currentSessionIdRef.current = currentSessionId
  const projectCwdRef = useRef<string | null>(null)
  projectCwdRef.current = projectCwd
  const messagesCacheRef = useRef(new Map<string, Message[]>())
  const sessionListRef = useRef(sessionList)
  sessionListRef.current = sessionList
  const addedProjectsRef = useRef(addedProjects)
  addedProjectsRef.current = addedProjects

  const stashMessagesInCache = useCallback((sessionId: string | null) => {
    if (sessionId && messagesRef.current.length > 0) {
      messagesCacheRef.current.set(sessionId, messagesRef.current)
    }
  }, [])

  const refreshSessionList = useCallback(() => {
    send({ type: "list_sessions" })
  }, [])

  // Refresh sidebar when projects are added/removed (not on every thread switch).
  useEffect(() => {
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
    const unsubscribe = hollowAgent.onEvent((event: AgentEvent) => {
      switch (event.type) {
        case "bridge_ready": {
          // Pipe is open — load catalog once. Cwd updates come via session_cwd.
          send({ type: "get_model_catalog" })
          send({ type: "get_state" })
          send({ type: "get_messages" })
          window.setTimeout(() => refreshSessionList(), 50)
          break
        }
        case "session_cwd": {
          const cwd = event.cwd
          setProjectCwd(cwd)
          setAddedProjects((prev) => (prev.includes(cwd) ? prev : [...prev, cwd]))
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
              const sid = event.data.sessionId || null
              setModel(event.data.model ?? null)
              setCurrentSessionId(sid)
              setIsStreaming(event.data.isStreaming)
              if (sid) {
                setSessionList((prev) =>
                  upsertActiveSession(prev, sid, projectCwdRef.current ?? "~"),
                )
              }
              break
            }
            case "get_available_models":
              setAvailableModels(event.data.models)
              break
            case "get_model_catalog": {
              const catalog = event.data
              setModelCatalog(catalog)
              setAvailableModels(catalog.models)
              const s = catalog.state
              if (s.modelId) {
                setModel({
                  id: s.modelId,
                  name: s.modelName,
                  provider: s.provider,
                  contextLabel: s.contextLabel,
                  reasoning: s.reasoning,
                })
              }
              break
            }
            case "list_sessions": {
              const sessions = event.data.sessions
              setSessionList((prev) => {
                const merged = mergeSessionList(sessions, prev, currentSessionIdRef.current)
                localStorage.setItem("hollow-session-cache", JSON.stringify(merged))
                return merged
              })
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
            case "set_model": {
              const s = event.data
              setModel({
                id: s.modelId,
                name: s.modelName,
                provider: s.provider,
                contextLabel: s.contextLabel,
                reasoning: s.reasoning,
              })
              setModelCatalog((prev) => (prev ? { ...prev, state: s } : prev))
              break
            }
            case "switch_session":
            case "new_session": {
              // session.history from the backend updates state + messages.
              break
            }
            case "list_connections": {
              setConnections(event.data.connections)
              setSettingsError(null)
              break
            }
            case "start_codex_login": {
              setCodexLogin(event.data)
              setSettingsError(null)
              break
            }
            case "cancel_codex_login": {
              setCodexLogin(null)
              break
            }
          }
          break
        }
        case "connection_changed": {
          setConnections(event.connections)
          // Codex login resolved (success or failure) — stop showing the code.
          setCodexLogin(null)
          if (event.error) setSettingsError(event.error)
          else setSettingsError(null)
          break
        }
        case "settings_error": {
          setSettingsError(event.error)
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
    const sessionCwd = sessionListRef.current.find((s) => s.id === currentSessionIdRef.current)?.cwd
    send({
      type: "prompt",
      message: content,
      cwd: sessionCwd ?? projectCwdRef.current ?? undefined,
    })
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
      if (!info.id) return
      send({ type: "switch_session", sessionPath: info.id })
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

  // Remove a project from the app (folder stays on disk) and delete its threads.
  const handleDeleteProject = useCallback(
    (cwd: string) => {
      const sessions = sessionListRef.current.filter((s) => s.cwd === cwd)
      const ids = sessions.map((s) => s.id)
      const wasActive = sessions.some((s) => s.id === currentSessionId)

      setAddedProjects((prev) => prev.filter((p) => p !== cwd))
      setProjectAliases((prev) => {
        const next = { ...prev }
        delete next[cwd]
        return next
      })
      setThreadAliases((prev) => {
        const next = { ...prev }
        for (const id of ids) delete next[id]
        return next
      })
      setHiddenThreads((prev) => prev.filter((id) => !ids.includes(id)))
      for (const id of ids) messagesCacheRef.current.delete(id)

      setSessionList((prev) => prev.filter((s) => s.cwd !== cwd))

      if (wasActive) {
        const nextCwd =
          addedProjectsRef.current.find((p) => p !== cwd) ??
          sessionListRef.current.find((s) => s.cwd !== cwd)?.cwd ??
          undefined
        streamingIdRef.current = null
        setCurrentSessionId(null)
        setMessages([])
        setLoadingThread(true)
        setSyncingThread(false)
        setProjectCwd(nextCwd ?? null)
        send({ type: "new_session", ...(nextCwd ? { cwd: nextCwd } : {}) })
      }

      send({ type: "delete_project_sessions", cwd })
    },
    [currentSessionId],
  )

  const handleRenameThread = useCallback((id: string, name: string) => {
    setThreadAliases((prev) => {
      const next = { ...prev }
      if (name.trim()) next[id] = name.trim()
      else delete next[id]
      return next
    })
  }, [])

  // Delete a thread's session file (Enough storage only, not the project folder).
  const handleDeleteThread = useCallback(
    (id: string) => {
      const wasActive = id === currentSessionId

      messagesCacheRef.current.delete(id)
      setThreadAliases((prev) => {
        const next = { ...prev }
        delete next[id]
        return next
      })
      setHiddenThreads((prev) => prev.filter((hid) => hid !== id))
      setSessionList((prev) => prev.filter((s) => s.id !== id))

      if (wasActive) {
        streamingIdRef.current = null
        setCurrentSessionId(null)
        setMessages([])
        setLoadingThread(true)
        setSyncingThread(false)
        send({ type: "new_session", cwd: projectCwd ?? undefined })
      }

      send({ type: "delete_session", sessionId: id })
    },
    [currentSessionId, projectCwd],
  )

  const handleAddProject = useCallback(() => setPickerOpen(true), [])

  // Register a project folder in the app (does not create anything on disk).
  const handleProjectChosen = useCallback((dir: string) => {
    setPickerOpen(false)
    setAddedProjects((prev) => (prev.includes(dir) ? prev : [dir, ...prev]))
    setProjectCwd(dir)
  }, [])

  const handleSelectModel = useCallback((provider: string, modelId: string, thinkingLevel: string) => {
    send({ type: "set_model", provider, modelId, thinkingLevel })
  }, [])

  const handleSettingsModel = useCallback(
    (m: AgentModel) => {
      const levels = m.thinkingLevels ?? []
      const thinking =
        modelCatalog?.state.thinkingLevel && levels.includes(modelCatalog.state.thinkingLevel)
          ? modelCatalog.state.thinkingLevel
          : levels.includes("medium")
            ? "medium"
            : levels.find((l) => l !== "off") ?? ""
      send({ type: "set_model", provider: m.provider, modelId: m.id, thinkingLevel: thinking })
    },
    [modelCatalog?.state.thinkingLevel],
  )

  // Refresh connection state whenever the Settings panel opens; clear any stale error.
  useEffect(() => {
    if (settingsOpen) {
      setSettingsError(null)
      send({ type: "list_connections" })
    } else {
      // Closing the panel cancels an in-flight Codex login so we don't orphan the poll.
      if (codexLogin) send({ type: "cancel_codex_login" })
      setCodexLogin(null)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [settingsOpen])

  const handleConnectKey = useCallback((provider: string, key: string) => {
    send({ type: "set_api_key", provider, key })
  }, [])
  const handleRemoveKey = useCallback((provider: string) => {
    send({ type: "remove_key", provider })
  }, [])
  const handleStartCodexLogin = useCallback(() => {
    send({ type: "start_codex_login" })
  }, [])
  const handleCancelCodexLogin = useCallback(() => {
    send({ type: "cancel_codex_login" })
    setCodexLogin(null)
  }, [])

  const current = sessionList.find((s) => s.id === currentSessionId)
  const currentCwd = current?.cwd ?? projectCwd ?? "~"

  const projects = useMemo(() => {
    const set = new Set<string>(addedProjects)
    for (const session of sessionList) set.add(session.cwd)
    if (projectCwd) set.add(projectCwd)
    return Array.from(set)
  }, [addedProjects, projectCwd, sessionList])

  const sidebarSessions = useMemo(
    () => sessionList.filter((s) => !hiddenThreads.includes(s.id)),
    [sessionList, hiddenThreads],
  )

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
            onOpenSettings={() => setSettingsOpen(true)}
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
          <ChatWorkspace
            loadingThread={loadingThread}
            messages={messages}
            sessionId={currentSessionId}
            currentCwd={currentCwd}
            modelCatalog={modelCatalog}
            isStreaming={isStreaming}
            syncingThread={syncingThread}
            onSend={handleSend}
            onAbort={handleAbort}
            onSelectModel={handleSelectModel}
          />
          <TerminalPanel open={terminalOpen} />
        </div>
        <TaskSidebar open={taskSidebarOpen} onClose={() => setTaskSidebarOpen(false)} />
      </main>

      <SettingsPanel
        open={settingsOpen}
        onClose={() => setSettingsOpen(false)}
        models={availableModels}
        currentModelId={model?.id ?? null}
        onSelectModel={handleSettingsModel}
        connections={connections}
        codexLogin={codexLogin}
        settingsError={settingsError}
        onClearError={() => setSettingsError(null)}
        onConnectKey={handleConnectKey}
        onRemoveKey={handleRemoveKey}
        onStartCodexLogin={handleStartCodexLogin}
        onCancelCodexLogin={handleCancelCodexLogin}
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
