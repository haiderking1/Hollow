// PORT STATUS: active
// source path: runtime/schemas.ts
// confidence: high
// status: phase_b_compile

import * as Schema from "@effect/schema/Schema";

// ==========================================
// Command Schemas (mirroring serve.go types)
// ==========================================

export const ListSessions = Schema.Struct({
  type: Schema.Literal("listSessions"),
});
export type ListSessions = Schema.Schema.Type<typeof ListSessions>;

export const OpenSession = Schema.Struct({
  type: Schema.Literal("openSession"),
  id: Schema.String,
});
export type OpenSession = Schema.Schema.Type<typeof OpenSession>;

export const NewSession = Schema.Struct({
  type: Schema.Literal("newSession"),
  cwd: Schema.optional(Schema.String),
});
export type NewSession = Schema.Schema.Type<typeof NewSession>;

export const DeleteSession = Schema.Struct({
  type: Schema.Literal("deleteSession"),
  id: Schema.String,
});
export type DeleteSession = Schema.Schema.Type<typeof DeleteSession>;

export const AttachmentSchema = Schema.Struct({
  mime: Schema.String,
  data: Schema.String, // base64
});
export type AttachmentSchema = Schema.Schema.Type<typeof AttachmentSchema>;

export const Prompt = Schema.Struct({
  type: Schema.Literal("prompt"),
  text: Schema.String,
  cwd: Schema.optional(Schema.String),
  attachments: Schema.optional(Schema.Array(AttachmentSchema)),
});
export type Prompt = Schema.Schema.Type<typeof Prompt>;

export const Interrupt = Schema.Struct({
  type: Schema.Literal("interrupt"),
});
export type Interrupt = Schema.Schema.Type<typeof Interrupt>;

export const SetModel = Schema.Struct({
  type: Schema.Literal("setModel"),
  provider: Schema.String,
  model: Schema.String,
  thinkingLevel: Schema.optional(Schema.String),
});
export type SetModel = Schema.Schema.Type<typeof SetModel>;

export const ListModels = Schema.Struct({
  type: Schema.Literal("listModels"),
});
export type ListModels = Schema.Schema.Type<typeof ListModels>;

export const ToggleModelEnabled = Schema.Struct({
  type: Schema.Literal("toggleModelEnabled"),
  modelId: Schema.String,
});
export type ToggleModelEnabled = Schema.Schema.Type<typeof ToggleModelEnabled>;

// One provider's connection state, surfaced to the Settings panel.
export const ConnectionInfo = Schema.Struct({
  provider: Schema.String, // provider id (e.g. "opencode-go")
  displayName: Schema.String, // "OpenCode Go"
  kind: Schema.Literal("key", "oauth"), // key = pasteable API key, oauth = codex device flow
  connected: Schema.Boolean,
});
export type ConnectionInfo = Schema.Schema.Type<typeof ConnectionInfo>;

export const ListConnections = Schema.Struct({
  type: Schema.Literal("listConnections"),
});
export type ListConnections = Schema.Schema.Type<typeof ListConnections>;

export const SetApiKey = Schema.Struct({
  type: Schema.Literal("setApiKey"),
  provider: Schema.String,
  key: Schema.String,
});
export type SetApiKey = Schema.Schema.Type<typeof SetApiKey>;

export const RemoveKey = Schema.Struct({
  type: Schema.Literal("removeKey"),
  provider: Schema.String,
});
export type RemoveKey = Schema.Schema.Type<typeof RemoveKey>;

export const StartCodexLogin = Schema.Struct({
  type: Schema.Literal("startCodexLogin"),
});
export type StartCodexLogin = Schema.Schema.Type<typeof StartCodexLogin>;

export const CancelCodexLogin = Schema.Struct({
  type: Schema.Literal("cancelCodexLogin"),
});
export type CancelCodexLogin = Schema.Schema.Type<typeof CancelCodexLogin>;

// Composer status bar: git diff shortstat + current branch for the session's
// cwd. Optional cwd falls back to the runtime's workDir. Pure local git calls.
export const RepoStatus = Schema.Struct({
  type: Schema.Literal("repoStatus"),
  cwd: Schema.optional(Schema.String),
});
export type RepoStatus = Schema.Schema.Type<typeof RepoStatus>;

export const DesktopCommand = Schema.Union(
  ListSessions,
  OpenSession,
  NewSession,
  DeleteSession,
  Prompt,
  Interrupt,
  SetModel,
  ListModels,
  ToggleModelEnabled,
  ListConnections,
  SetApiKey,
  RemoveKey,
  StartCodexLogin,
  CancelCodexLogin,
  RepoStatus,
);
export type DesktopCommand = Schema.Schema.Type<typeof DesktopCommand>;

// ==========================================
// Event Schemas (subset of backend/core/events.ts)
// ==========================================

export const AssistantStartEvent = Schema.Struct({
  kind: Schema.Literal("assistant_start"),
  data: Schema.Unknown,
});

export const AssistantDeltaEvent = Schema.Struct({
  kind: Schema.Literal("assistant_delta"),
  data: Schema.String,
});

export const ToolStartEvent = Schema.Struct({
  kind: Schema.Literal("tool_start"),
  data: Schema.Unknown,
});

export const ToolDeltaEvent = Schema.Struct({
  kind: Schema.Literal("tool_delta"),
  data: Schema.Unknown,
});

export const ToolResultEvent = Schema.Struct({
  kind: Schema.Literal("tool_result"),
  data: Schema.Unknown,
});

export const ErrorEvent = Schema.Struct({
  kind: Schema.Literal("error"),
  data: Schema.String,
});

export const SystemEvent = Schema.Struct({
  kind: Schema.Literal("system"),
  data: Schema.String,
});

export const CompactionStartEvent = Schema.Struct({
  kind: Schema.Literal("compaction_start"),
  data: Schema.Unknown,
});

export const CompactionEndEvent = Schema.Struct({
  kind: Schema.Literal("compaction_end"),
  data: Schema.Unknown,
});

// Bridge-emitted (not an agent event): connection state changed, e.g. after a
// key was saved/removed or the Codex OAuth login completed. data = the same
// payload the listConnections/setApiKey dispatch responses carry, plus an
// optional `error` string when a background Codex login failed.
export const ConnectionChangedEvent = Schema.Struct({
  kind: Schema.Literal("connection.changed"),
  data: Schema.Unknown,
});

export const DesktopEvent = Schema.Union(
  AssistantStartEvent,
  AssistantDeltaEvent,
  ToolStartEvent,
  ToolDeltaEvent,
  ToolResultEvent,
  ErrorEvent,
  SystemEvent,
  CompactionStartEvent,
  CompactionEndEvent,
  ConnectionChangedEvent
);
export type DesktopEvent = Schema.Schema.Type<typeof DesktopEvent>;

// ==========================================
// Decode/Encode Helpers
// ==========================================

export const decodeCommand = Schema.decodeUnknownOption(DesktopCommand);
export const encodeCommand = Schema.encode(DesktopCommand);

export const decodeEvent = Schema.decodeUnknownOption(DesktopEvent);
export const encodeEvent = Schema.encode(DesktopEvent);
