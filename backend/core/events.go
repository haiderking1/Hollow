package core

import "encoding/json"

// Event is emitted by the backend and consumed by any frontend.
type Event struct {
	Kind string
	Data any
}

const (
	EventUserMessage      = "user_message"
	EventAssistantStart          = "assistant_start"
	EventAssistantThinkingDelta  = "assistant_thinking_delta"
	EventAssistantDelta          = "assistant_delta"
	EventAssistantMessage        = "assistant_message"
	EventToolStart    = "tool_start"
	EventToolDelta    = "tool_delta" // incremental tool output (e.g. live bash stdout/stderr)
	EventToolResult   = "tool_result"
	EventToolActivity = "tool_activity" // legacy
	EventError            = "error"
	EventSystem           = "system"

	// legacy
	EventLog       = "log"
	EventPhase     = "phase"
	EventUncUpdate = "uncertainty_update"

	// v2 evidence runtime
	EventEvidenceAppend   = "evidence_append"
	EventObligationUpdate = "obligation_update"

	EventCompactionStart = "compaction_start"
	EventCompactionEnd   = "compaction_end"

	EventBranchSummaryStart = "branch_summary_start"
	EventBranchSummaryEnd   = "branch_summary_end"
)

// RuntimeNoticePrefix marks runtime-injected continuation messages (e.g. the
// turn-incomplete notice). They are real user-role messages for the model but
// internal plumbing for humans — frontends must not render them in the chat.
const RuntimeNoticePrefix = "[enough-runtime] "

type LogEntry struct {
	Level   string
	Message string
}

// ToolCallEvent carries structured tool UI data to the frontend.
type ToolCallEvent struct {
	ID      string
	Name    string
	Args    string
	Result  string
	Error   bool
	Details json.RawMessage
}

// EvidenceEvent is a sanitized ledger entry for the UI: paths, kinds, and
// counts only — never file contents.
type EvidenceEvent struct {
	Kind  string
	Path  string
	Count int // total ledger entries this turn
}

// ObligationItem is one obligation row for the UI.
type ObligationItem struct {
	Kind        string
	Description string
	Closed      bool
}

// ObligationEvent is a full snapshot of the current turn's obligations.
type ObligationEvent struct {
	Open   int
	Closed int
	Items  []ObligationItem
}

type CompactionStartEvent struct {
	Reason string
}

type CompactionEndEvent struct {
	Reason       string
	Result       any // will be cast to *session.CompactionResult
	Aborted      bool
	WillRetry    bool
	ErrorMessage string
}

type BranchSummaryStartEvent struct {
	TargetID string
}

type BranchSummaryEndEvent struct {
	TargetID     string
	Result       any // will be cast to *session.BranchSummaryResult
	Aborted      bool
	ErrorMessage string
}
