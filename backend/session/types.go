package session

import (
	"strings"
	"time"

	"github.com/enough/enough/backend/opencode"
)

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

// ChatLine is a TUI-friendly view of a persisted message.
type ChatLine struct {
	Role     string
	Text     string
	Thinking string
}

func nowISO() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func fileTimestamp(t time.Time) string {
	return strings.NewReplacer(":", "-", ".", "-").Replace(t.UTC().Format(time.RFC3339Nano))
}
