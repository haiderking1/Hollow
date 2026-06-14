package opencode

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func thinkTag(open bool) string {
	if open {
		return string([]byte{0x3c, 0x74, 0x68, 0x69, 0x6e, 0x6b, 0x3e})
	}
	return string([]byte{0x3c, 0x2f, 0x74, 0x68, 0x69, 0x6e, 0x6b, 0x3e})
}

func TestSplitEmbeddedThinking(t *testing.T) {
	open := thinkTag(true)
	close := thinkTag(false)

	t.Run("minimax style", func(t *testing.T) {
		raw := open + "Haider is just thinking, no action needed." + close + "bro what you scheming"
		text, thinking := SplitEmbeddedThinking(raw)
		if thinking != "Haider is just thinking, no action needed." {
			t.Fatalf("thinking = %q", thinking)
		}
		if text != "bro what you scheming" {
			t.Fatalf("text = %q", text)
		}
	})

	t.Run("plain text unchanged", func(t *testing.T) {
		text, thinking := SplitEmbeddedThinking("hello world")
		if thinking != "" || text != "hello world" {
			t.Fatalf("text=%q thinking=%q", text, thinking)
		}
	})

	t.Run("multiple blocks", func(t *testing.T) {
		raw := open + "one" + close + " middle " + open + "two" + close + " end"
		text, thinking := SplitEmbeddedThinking(raw)
		if thinking != "onetwo" {
			t.Fatalf("thinking = %q", thinking)
		}
		if text != "middle end" {
			t.Fatalf("text = %q", text)
		}
	})
}

func TestSanitizeEmbeddedThinking(t *testing.T) {
	open := thinkTag(true)
	close := thinkTag(false)

	msg := Message{
		Role:    "assistant",
		Content: StringContent(open + "secret" + close + "visible"),
	}
	SanitizeEmbeddedThinking(&msg)
	if ContentString(msg) != "visible" {
		t.Fatalf("content = %q", ContentString(msg))
	}
	if msg.GetReasoning() != "secret" {
		t.Fatalf("reasoning = %q", msg.GetReasoning())
	}
}

func TestThinkStreamSplitter(t *testing.T) {
	open := thinkTag(true)
	close := thinkTag(false)

	var textParts, thinkParts []string
	var splitter thinkStreamSplitter
	emitText := func(s string) { textParts = append(textParts, s) }
	emitThink := func(s string) { thinkParts = append(thinkParts, s) }

	// Split deltas mid-word and mid-tag.
	chunk1 := open + "pla"
	chunk2 := "nning" + close + "ans"
	chunk3 := "wer"

	splitter.feed(chunk1, emitText, emitThink)
	splitter.feed(chunk2, emitText, emitThink)
	splitter.feed(chunk3, emitText, emitThink)
	splitter.flush(emitText, emitThink)

	if strings.Join(thinkParts, "") != "planning" {
		t.Fatalf("thinking = %q", strings.Join(thinkParts, ""))
	}
	if strings.Join(textParts, "") != "answer" {
		t.Fatalf("text = %q", strings.Join(textParts, ""))
	}
}

func TestChatStreamSplitsEmbeddedThinking(t *testing.T) {
	open := thinkTag(true)
	close := thinkTag(false)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		chunk1, _ := json.Marshal(map[string]any{
			"choices": []map[string]any{{"delta": map[string]string{"content": open + "plan"}}},
		})
		chunk2, _ := json.Marshal(map[string]any{
			"choices": []map[string]any{{"delta": map[string]string{"content": "ning" + close + "hi"}}},
		})
		chunk3, _ := json.Marshal(map[string]any{
			"choices": []map[string]any{{"delta": map[string]any{}, "finish_reason": "stop"}},
		})
		for _, chunk := range [][]byte{chunk1, chunk2, chunk3} {
			_, _ = w.Write([]byte("data: "))
			_, _ = w.Write(chunk)
			_, _ = w.Write([]byte("\n\n"))
			flusher.Flush()
		}
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "test-key", "minimax-m3")
	var textDeltas, thinkDeltas []string
	msg, err := client.ChatStream(context.Background(), ChatRequest{
		Model: "minimax-m3",
		Messages: []Message{
			{Role: "user", Content: StringContent("hi")},
		},
	}, StreamCallbacks{
		OnText:     func(s string) { textDeltas = append(textDeltas, s) },
		OnThinking: func(s string) { thinkDeltas = append(thinkDeltas, s) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(thinkDeltas, "") != "planning" {
		t.Fatalf("think deltas = %q", strings.Join(thinkDeltas, ""))
	}
	if strings.Join(textDeltas, "") != "hi" {
		t.Fatalf("text deltas = %q", strings.Join(textDeltas, ""))
	}
	if ContentString(msg) != "hi" {
		t.Fatalf("msg content = %q", ContentString(msg))
	}
	if msg.GetReasoning() != "planning" {
		t.Fatalf("msg reasoning = %q", msg.GetReasoning())
	}
}
