package core

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
	EventToolActivity     = "tool_activity"
	EventError            = "error"
	EventSystem           = "system"

	// legacy
	EventLog       = "log"
	EventPhase     = "phase"
	EventUncUpdate = "uncertainty_update"
)

type LogEntry struct {
	Level   string
	Message string
}
