import type {
  AgentEvent,
  AgentModel,
  AgentProvider,
  AgentSessionInfo,
  AgentSessionState,
  ConnectionInfo,
  ContentPart,
  CodexLoginState,
  ModelCatalog,
  ModelSelectionState,
  RawMessage,
} from "./rpc"
import type { ToolVerb, Diff, DiffLine, RepoStatus } from "../types"
import {
  appendText,
  appendThinking,
  streamBlocksToContent,
  upsertTool,
  type StreamBlock,
} from "./stream-blocks"
import { sameProjectCwd } from "../lib/path"

type Listener = (event: AgentEvent) => void

// The window.hollowDesktop type (HollowBridge) is declared in vite-env.d.ts.

interface BackendSession {
  id: string
  path?: string
  cwd?: string
  title: string
  createdAt: string
  created?: string
  modified?: string
  messageCount?: number
}

interface BackendHistoryTool {
  id: string
  name: string
  arguments: string
  status: "running" | "completed" | "failed"
  result?: string
  details?: string
}

interface BackendHistoryMessage {
  id?: string
  role: "user" | "assistant" | "system" | "compactionSummary"
  content: string
  thinking?: string
  timestamp?: string
  tokensBefore?: number
  tools?: BackendHistoryTool[]
}

interface BackendModelsCatalog {
  type: "models.catalog"
  providers: AgentProvider[]
  models: AgentModel[]
  state: ModelSelectionState
}

type BackendMessage =
  | { type: "ready" }
  | { type: "session.list"; sessions: BackendSession[] | null }
  | { type: "session.history"; sessionId: string; cwd?: string; messages: BackendHistoryMessage[] | null }
  | BackendModelsCatalog
  | { type: "connections.list"; connections: ConnectionInfo[]; catalog: ModelCatalog }
  | { type: "connection.changed"; connections: ConnectionInfo[]; catalog: ModelCatalog; error?: string }
  | { type: "codex.login.start"; user_code: string; verify_url: string; poll_interval: number }
  | { type: "codex.login.cancelled" }
  | { type: "repoStatus"; added: number; removed: number; branch: string; contextPct: number }
  | { type: "token"; text?: string }
  | { type: "thinking"; text?: string }
  | {
      type: "tool"
      id: string
      name: string
      arguments: string
      status: "running" | "completed" | "failed"
      result?: string
      details?: string
    }
  | { type: "done"; requestId?: string }
  | { type: "compaction.start"; requestId: string; operationId: string; sessionId?: string; reason: string }
  | {
      type: "compaction.end"
      requestId: string
      operationId: string
      sessionId?: string
      reason: string
      result?: {
        summary: string
        firstKeptEntryId: string
        tokensBefore: number
        estimatedTokensAfter?: number
      }
      aborted: boolean
      willRetry: boolean
      errorMessage?: string
    }
  | { type: "error"; message: string }
  | { type: "loop.status"; active: boolean; iteration: number; maxIterations: number; task: string }

const DEFAULT_CWD = "Hollow"

const emptyCatalog = (): ModelCatalog => ({
  providers: [],
  models: [],
  state: {
    provider: "",
    modelId: "",
    modelName: "…",
    thinkingLevel: "",
  },
})

const nowIso = () => new Date().toISOString()

const newRequestId = () =>
  globalThis.crypto?.randomUUID?.() ?? `request-${Date.now()}-${Math.random().toString(36).slice(2)}`

function commandText(command: Record<string, unknown>, key: string) {
  const value = command[key]
  return typeof value === "string" ? value : ""
}

function stateToModel(state: ModelSelectionState): AgentModel {
  return {
    id: state.modelId,
    name: state.modelName,
    provider: state.provider,
    contextLabel: state.contextLabel,
    reasoning: state.reasoning,
  }
}

function toolVerb(name?: string): ToolVerb {
  const lower = (name ?? "tool").toLowerCase()
  if (lower.includes("write")) return "Write"
  if (lower.includes("edit") || lower.includes("patch")) return "Edit"
  if (lower.includes("read") || lower.includes("file") || lower.includes("dir")) return "Read"
  if (lower.includes("glob")) return "Glob"
  if (lower.includes("grep")) return "Grep"
  if (lower === "web_search" || lower.includes("web_search")) return "Web Search"
  if (lower.includes("browser")) return "Browser"
  if (lower.includes("search") || lower.includes("find")) return "Search"
  if (lower.includes("swarm")) return "Swarm Agent"
  if (lower.includes("task") || lower.includes("agent") || lower.includes("skill") || lower.includes("memory")) return "Task"
  if (lower.includes("fetch")) return "Fetch"
  return "Bash"
}

function toolTitle(tool: BackendHistoryTool | (BackendMessage & { type: "tool" })) {
  try {
    const args = JSON.parse(tool.arguments || "{}")
    const nm = (tool.name || "").toLowerCase()
    if (nm.includes("browser")) return ""
    return (
      args.CommandLine ||
      args.command ||
      args.AbsolutePath ||
      args.TargetFile ||
      args.path ||
      args.filename ||
      args.pattern ||
      args.query ||
      args.url ||
      (Array.isArray(args.urls) ? args.urls.join(", ") : "") ||
      tool.name
    )
  } catch {
    return tool.arguments || tool.name
  }
}

function toolMeta(name: string, argsJson: string): string | undefined {
  try {
    const args = JSON.parse(argsJson || "{}")
    const lower = name.toLowerCase()
    if (lower.includes("write") && typeof args.content === "string") {
      const lines = args.content === "" ? 0 : args.content.split("\n").length
      return lines > 0 ? `+${lines}` : undefined
    }
    if ((lower.includes("edit") || lower.includes("patch")) && (typeof args.new_string === "string" || typeof args.old_string === "string")) {
      const added = typeof args.new_string === "string" && args.new_string !== "" ? args.new_string.split("\n").length : 0
      const removed = typeof args.old_string === "string" && args.old_string !== "" ? args.old_string.split("\n").length : 0
      if (added > 0 && removed > 0) {
        return `+${added} -${removed}`
      }
      if (added > 0) {
        return `+${added}`
      }
      if (removed > 0) {
        return `-${removed}`
      }
    }
  } catch {
    /* ignore */
  }
  return undefined
}

function parseDiffString(file: string, diffStr: string): Diff {
  const lines: DiffLine[] = []
  let added = 0
  let removed = 0

  const rawLines = diffStr.split("\n")
  for (const line of rawLines) {
    if (line === "" && rawLines.indexOf(line) === rawLines.length - 1) continue
    const indicator = line[0]
    const text = line.slice(1)
    if (indicator === "-") {
      lines.push({ type: "remove", text })
      removed++
    } else if (indicator === "+") {
      lines.push({ type: "add", text })
      added++
    } else {
      lines.push({ type: "context", text: line.startsWith(" ") ? text : line })
    }
  }

  return {
    file,
    added,
    removed,
    lines,
  }
}

function mapTool(tool: BackendHistoryTool | (BackendMessage & { type: "tool" })): ContentPart {
  const name = tool.name || "tool"
  const args = tool.arguments ?? ""
  let diff: Diff | undefined = undefined

  if (tool.details) {
    try {
      const parsed = JSON.parse(tool.details)
      if (parsed && typeof parsed.diff === "string") {
        diff = parseDiffString(toolTitle({ ...tool, name }), parsed.diff)
      }
    } catch {
      /* ignore */
    }
  }

  let meta = toolMeta(name, args)
  if (diff) {
    if (diff.added > 0 && diff.removed > 0) {
      meta = `+${diff.added} -${diff.removed}`
    } else if (diff.added > 0) {
      meta = `+${diff.added}`
    } else if (diff.removed > 0) {
      meta = `-${diff.removed}`
    } else {
      meta = undefined
    }
  }

  return {
    type: "tool",
    tool: toolVerb(name),
    title: toolTitle({ ...tool, name }),
    meta,
    status: tool.status === "failed" ? "error" : tool.status === "running" ? "running" : "done",
    output: tool.result,
    diff,
    details: tool.details,
  }
}

function mapHistory(messages: BackendHistoryMessage[] | null | undefined): RawMessage[] {
  return (messages ?? []).flatMap((message): RawMessage[] => {
    if (message.role === "compactionSummary") {
      return [{
        role: "compactionSummary",
        content: [{ type: "text", text: message.content }],
        tokensBefore: message.tokensBefore,
      }]
    }
    if (message.role === "system") {
      return [{ role: "assistant", content: [{ type: "text", text: message.content }] }]
    }

    const content: ContentPart[] = []
    if (message.thinking) content.push({ type: "thinking", thinking: message.thinking })
    if (message.content) content.push({ type: "text", text: message.content })
    for (const tool of message.tools ?? []) content.push(mapTool(tool))
    return [{ role: message.role, content }]
  })
}

function mapSession(session: BackendSession): AgentSessionInfo {
  const modified = session.modified || nowIso()
  const created = session.created || modified
  return {
    path: session.path || session.id,
    id: session.id,
    cwd: session.cwd || DEFAULT_CWD,
    name: session.title,
    created,
    modified,
    messageCount: session.messageCount ?? 0,
    firstMessage: session.title,
  }
}

export class HollowClient {
  private listeners = new Set<Listener>()
  private connectStarted = false
  private ipcUnsubscribe: (() => void) | null = null
  private sessions: AgentSessionInfo[] = []
  private histories = new Map<string, RawMessage[]>()
  private currentSessionId: string | null = null
  private streaming = false
  private streamBlocks: StreamBlock[] = []
  private toolMetaMap = new Map<string, { name: string; arguments: string }>()
  private catalog: ModelCatalog = emptyCatalog()
  private connections: ConnectionInfo[] = []
  private awaitingNewSession = false
  /** After deleting the active thread, don't resurrect it from a stale listSessions. */
  private skipAutoOpenUntilEmpty = false
  private activeRequest: {
    id: string
    compaction: boolean
    reason: string
    cancellationPending: boolean
    compactionOperations: Set<string>
    /** Compaction.end already processed for this operation; duplicate deliveries are dropped. */
    endedCompactionOperations: Set<string>
  } | null = null

  onEvent(listener: Listener) {
    this.listeners.add(listener)
    this.connect()
    return () => {
      this.listeners.delete(listener)
    }
  }

  send(command: Record<string, unknown>) {
    this.connect()

    switch (command.type) {
      case "get_available_models":
        this.emit({
          type: "response",
          command: "get_available_models",
          success: true,
          data: { models: this.catalog.models },
        })
        break
      case "get_model_catalog":
        this.dispatch({ type: "listModels" })
        break
      case "get_state":
        this.emit({ type: "response", command: "get_state", success: true, data: this.state() })
        break
      case "get_messages":
        this.emit({
          type: "response",
          command: "get_messages",
          success: true,
          data: { messages: this.currentSessionId ? this.histories.get(this.currentSessionId) ?? [] : [] },
        })
        break
      case "list_sessions":
        this.dispatch({ type: "listSessions" })
        break
      case "switch_session": {
        const raw = commandText(command, "sessionPath")
        if (raw) {
          const id = this.resolveSessionId(raw)
          this.currentSessionId = id
          this.dispatch({ type: "openSession", id: raw })
        }
        this.emit({ type: "response", command: "switch_session", success: true, data: { cancelled: false } })
        break
      }
      case "new_session": {
        const cwd = commandText(command, "cwd")
        this.awaitingNewSession = true
        this.dispatch({ type: "newSession", ...(cwd ? { cwd } : {}) })
        this.emit({ type: "response", command: "new_session", success: true, data: { cancelled: false } })
        break
      }
      case "delete_session": {
        const id = commandText(command, "sessionId")
        const resolved = this.resolveSessionId(id)
        const match = this.sessions.find((s) => s.id === id || s.path === id || s.id === resolved)
        const deleteTarget = match?.path || match?.id || id
        if (this.currentSessionId === id || this.currentSessionId === resolved) {
          this.currentSessionId = null
          this.skipAutoOpenUntilEmpty = true
        }
        this.histories.delete(id)
        this.histories.delete(resolved)
        this.sessions = this.sessions.filter((s) => s.id !== id && s.path !== id && s.id !== resolved)
        if (!window.hollowDesktop) {
          this.emit({ type: "bridge_error", error: "Desktop IPC unavailable — run via electron:dev" })
          break
        }
        void window.hollowDesktop
          .dispatch({ type: "deleteSession", id: deleteTarget })
          .then((result) => {
            if (!result.ok) {
              this.skipAutoOpenUntilEmpty = false
              this.emit({ type: "bridge_error", error: result.error ?? "Failed to delete session" })
              this.dispatch({ type: "listSessions" })
              return
            }
            this.dispatch({ type: "listSessions" })
          })
          .catch((err) => {
            this.skipAutoOpenUntilEmpty = false
            this.emit({ type: "bridge_error", error: String(err?.message || err) })
            this.dispatch({ type: "listSessions" })
          })
        break
      }
      case "delete_project_sessions": {
        const cwd = commandText(command, "cwd")
        if (cwd === "") break
        const sessionPaths = Array.isArray(command.sessionPaths)
          ? command.sessionPaths.filter((p): p is string => typeof p === "string" && p !== "")
          : []
        const inProject = this.sessions.filter((s) => sameProjectCwd(s.cwd, cwd))
        const clearingActive = inProject.some(
          (s) => s.id === this.currentSessionId || s.path === this.currentSessionId,
        )
        if (clearingActive) {
          this.currentSessionId = null
          this.skipAutoOpenUntilEmpty = true
        }
        for (const session of inProject) {
          this.histories.delete(session.id)
          if (session.path) this.histories.delete(session.path)
        }
        this.sessions = this.sessions.filter((s) => !sameProjectCwd(s.cwd, cwd))
        if (!window.hollowDesktop) {
          this.emit({ type: "bridge_error", error: "Desktop IPC unavailable — run via electron:dev" })
          break
        }
        void window.hollowDesktop
          .dispatch({ type: "deleteProjectSessions", cwd, sessionPaths })
          .then((result) => {
            if (!result.ok) {
              this.skipAutoOpenUntilEmpty = false
              this.emit({ type: "bridge_error", error: result.error ?? "Failed to remove project" })
              this.dispatch({ type: "listSessions" })
              return
            }
            const data = result.data as { deleted?: number } | undefined
            const deleted = typeof data?.deleted === "number" ? data.deleted : 0
            if (deleted === 0 && (sessionPaths.length > 0 || inProject.length > 0)) {
              this.emit({
                type: "bridge_error",
                error: "Project was removed from the app but no session files were deleted from disk.",
              })
            }
            this.dispatch({ type: "listSessions" })
          })
          .catch((err) => {
            this.skipAutoOpenUntilEmpty = false
            this.emit({ type: "bridge_error", error: String(err?.message || err) })
            this.dispatch({ type: "listSessions" })
          })
        break
      }
      case "prompt": {
        const text = commandText(command, "message")
        const requestId = newRequestId()
        const compaction = /^\/compact(?:\s|$)/.test(text)
        this.streaming = true
        this.streamBlocks = []
        this.toolMetaMap.clear()
        this.activeRequest = {
          id: requestId,
          compaction,
          reason: compaction ? "manual" : "",
          cancellationPending: false,
          compactionOperations: new Set(),
          endedCompactionOperations: new Set(),
        }
        this.dispatchPrompt({
          type: "prompt",
          text,
          requestId,
          ...(commandText(command, "cwd") ? { cwd: commandText(command, "cwd") } : {}),
        }, requestId)
        this.emit({ type: "response", command: "get_state", success: true, data: this.state() })
        break
      }
      case "abort":
        if (this.activeRequest) this.activeRequest.cancellationPending = true
        this.dispatchInterrupt()
        break
      case "set_model":
        this.dispatch({
          type: "setModel",
          provider: commandText(command, "provider"),
          model: commandText(command, "modelId"),
          thinkingLevel: commandText(command, "thinkingLevel"),
        })
        break
      case "toggle_model_enabled":
        this.dispatch({
          type: "toggleModelEnabled",
          modelId: commandText(command, "modelId"),
        })
        break
      case "list_connections":
        this.dispatchSettings({ type: "listConnections" }, "list_connections")
        break
      case "set_api_key":
        this.dispatchSettings(
          { type: "setApiKey", provider: commandText(command, "provider"), key: commandText(command, "key") },
          "set_api_key",
        )
        break
      case "remove_key":
        this.dispatchSettings({ type: "removeKey", provider: commandText(command, "provider") }, "remove_key")
        break
      case "start_codex_login":
        this.dispatchSettings({ type: "startCodexLogin" }, "start_codex_login")
        break
      case "cancel_codex_login":
        this.dispatchSettings({ type: "cancelCodexLogin" }, "cancel_codex_login")
        break
    }
  }

  private connect() {
    if (this.connectStarted) return
    this.connectStarted = true

    if (!window.hollowDesktop || typeof window.hollowDesktop.dispatch !== "function") {
      this.emit({ type: "bridge_error", error: "Desktop IPC unavailable — run via electron:dev" })
      return
    }

    this.ipcUnsubscribe = window.hollowDesktop.onEvent((msg: unknown) => {
      try {
        this.handleBackendMessage(msg as BackendMessage)
      } catch (error) {
        this.emit({ type: "bridge_error", error: String(error) })
      }
    })

    // Same on-open sequence as WS: ready → list sessions + models.
    this.emit({ type: "bridge_ready" })
    this.dispatch({ type: "listSessions" })
    this.dispatch({ type: "listModels" })
  }

  /**
   * Composer status bar query: git diff shortstat + branch + context-window fill
   * for the session cwd. Awaits the IPC round-trip and returns the parsed
   * result directly (not via the event stream) — the caller polls on a timer.
   * Returns null when IPC is unavailable or the call fails (non-git dirs, etc).
   */
  async repoStatus(cwd: string): Promise<RepoStatus | null> {
    if (!window.hollowDesktop) return null
    try {
      const result = await window.hollowDesktop.dispatch({ type: "repoStatus", cwd })
      if (!result.ok || result.data === undefined) return null
      const d = result.data as RepoStatus
      return d && d.type === "repoStatus" ? d : null
    } catch {
      return null
    }
  }

  // IPC dispatch is the ONLY transport — no WS fallback.
  private dispatch(message: Record<string, unknown>) {
    if (!window.hollowDesktop) {
      this.emit({ type: "bridge_error", error: "Desktop IPC unavailable — run via electron:dev" })
      return
    }
    void window.hollowDesktop
      .dispatch(message)
      .then((result) => {
        if (!result.ok) {
          this.emit({ type: "bridge_error", error: result.error ?? "dispatch failed" })
          return
        }
        if (result.data !== undefined) {
          this.handleDispatchResponse(result.data)
        }
      })
      .catch((err) => {
        this.emit({ type: "bridge_error", error: String(err?.message || err) })
      })
  }

  private dispatchPrompt(message: Record<string, unknown>, requestId: string) {
    if (!window.hollowDesktop) {
      this.failPromptDispatch(requestId, "Desktop IPC unavailable — run via electron:dev")
      return
    }
    void window.hollowDesktop
      .dispatch(message)
      .then((result) => {
        if (!result.ok) {
          this.failPromptDispatch(requestId, result.error ?? "dispatch failed")
          return
        }
        // prompt.success is only an acknowledgement. The event stream owns the
        // request's terminal done/compaction.end lifecycle.
        if (result.data !== undefined) this.handleDispatchResponse(result.data)
      })
      .catch((err) => this.failPromptDispatch(requestId, String(err?.message || err)))
  }

  private dispatchInterrupt() {
    if (!window.hollowDesktop) return
    void window.hollowDesktop
      .dispatch({ type: "interrupt" })
      .then((result) => {
        if (result.ok && result.data !== undefined) this.handleDispatchResponse(result.data)
      })
      .catch(() => {
        // The prompt remains active until its own terminal event, even if the
        // best-effort cancellation IPC call fails.
      })
  }

  private failPromptDispatch(requestId: string, error: string) {
    const active = this.activeRequest
    if (!active || active.id !== requestId) return
    if (active.compaction) {
      this.finishCompaction({
        type: "compaction.end",
        requestId,
        operationId: requestId,
        reason: active.reason,
        aborted: false,
        willRetry: false,
        errorMessage: error,
      })
      return
    }
    const prefix = this.streamBlocks.length ? "\n\n" : ""
    this.streamBlocks = appendText(this.streamBlocks, `${prefix}Agent error: ${error}`)
    this.finishNormalRequest(requestId)
  }

  private finishNormalRequest(requestId?: string) {
    const active = this.activeRequest
    if (!active || (requestId && active.id !== requestId) || active.compaction) return
    if (active.cancellationPending && this.streamBlocks.length === 0) {
      this.streamBlocks = appendText(this.streamBlocks, "Generation cancelled.")
    }
    if (this.streamBlocks.length > 0) this.emitAssistantUpdate()
    this.streaming = false
    this.activeRequest = null
    this.toolMetaMap.clear()
    this.emit({ type: "agent_end", requestId: active.id })
    this.dispatch({ type: "listSessions" })
  }

  private finishCompaction(message: Extract<BackendMessage, { type: "compaction.end" }>) {
    const active = this.activeRequest
    if (!active || active.id !== message.requestId) return
    if (active.endedCompactionOperations.has(message.operationId)) return
    active.endedCompactionOperations.add(message.operationId)
    active.compactionOperations.delete(message.operationId)
    this.emit({
      type: "compaction_end",
      requestId: message.requestId,
      operationId: message.operationId,
      sessionId: message.sessionId,
      reason: message.reason,
      result: message.result,
      aborted: message.aborted,
      willRetry: message.willRetry,
      errorMessage: message.errorMessage,
    })
    if (message.reason === "manual") {
      this.streaming = false
      this.activeRequest = null
      this.streamBlocks = []
      this.toolMetaMap.clear()
    }
    this.dispatch({ type: "listSessions" })
  }

  // Like dispatch(), but failures surface as a settings_error event (which the
  // Settings panel shows inline) instead of a bridge_error (which is dropped
  // unless a chat turn is streaming). Used by the connection-management commands.
  private dispatchSettings(message: Record<string, unknown>, commandName: string) {
    if (!window.hollowDesktop) {
      this.emit({ type: "settings_error", command: commandName, error: "Desktop IPC unavailable — run via electron:dev" })
      return
    }
    void window.hollowDesktop
      .dispatch(message)
      .then((result) => {
        if (!result.ok) {
          this.emit({ type: "settings_error", command: commandName, error: result.error ?? "dispatch failed" })
          return
        }
        if (result.data !== undefined) {
          this.handleDispatchResponse(result.data)
        }
      })
      .catch((err) => {
        this.emit({ type: "settings_error", command: commandName, error: String(err?.message || err) })
      })
  }

  /** Apply synchronous command results (session list, models catalog, …). */
  private handleDispatchResponse(data: unknown) {
    if (Array.isArray(data)) {
      for (const item of data) {
        this.handleBackendMessage(item as BackendMessage)
      }
      return
    }
    if (data !== null && typeof data === "object" && "type" in data) {
      this.handleBackendMessage(data as BackendMessage)
    }
  }

  private applyCatalog(catalog: ModelCatalog) {
    this.catalog = catalog
    this.emit({ type: "response", command: "get_model_catalog", success: true, data: catalog })
    this.emit({ type: "response", command: "get_available_models", success: true, data: { models: catalog.models } })
    this.emit({ type: "response", command: "get_state", success: true, data: this.state() })
  }

  private handleBackendMessage(message: BackendMessage) {
    switch (message.type) {
      case "ready":
        break
      case "models.catalog":
        this.applyCatalog({
          providers: message.providers ?? [],
          models: message.models ?? [],
          state: message.state,
        })
        break
      case "connections.list":
        if (message.catalog) this.applyCatalog(message.catalog)
        this.connections = message.connections ?? []
        this.emit({
          type: "response",
          command: "list_connections",
          success: true,
          data: { connections: this.connections, catalog: this.catalog },
        })
        this.emit({ type: "response", command: "get_state", success: true, data: this.state() })
        break
      case "connection.changed":
        if (message.catalog) this.applyCatalog(message.catalog)
        this.connections = message.connections ?? this.connections
        this.emit({
          type: "connection_changed",
          connections: this.connections,
          catalog: this.catalog,
          error: message.error,
        })
        this.emit({ type: "response", command: "get_state", success: true, data: this.state() })
        break
      case "codex.login.start":
        this.emit({
          type: "response",
          command: "start_codex_login",
          success: true,
          data: {
            user_code: message.user_code,
            verify_url: message.verify_url,
            poll_interval: message.poll_interval,
          },
        })
        break
      case "codex.login.cancelled":
        this.emit({ type: "response", command: "cancel_codex_login", success: true, data: { cancelled: true } })
        break
      case "session.list": {
        this.sessions = (message.sessions ?? []).map(mapSession)
        const first = this.sessions[0]
        if (
          !this.currentSessionId &&
          first &&
          !this.awaitingNewSession &&
          !this.skipAutoOpenUntilEmpty
        ) {
          this.currentSessionId = first.id
          this.dispatch({ type: "openSession", id: first.id })
        }
        if (this.skipAutoOpenUntilEmpty && this.sessions.length === 0) {
          this.skipAutoOpenUntilEmpty = false
        }
        this.emit({ type: "response", command: "list_sessions", success: true, data: { sessions: this.sessions } })
        this.emit({ type: "response", command: "get_state", success: true, data: this.state() })
        break
      }
      case "session.history": {
        this.awaitingNewSession = false
        this.currentSessionId = message.sessionId
        const history = mapHistory(message.messages)
        this.histories.set(message.sessionId, history)
        const cwd = message.cwd || this.sessions.find((s) => s.id === message.sessionId)?.cwd
        if (cwd) {
          this.emit({ type: "session_cwd", cwd })
        }
        this.emit({ type: "response", command: "get_state", success: true, data: this.state() })
        this.emit({ type: "response", command: "get_messages", success: true, data: { messages: history } })
        break
      }
      case "token":
        this.streamBlocks = appendText(this.streamBlocks, message.text ?? "")
        this.emitAssistantUpdate()
        break
      case "thinking":
        this.streamBlocks = appendThinking(this.streamBlocks, message.text ?? "")
        this.emitAssistantUpdate()
        break
      case "tool": {
        const id = message.id || `tool-${this.streamBlocks.length}`
        if (message.name) {
          this.toolMetaMap.set(id, {
            name: message.name,
            arguments: message.arguments || this.toolMetaMap.get(id)?.arguments || "",
          })
        }
        const meta = this.toolMetaMap.get(id)
        const prev = this.streamBlocks.find((b) => b.type === "tool" && b.id === id)
        const prevOutput = prev?.type === "tool" ? prev.part.output : undefined
        const prevDetails = prev?.type === "tool" ? prev.part.details : undefined
        this.streamBlocks = upsertTool(
          this.streamBlocks,
          id,
          mapTool({
            type: "tool",
            id,
            name: message.name || meta?.name || "tool",
            arguments: message.arguments || meta?.arguments || "",
            status: message.status ?? "running",
            result: message.result || prevOutput,
            details: message.details || prevDetails,
          }),
        )
        this.emitAssistantUpdate()
        break
      }
      case "compaction.start": {
        const active = this.activeRequest
        if (
          !active ||
          active.id !== message.requestId ||
          active.compactionOperations.has(message.operationId) ||
          active.endedCompactionOperations.has(message.operationId)
        )
          break
        active.compactionOperations.add(message.operationId)
        if (message.reason === "manual") {
          active.compaction = true
        }
        active.reason = message.reason
        this.emit({
          type: "compaction_start",
          requestId: message.requestId,
          operationId: message.operationId,
          sessionId: message.sessionId,
          reason: message.reason,
        })
        break
      }
      case "compaction.end":
        this.finishCompaction(message)
        break
      case "done":
        this.finishNormalRequest(message.requestId)
        break
      case "error":
        this.awaitingNewSession = false
        if (this.activeRequest?.compaction) {
          this.finishCompaction({
            type: "compaction.end",
            requestId: this.activeRequest.id,
            operationId: this.activeRequest.id,
            reason: this.activeRequest.reason,
            aborted: this.activeRequest.cancellationPending,
            willRetry: false,
            errorMessage: message.message,
          })
        } else if (this.activeRequest) {
          const requestId = this.activeRequest.id
          const prefix = this.streamBlocks.length ? "\n\n" : ""
          this.streamBlocks = appendText(this.streamBlocks, `${prefix}Agent error: ${message.message}`)
          this.finishNormalRequest(requestId)
        } else {
          this.emit({ type: "bridge_error", error: message.message })
        }
        break
      case "loop.status":
        this.emit({
          type: "loop_status" as any,
          active: message.active,
          iteration: message.iteration,
          maxIterations: message.maxIterations,
          task: message.task,
        })
        break
    }
  }

  private emitAssistantUpdate() {
    const content = streamBlocksToContent(this.streamBlocks)
    this.emit({
      type: "message_update",
      assistantMessageEvent: {
        partial: {
          role: "assistant",
          content,
        },
      },
    })
  }

  private resolveSessionId(raw: string): string {
    const match = this.sessions.find((s) => s.id === raw || s.path === raw)
    return match?.id ?? raw
  }

  private state(): AgentSessionState {
    return {
      model: this.catalog.state.modelId ? stateToModel(this.catalog.state) : undefined,
      sessionId: this.currentSessionId ?? "",
      isStreaming: this.streaming,
      messageCount: this.currentSessionId ? this.histories.get(this.currentSessionId)?.length ?? 0 : 0,
    }
  }

  private emit(event: AgentEvent) {
    for (const listener of this.listeners) listener(event)
  }
}

export const hollowAgent = new HollowClient()
