package session

import (
	"strings"
	"time"

	"github.com/enough/enough/backend/opencode"
)

type EntryType string

const (
	TypeSession             EntryType = "session"
	TypeMessage             EntryType = "message"
	TypeThinkingLevelChange EntryType = "thinking_level_change"
	TypeModelChange         EntryType = "model_change"
	TypeCompaction          EntryType = "compaction"
	TypeBranchSummary       EntryType = "branch_summary"
	TypeCustom              EntryType = "custom"
	TypeCustomMessage       EntryType = "custom_message"
	TypeLabel               EntryType = "label"
	TypeSessionInfo         EntryType = "session_info"
	// TypeSystemPrompt stores the session's cached system prompt so resumed
	// sessions replay the byte-identical prompt (prefix-cache invariant).
	TypeSystemPrompt        EntryType = "system_prompt"
)

type SessionEntry struct {
	Type          EntryType         `json:"type"`
	ID            string            `json:"id"`
	ParentID      *string           `json:"parentId"`
	Timestamp     string            `json:"timestamp"`

	// message fields
	Message       *opencode.Message `json:"message,omitempty"`
	ToolDetails   string            `json:"toolDetails,omitempty"`

	// thinking_level_change
	ThinkingLevel string            `json:"thinkingLevel,omitempty"`

	// model_change
	Provider      string            `json:"provider,omitempty"`
	ModelID       string            `json:"modelId,omitempty"`

	// compaction
	Summary          string         `json:"summary,omitempty"`
	FirstKeptEntryID string         `json:"firstKeptEntryId,omitempty"`
	TokensBefore     int            `json:"tokensBefore,omitempty"`
	Details          any            `json:"details,omitempty"`
	FromHook         bool           `json:"fromHook,omitempty"`

	// branch_summary
	FromID           string         `json:"fromId,omitempty"`

	// custom, custom_message
	CustomType       string         `json:"customType,omitempty"`
	Data             any            `json:"data,omitempty"`
	Content          any            `json:"content,omitempty"`
	Display          *bool          `json:"display,omitempty"`

	// label
	TargetID         string         `json:"targetId,omitempty"`
	Label            string         `json:"label,omitempty"`

	// session_info
	Name             string         `json:"name,omitempty"`
}

type FileEntry struct {
	SessionEntry

	// Header fields (for type: "session")
	Version int    `json:"version,omitempty"`
	CWD     string `json:"cwd,omitempty"`
}

type Header struct {
	Type      string `json:"type"`
	Version   int    `json:"version,omitempty"`
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	CWD       string `json:"cwd"`
}

type MessageEntry struct {
	Type      string           `json:"type"`
	ID        string           `json:"id"`
	ParentID  *string          `json:"parentId"`
	Timestamp string           `json:"timestamp"`
	Message   opencode.Message `json:"message"`
}

type ChatImage struct {
	URL      string // data URL
	MIMEType string
	Width    int
	Height   int
}

// ChatLine is a TUI-friendly view of a persisted message or compaction summary.
type ChatLine struct {
	Role         string
	Text         string
	Thinking     string
	ToolName     string
	ToolArgs     string
	ToolResult   string
	ToolDetails  string
	ToolError    bool
	TokensBefore int
	Images       []ChatImage
}

func nowISO() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func fileTimestamp(t time.Time) string {
	return strings.NewReplacer(":", "-", ".", "-").Replace(t.UTC().Format(time.RFC3339Nano))
}
