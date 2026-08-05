// ── Chat / transcript types ─────────────────────────────────────────────────

export type Role = "user" | "assistant"

export type ToolStatus = "running" | "done" | "error"

export interface DiffLine {
  type: "add" | "remove" | "context"
  text: string
}

export interface Diff {
  file: string
  added: number
  removed: number
  lines: DiffLine[]
}

export interface TodoItem {
  text: string
  status: "pending" | "in_progress" | "done"
}

export type ToolVerb = "Write" | "Edit" | "Read" | "Bash" | "Grep" | "Search" | "Web Search" | "Browser" | "Task" | "Swarm Agent" | "Fetch" | "Glob"

export type Block =
  | { type: "text"; text: string }
  | { type: "thinking"; text: string }
  | {
      type: "tool"
      tool: ToolVerb
      /** monospace label shown next to the verb, e.g. a file path or command */
      title: string
      /** secondary monospace meta, e.g. "+6 -1" or "exit 0" */
      meta?: string
      status: ToolStatus
      /** terminal-style output revealed when expanded */
      output?: string
      diff?: Diff
    }
  | { type: "todo"; items: TodoItem[] }

export interface UserMessage {
  id: string
  role: "user"
  text: string
}

export interface AssistantMessage {
  id: string
  role: "assistant"
  blocks: Block[]
  /** is this turn still streaming */
  streaming?: boolean
  /** footer stats shown under the turn */
  duration?: string
  tokens?: string
}

export type CompactionStatus = "running" | "success" | "error" | "cancelled"

export interface CompactionMessage {
  id: string
  role: "compaction"
  requestId: string
  operationId: string
  sessionId?: string
  status: CompactionStatus
  reason?: string
  summary?: string
  tokensBefore?: number
  estimatedTokensAfter?: number
  errorMessage?: string
  cancellationPending?: boolean
  willRetry?: boolean
}

export type Message = UserMessage | AssistantMessage | CompactionMessage

/** Composer status-bar snapshot: uncommitted git changes, current branch, context-window fill. */
export interface RepoStatus {
  type: "repoStatus"
  added: number
  removed: number
  branch: string
  contextPct: number
}
