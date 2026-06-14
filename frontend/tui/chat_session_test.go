package tui

import (
	"strings"
	"testing"

	"github.com/enough/enough/backend/core"
	"github.com/enough/enough/backend/session"
)

func TestChatMsgFromSessionLineIncludesImages(t *testing.T) {
	line := session.ChatLine{
		Role: "user",
		Text: "look at this",
		Images: []session.ChatImage{
			{URL: "data:image/png;base64,abc"},
		},
	}
	msg, ok := chatMsgFromSessionLine(line, false)
	if !ok {
		t.Fatal("expected chat message")
	}
	if len(msg.images) != 1 || msg.images[0].URL != "data:image/png;base64,abc" {
		t.Fatalf("unexpected images: %+v", msg.images)
	}
}

func TestChatMsgFromSessionLineSkipsRuntimeNotice(t *testing.T) {
	line := session.ChatLine{
		Role: "user",
		Text: core.RuntimeNoticePrefix + " internal",
	}
	_, ok := chatMsgFromSessionLine(line, true)
	if ok {
		t.Fatal("expected runtime notice to be skipped")
	}
	_, ok = chatMsgFromSessionLine(line, false)
	if !ok {
		t.Fatal("expected runtime notice when skip disabled")
	}
}

func TestChatMsgFromSessionLinePreservesToolDetails(t *testing.T) {
	details := `{"action":"list","tabs":[{"id":"TAB1","title":"Example","url":"https://example.com","type":"page"}]}`
	line := session.ChatLine{
		Role:        "tool",
		ToolName:    "browser",
		ToolArgs:    `{"action":"list"}`,
		ToolResult:  "TAB1 [page] Example - https://example.com",
		ToolDetails: details,
	}
	msg, ok := chatMsgFromSessionLine(line, false)
	if !ok {
		t.Fatal("expected chat message")
	}
	if msg.toolDetails != details {
		t.Fatalf("toolDetails = %q, want %q", msg.toolDetails, details)
	}
	if strings.Contains(msg.toolResult, "--METADATA--") {
		t.Fatalf("toolResult should stay clean, got %q", msg.toolResult)
	}

	row := parseToolRow(chatMsg{
		role:        msg.role,
		toolName:    msg.toolName,
		toolArgs:    msg.toolArgs,
		toolResult:  msg.toolResult,
		toolDetails: msg.toolDetails,
	})
	lines := renderBrowserBlock(NewStyles(), row, 80, false)
	plain := strings.Join(lines, "\n")
	if !strings.Contains(plain, "1 tab(s)") {
		t.Fatalf("expected tab count from persisted details, got:\n%s", plain)
	}
}

func TestChatMsgFromSessionLineLegacyMetadataFallback(t *testing.T) {
	details := `{"action":"list","tabs":[{"id":"TAB1"}]}`
	line := session.ChatLine{
		Role:       "tool",
		ToolName:   "browser",
		ToolArgs:   `{"action":"list"}`,
		ToolResult: "TAB1 [page] Example\n\n--METADATA--\n" + details,
	}
	msg, ok := chatMsgFromSessionLine(line, false)
	if !ok {
		t.Fatal("expected chat message")
	}
	if msg.toolDetails != details {
		t.Fatalf("toolDetails = %q, want legacy metadata %q", msg.toolDetails, details)
	}
	if strings.Contains(msg.toolResult, "--METADATA--") {
		t.Fatalf("legacy metadata should be stripped from toolResult, got %q", msg.toolResult)
	}
}

func TestChatMsgFromSessionLinePrefersStoredDetailsOverLegacy(t *testing.T) {
	stored := `{"action":"list","tabs":[{"id":"TAB1"},{"id":"TAB2"}]}`
	legacy := `{"action":"list","tabs":[{"id":"OLD"}]}`
	line := session.ChatLine{
		Role:        "tool",
		ToolName:    "browser",
		ToolResult:  "tabs\n\n--METADATA--\n" + legacy,
		ToolDetails: stored,
	}
	msg, ok := chatMsgFromSessionLine(line, false)
	if !ok {
		t.Fatal("expected chat message")
	}
	if msg.toolDetails != stored {
		t.Fatalf("stored ToolDetails should win over legacy suffix, got %q", msg.toolDetails)
	}
}

func TestChatMsgFromSessionLinePreservesText(t *testing.T) {
	line := session.ChatLine{Role: "user", Text: "hello"}
	msg, ok := chatMsgFromSessionLine(line, false)
	if !ok || msg.text != "hello" {
		t.Fatalf("unexpected msg: %+v ok=%v", msg, ok)
	}
	if strings.TrimSpace(msg.toolResult) != "" {
		t.Fatalf("unexpected tool result on user line: %q", msg.toolResult)
	}
}
