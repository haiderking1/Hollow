package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/enough/enough/backend/config"
	"github.com/enough/enough/backend/core"
	"github.com/enough/enough/backend/opencode"
)

func TestLoopPromptAndContinueInjectHiddenRuntimeNotices(t *testing.T) {
	requests := 0
	srv := scriptedServer(t, func(req opencode.ChatRequest) (string, []toolCallJSON) {
		requests++
		last := lastMessageText(req)
		if !strings.HasPrefix(last, core.RuntimeNoticePrefix) {
			t.Fatalf("request %d last message is not a runtime notice: %q", requests, last)
		}
		if !strings.Contains(last, "fix auth") {
			t.Fatalf("request %d lost the locked prompt: %q", requests, last)
		}
		if requests == 1 && !strings.Contains(last, "iteration 1") {
			t.Fatalf("first notice missing iteration: %q", last)
		}
		if requests == 2 && !strings.Contains(last, "iteration 2") {
			t.Fatalf("continuation notice missing iteration: %q", last)
		}
		return "working", nil
	})
	defer srv.Close()

	cfg := config.Runtime{
		Endpoint: srv.URL,
		APIKey:   "k",
		Model:    "test-model",
	}
	a := New(cfg, t.TempDir(), nil)

	if err := a.LoopPrompt(context.Background(), cfg, "fix auth", func(core.Event) {}); err != nil {
		t.Fatalf("LoopPrompt: %v", err)
	}
	if err := a.LoopContinue(context.Background(), cfg, "fix auth", 2, func(core.Event) {}); err != nil {
		t.Fatalf("LoopContinue: %v", err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}

	runtimeNotices := 0
	visibleUserMessages := 0
	for _, msg := range a.messages {
		if msg.Role != "user" {
			continue
		}
		text := opencode.ContentString(msg)
		if strings.HasPrefix(text, core.RuntimeNoticePrefix) {
			runtimeNotices++
		} else {
			visibleUserMessages++
		}
	}
	if runtimeNotices != 2 {
		t.Fatalf("runtime notices = %d, want 2", runtimeNotices)
	}
	if visibleUserMessages != 1 {
		t.Fatalf("visible user messages = %d, want 1", visibleUserMessages)
	}
}

func TestLoopRuntimeNoticeUsesCustomPromise(t *testing.T) {
	notice := loopRuntimeNotice("finish with <promise>COMPLETE</promise>", 3, true)
	if !strings.Contains(notice, "<promise>COMPLETE</promise>") {
		t.Fatalf("custom promise missing from notice: %q", notice)
	}
	if !strings.Contains(notice, "GOAL LOCK") {
		t.Fatalf("goal lock missing from notice: %q", notice)
	}
}
