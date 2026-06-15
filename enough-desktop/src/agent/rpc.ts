// Adapter between Enough's WebSocket events and the desktop UI data model.
import type { Block, Message, ToolVerb } from "../types"

// ── Shapes from the agent (we only type what we read) ────────────────────────

export interface ContentPart {
  type: string
  text?: string
  thinking?: string
  tool?: ToolVerb
  title?: string
  meta?: string
  status?: "running" | "done" | "error"
  output?: string
}

export interface RawMessage {
  role: string
  content?: ContentPart[]
  timestamp?: number
}

export interface AgentProvider {
  id: string
  name: string
  connected: boolean
}

export interface AgentModel {
  id: string
  name: string
  provider: string
  contextWindow?: number
  contextLabel?: string
  reasoning?: boolean
  thinkingLevels?: string[]
  thinkingLevelLabels?: string[]
}

export interface ModelSelectionState {
  provider: string
  modelId: string
  modelName: string
  thinkingLevel: string
  contextLabel?: string
  reasoning?: boolean
}

export interface ModelCatalog {
  providers: AgentProvider[]
  models: AgentModel[]
  state: ModelSelectionState
}

export interface AgentSessionInfo {
  path: string
  id: string
  cwd: string
  name?: string
  created: string
  modified: string
  messageCount: number
  firstMessage: string
}

export interface AgentSessionState {
  model?: AgentModel
  sessionId: string
  sessionName?: string
  isStreaming: boolean
  messageCount: number
}

// ── Events (stdout) ──────────────────────────────────────────────────────────

export type AgentEvent =
  // Streaming lifecycle
  | { type: "message_update"; assistantMessageEvent?: { partial?: RawMessage } }
  | { type: "turn_end"; message?: RawMessage }
  | { type: "agent_end"; willRetry?: boolean }
  | { type: "session_info_changed"; name?: string }
  // Command responses
  | { type: "response"; command: "get_state"; success: true; data: AgentSessionState }
  | { type: "response"; command: "get_available_models"; success: true; data: { models: AgentModel[] } }
  | { type: "response"; command: "get_model_catalog"; success: true; data: ModelCatalog }
  | { type: "response"; command: "list_sessions"; success: true; data: { sessions: AgentSessionInfo[] } }
  | { type: "response"; command: "set_model"; success: true; data: ModelSelectionState }
  | { type: "response"; command: "get_messages"; success: true; data: { messages: RawMessage[] } }
  | { type: "response"; command: "switch_session"; success: true; data: { cancelled: boolean } }
  | { type: "response"; command: "new_session"; success: true; data: { cancelled: boolean } }
  // Bridge (from electron main, not the agent)
  | { type: "bridge_ready"; cwd?: string }
  | { type: "session_cwd"; cwd: string }
  | { type: "bridge_error"; error: string }
  | { type: "bridge_exit"; code: number | null }
  // Anything else we don't handle yet
  | { type: "other" }

// ── Mapping ──────────────────────────────────────────────────────────────────

/** Map an assistant message's content array to renderable Blocks. */
export function mapAssistantContent(content: ContentPart[] | undefined): Block[] {
  if (!content) return []
  const blocks: Block[] = []
  for (const part of content) {
    if (part.type === "text" && part.text) {
      blocks.push({ type: "text", text: part.text })
    } else if (part.type === "thinking" && part.thinking) {
      blocks.push({ type: "thinking", text: part.thinking })
    } else if (part.type === "tool" && part.tool) {
      blocks.push({
        type: "tool",
        tool: part.tool as Extract<Block, { type: "tool" }>["tool"],
        title: part.title ?? "",
        meta: part.meta,
        status: part.status ?? "done",
        output: part.output,
      })
    }
  }
  return blocks
}

/** The cumulative assistant content carried by a streaming/turn event, if any. */
export function assistantContentFromEvent(event: AgentEvent): ContentPart[] | undefined {
  if (event.type === "message_update") return event.assistantMessageEvent?.partial?.content
  if (event.type === "turn_end") return event.message?.content
  return undefined
}

/** Map a full agent message history to the UI's Message[] (for loading a session). */
export function mapMessages(raw: RawMessage[]): Message[] {
  const out: Message[] = []
  raw.forEach((m, i) => {
    if (m.role === "user") {
      const text = (m.content ?? [])
        .filter((c) => c.type === "text" && c.text)
        .map((c) => c.text)
        .join("\n")
      out.push({ id: `h-${i}`, role: "user", text })
    } else if (m.role === "assistant") {
      out.push({ id: `h-${i}`, role: "assistant", blocks: mapAssistantContent(m.content) })
    }
  })
  return out
}
