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
