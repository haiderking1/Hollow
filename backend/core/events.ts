// Event kind constants and payload shapes for the agent pub/sub bus.

export type event = {
  kind: string;
  data: unknown;
};

export const event_user_message = "user_message";
export const event_assistant_start = "assistant_start";
export const event_assistant_thinking_delta = "assistant_thinking_delta";
export const event_assistant_delta = "assistant_delta";
export const event_assistant_message = "assistant_message";
export const event_tool_start = "tool_start";
// incremental tool output (e.g. live bash stdout/stderr)
export const event_tool_delta = "tool_delta";
export const event_tool_result = "tool_result";
// legacy
export const event_tool_activity = "tool_activity";
export const event_error = "error";
export const event_system = "system";

// legacy
export const event_log = "log";
export const event_phase = "phase";
export const event_unc_update = "uncertainty_update";

// v2 evidence runtime
export const event_evidence_append = "evidence_append";
export const event_obligation_update = "obligation_update";

export const event_compaction_start = "compaction_start";
export const event_compaction_end = "compaction_end";

// Terminal marker for a dispatched prompt/command. The desktop bridge emits it
// once the runtime prompt effect settles (data.request_id = the dispatch id);
// the renderer maps it to "done".
export const event_request_end = "request_end";

export const event_branch_summary_start = "branch_summary_start";
export const event_branch_summary_end = "branch_summary_end";

export const event_workflow_start = "workflow_start";
export const event_workflow_phase = "workflow_phase";
export const event_workflow_agent_start = "workflow_agent_start";
export const event_workflow_agent_delta = "workflow_agent_delta";
export const event_workflow_agent_end = "workflow_agent_end";
export const event_workflow_paused = "workflow_paused";
export const event_workflow_end = "workflow_end";

// RuntimeNoticePrefix marks runtime-injected continuation messages (e.g. the
// turn-incomplete notice). They are real user-role messages for the model but
// internal plumbing for humans — frontends must not render them in the chat.
export const runtime_notice_prefix = "[hollow-runtime] ";

export type log_entry = {
  level: string;
  message: string;
};

// ToolCallEvent carries structured tool UI data to the frontend.
export type tool_call_event = {
  id: string;
  name: string;
  args: string;
  result: string;
  error: boolean;
  details: Uint8Array; // json.RawMessage
};

// EvidenceEvent is a sanitized ledger entry for the UI: paths, kinds, and
// counts only — never file contents.
export type evidence_event = {
  kind: string;
  path: string;
  count: number; // total ledger entries this turn
};

// ObligationItem is one obligation row for the UI.
export type obligation_item = {
  kind: string;
  description: string;
  closed: boolean;
};

// ObligationEvent is a full snapshot of the current turn's obligations.
export type obligation_event = {
  open: number;
  closed: number;
  items: obligation_item[];
};

export type compaction_start_event = {
  reason: string;
  request_id: string;
  operation_id: string;
  session_id: string;
};

export type compaction_end_event = {
  reason: string;
  result: unknown; // will be cast to *session.CompactionResult
  aborted: boolean;
  will_retry: boolean;
  error_message: string;
  request_id: string;
  operation_id: string;
  session_id: string;
};

export type branch_summary_start_event = {
  target_id: string;
};

export type branch_summary_end_event = {
  target_id: string;
  result: unknown; // will be cast to *session.BranchSummaryResult
  aborted: boolean;
  error_message: string;
};

export type workflow_run_event = {
  id: string;
  name: string;
  description: string;
  script_path: string;
  status: string;
  phase: string;
  phases: string[];
  queued: number;
  running: number;
  done: number;
  failed: number;
  tokens: number;
  started_at: Date; // time.Time
  elapsed: number; // time.Duration
  message: string;
};

export type workflow_agent_event = {
  workflow_id: string;
  phase: string;
  key: string;
  role: string;
  status: string;
  prompt: string;
  tool: tool_call_event;
  result: string;
  json: unknown;
  error: string;
  tokens: number;
  turns: number;
};
