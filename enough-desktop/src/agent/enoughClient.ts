import type {
  AgentEvent,
  AgentModel,
  AgentProvider,
  AgentSessionInfo,
  AgentSessionState,
  ContentPart,
  ModelCatalog,
  ModelSelectionState,
  RawMessage,
} from "./rpc"
import type { ToolVerb } from "../types"
import {
  appendText,
  appendThinking,
  streamBlocksToContent,
  upsertTool,
  type StreamBlock,
} from "./stream-blocks"

type Listener = (event: AgentEvent) => void

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
}

interface BackendHistoryMessage {
  id: string
  role: "user" | "assistant" | "system"
  content: string
  thinking?: string
  timestamp: string
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
  | { type: "token"; text?: string }
  | { type: "thinking"; text?: string }
  | {
      type: "tool"
      id: string
      name: string
      arguments: string
      status: "running" | "completed" | "failed"
      result?: string
    }
  | { type: "done" }
  | { type: "error"; message: string }

const WS_URL = import.meta.env.VITE_ENOUGH_WS || "ws://127.0.0.1:8754"
const DEFAULT_CWD = "Enough"

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
  if (lower.includes("read") || lower.includes("file")) return "Read"
  if (lower.includes("grep")) return "Grep"
  if (lower.includes("search") || lower.includes("find")) return "Search"
  if (lower.includes("task") || lower.includes("agent")) return "Task"
  if (lower.includes("fetch")) return "Fetch"
  return "Bash"
}

function toolTitle(tool: BackendHistoryTool | (BackendMessage & { type: "tool" })) {
  try {
    const args = JSON.parse(tool.arguments || "{}")
    return (
      args.CommandLine ||
      args.command ||
      args.AbsolutePath ||
      args.TargetFile ||
      args.path ||
      args.filename ||
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
      const lines = args.content.split("\n").length
      return lines > 0 ? `+${lines}` : undefined
    }
    if ((lower.includes("edit") || lower.includes("patch")) && typeof args.new_string === "string") {
      const lines = args.new_string.split("\n").length
      return lines > 0 ? `+${lines}` : undefined
    }
  } catch {
    /* ignore */
  }
  return undefined
}

function mapTool(tool: BackendHistoryTool | (BackendMessage & { type: "tool" })): ContentPart {
  const name = tool.name || "tool"
  const args = tool.arguments ?? ""
  return {
    type: "tool",
    tool: toolVerb(name),
    title: toolTitle({ ...tool, name }),
    meta: toolMeta(name, args),
    status: tool.status === "failed" ? "error" : tool.status === "running" ? "running" : "done",
    output: tool.result,
  }
}

function mapHistory(messages: BackendHistoryMessage[] | null | undefined): RawMessage[] {
  return (messages ?? []).flatMap((message): RawMessage[] => {
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

class EnoughClient {
  private ws: WebSocket | null = null
  private listeners = new Set<Listener>()
  private connectStarted = false
  private reconnectTimer: number | null = null
  private sessions: AgentSessionInfo[] = []
  private histories = new Map<string, RawMessage[]>()
  private currentSessionId: string | null = null
  private streaming = false
  private streamBlocks: StreamBlock[] = []
  private toolMeta = new Map<string, { name: string; arguments: string }>()
  private catalog: ModelCatalog = emptyCatalog()

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
        this.sendWs({ type: "listModels" })
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
        this.sendWs({ type: "listSessions" })
        break
      case "switch_session": {
        const raw = commandText(command, "sessionPath")
        if (raw) {
          const id = this.resolveSessionId(raw)
          this.currentSessionId = id
          this.sendWs({ type: "openSession", id: raw })
        }
        this.emit({ type: "response", command: "switch_session", success: true, data: { cancelled: false } })
        break
      }
      case "new_session": {
        const cwd = commandText(command, "cwd")
        this.sendWs({ type: "newSession", ...(cwd ? { cwd } : {}) })
        this.emit({ type: "response", command: "new_session", success: true, data: { cancelled: false } })
        break
      }
      case "prompt":
        this.streaming = true
        this.streamBlocks = []
        this.toolMeta.clear()
        this.sendWs({
          type: "prompt",
          text: commandText(command, "message"),
          ...(commandText(command, "cwd") ? { cwd: commandText(command, "cwd") } : {}),
        })
        this.emit({ type: "response", command: "get_state", success: true, data: this.state() })
        break
      case "abort":
        this.sendWs({ type: "interrupt" })
        this.streaming = false
        this.emit({ type: "agent_end" })
        break
      case "set_model":
        this.sendWs({
          type: "setModel",
          provider: commandText(command, "provider"),
          model: commandText(command, "modelId"),
          thinkingLevel: commandText(command, "thinkingLevel"),
        })
        break
    }
  }

  private connect() {
    if (this.connectStarted) return
    this.connectStarted = true
    this.openSocket()
  }

  private openSocket() {
    this.ws = new WebSocket(WS_URL)

    this.ws.onopen = () => {
      this.emit({ type: "bridge_ready" })
      this.sendWs({ type: "listSessions" })
      this.sendWs({ type: "listModels" })
    }

    this.ws.onmessage = (event) => {
      try {
        this.handleBackendMessage(JSON.parse(event.data) as BackendMessage)
      } catch (error) {
        this.emit({ type: "bridge_error", error: String(error) })
      }
    }

    this.ws.onclose = () => {
      this.ws = null
      this.connectStarted = false
      this.emit({ type: "bridge_exit", code: null })
      if (this.reconnectTimer === null) {
        this.reconnectTimer = window.setTimeout(() => {
          this.reconnectTimer = null
          this.connect()
        }, 1000)
      }
    }

    this.ws.onerror = () => {
      this.emit({ type: "bridge_error", error: `Cannot connect to ${WS_URL}` })
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
      case "session.list": {
        this.sessions = (message.sessions ?? []).map(mapSession)
        const first = this.sessions[0]
        if (!this.currentSessionId && first) {
          this.currentSessionId = first.id
          this.sendWs({ type: "openSession", id: first.id })
        }
        this.emit({ type: "response", command: "list_sessions", success: true, data: { sessions: this.sessions } })
        this.emit({ type: "response", command: "get_state", success: true, data: this.state() })
        break
      }
      case "session.history": {
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
          this.toolMeta.set(id, {
            name: message.name,
            arguments: message.arguments ?? this.toolMeta.get(id)?.arguments ?? "",
          })
        }
        const meta = this.toolMeta.get(id)
        const prev = this.streamBlocks.find((b) => b.type === "tool" && b.id === id)
        const prevOutput = prev?.type === "tool" ? prev.part.output : undefined
        this.streamBlocks = upsertTool(
          this.streamBlocks,
          id,
          mapTool({
            type: "tool",
            id,
            name: message.name || meta?.name || "tool",
            arguments: message.arguments ?? meta?.arguments ?? "",
            status: message.status ?? "running",
            result: message.result ?? prevOutput,
          }),
        )
        this.emitAssistantUpdate()
        break
      }
      case "done":
        this.streaming = false
        this.toolMeta.clear()
        this.emit({ type: "agent_end" })
        this.sendWs({ type: "listSessions" })
        break
      case "error":
        this.streaming = false
        this.toolMeta.clear()
        this.emit({ type: "bridge_error", error: message.message })
        this.emit({ type: "agent_end" })
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

  private sendWs(message: Record<string, unknown>) {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(message))
    }
  }

  private emit(event: AgentEvent) {
    for (const listener of this.listeners) listener(event)
  }
}

export const enoughAgent = new EnoughClient()
