package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/enough/enough/backend/opencode"
)

func TestContinueRecentRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cwd := filepath.Join(dir, "proj")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}

	origHome := os.Getenv("HOME")
	t.Setenv("HOME", dir)

	sm, err := ContinueRecent(cwd)
	if err != nil {
		t.Fatal(err)
	}

	user := opencode.Message{Role: "user", Content: opencode.StringContent("hello")}
	if err := sm.AppendMessage(user); err != nil {
		t.Fatal(err)
	}
	assistant := opencode.Message{Role: "assistant", Content: opencode.StringContent("hi there")}
	if err := sm.AppendMessage(assistant); err != nil {
		t.Fatal(err)
	}

	if sm.SessionFile() == "" {
		t.Fatal("expected session file path")
	}
	if _, err := os.Stat(sm.SessionFile()); err != nil {
		t.Fatalf("session file missing: %v", err)
	}

	sm2, err := ContinueRecent(cwd)
	if err != nil {
		t.Fatal(err)
	}

	msgs := sm2.Messages()
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2", len(msgs))
	}
	if opencode.ContentString(msgs[0]) != "hello" {
		t.Fatalf("user message = %q", opencode.ContentString(msgs[0]))
	}
	if opencode.ContentString(msgs[1]) != "hi there" {
		t.Fatalf("assistant message = %q", opencode.ContentString(msgs[1]))
	}

	_ = origHome
}

func TestEncodeCWD(t *testing.T) {
	got := EncodeCWD("/home/idk/projects/Enough")
	want := "--home-idk-projects-Enough--"
	if got != want {
		t.Fatalf("EncodeCWD = %q, want %q", got, want)
	}
}

func TestAppendMessageWithDetailsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cwd := filepath.Join(dir, "proj")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", dir)

	sm, err := ContinueRecent(cwd)
	if err != nil {
		t.Fatal(err)
	}

	assistantCall := opencode.Message{
		Role:    "assistant",
		Content: opencode.StringContent("call browser"),
		ToolCalls: []opencode.ToolCall{{
			ID:   "c1",
			Type: "function",
			Function: opencode.ToolCallFunction{
				Name:      "browser",
				Arguments: `{"action":"list"}`,
			},
		}},
	}
	if err := sm.AppendMessage(assistantCall); err != nil {
		t.Fatal(err)
	}

	toolResponse := opencode.Message{
		Role:       "tool",
		ToolCallID: "c1",
		Name:       "browser",
		Content:    opencode.StringContent("TAB1 Example"),
	}
	details := `{"action":"list","tabs":[{"id":"TAB1"}]}`
	if err := sm.AppendMessageWithDetails(toolResponse, details); err != nil {
		t.Fatal(err)
	}

	sm2, err := ContinueRecent(cwd)
	if err != nil {
		t.Fatal(err)
	}

	lines := sm2.ChatLines()
	var toolLine *ChatLine
	for i := range lines {
		if lines[i].Role == "tool" {
			toolLine = &lines[i]
			break
		}
	}

	if toolLine == nil {
		t.Fatal("expected tool line, got none")
	}
	if toolLine.ToolDetails != details {
		t.Errorf("expected details %q, got %q", details, toolLine.ToolDetails)
	}
}
