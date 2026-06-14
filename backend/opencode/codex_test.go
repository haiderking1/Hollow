package opencode

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseResponsesMessage(t *testing.T) {
	raw := `{
		"status": "completed",
		"output": [
			{
				"type": "message",
				"role": "assistant",
				"content": [{"type": "output_text", "text": "hello codex"}]
			},
			{
				"type": "function_call",
				"call_id": "call_abc",
				"name": "bash",
				"arguments": "{\"command\":\"ls\"}",
				"status": "completed"
			}
		],
		"usage": {"input_tokens": 3, "output_tokens": 5, "total_tokens": 8}
	}`
	var resp responsesResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatal(err)
	}
	msg, err := parseResponsesMessage(resp)
	if err != nil {
		t.Fatal(err)
	}
	if len(msg.Content) == 0 || ContentString(msg) != "hello codex" {
		t.Fatalf("content = %s", ContentString(msg))
	}
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].Function.Name != "bash" {
		t.Fatalf("tool calls = %#v", msg.ToolCalls)
	}
	if msg.Usage == nil || msg.Usage.Input != 3 {
		t.Fatalf("usage = %#v", msg.Usage)
	}
}

func TestMessagesToResponsesInput(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: StringContent("hi")},
		{Role: "assistant", Content: StringContent("hey"), ToolCalls: []ToolCall{{
			ID: "call_1", Type: "function", Function: ToolCallFunction{Name: "bash", Arguments: `{}`},
		}}},
		{Role: "tool", ToolCallID: "call_1", Content: StringContent("ok")},
	}
	items := messagesToResponsesInput(msgs)
	if len(items) != 4 {
		t.Fatalf("expected 4 items, got %d", len(items))
	}
}

func TestBuildResponsesRequestInstructions(t *testing.T) {
	client := NewCodexClient("https://example.com", "token", "gpt-5-codex")
	req, err := client.buildResponsesRequest(ChatRequest{
		Messages: []Message{
			{Role: "system", Content: StringContent("You are Enough.")},
			{Role: "developer", Content: StringContent("Use tools when needed.")},
			{Role: "user", Content: StringContent("hi")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if req.Instructions != "You are Enough.\n\nUse tools when needed." {
		t.Fatalf("instructions = %q", req.Instructions)
	}
	if req.Store {
		t.Fatal("expected store=false")
	}
	input, ok := req.Input.([]any)
	if !ok || len(input) != 1 {
		t.Fatalf("input = %#v", req.Input)
	}
}

func TestBuildResponsesRequestInstructionsFallback(t *testing.T) {
	client := NewCodexClient("https://example.com", "token", "gpt-5-codex")
	req, err := client.buildResponsesRequest(ChatRequest{
		Messages: []Message{{Role: "user", Content: StringContent("hi")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(req.Instructions) == "" {
		t.Fatal("expected non-empty instructions fallback")
	}
}

func TestCodexResponsesStreamTextDeltas(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req responsesRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if !req.Stream {
			t.Fatal("expected stream=true")
		}
		if strings.TrimSpace(req.Instructions) == "" {
			t.Fatal("expected instructions on codex request")
		}
		if req.Store {
			t.Fatal("expected store=false on codex request")
		}
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Fatalf("accept = %q", r.Header.Get("Accept"))
		}

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write([]byte("event: response.output_text.delta\n"))
		_, _ = w.Write([]byte(`data: {"type":"response.output_text.delta","delta":"Hello"}` + "\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("event: response.output_text.delta\n"))
		_, _ = w.Write([]byte(`data: {"type":"response.output_text.delta","delta":" world"}` + "\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte("event: response.completed\n"))
		_, _ = w.Write([]byte(`data: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}}` + "\n\n"))
		flusher.Flush()
	}))
	defer srv.Close()

	var deltas []string
	client := NewCodexClient(srv.URL, "token", "gpt-5-codex")
	msg, err := client.ChatStream(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: StringContent("hi")}},
	}, StreamCallbacks{
		OnText: func(s string) { deltas = append(deltas, s) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(deltas, "") != "Hello world" {
		t.Fatalf("deltas = %q", strings.Join(deltas, ""))
	}
	if len(msg.Content) == 0 || ContentString(msg) != "Hello world" {
		t.Fatalf("content = %s", ContentString(msg))
	}
	if msg.Usage == nil || msg.Usage.TotalTokens != 5 {
		t.Fatalf("usage = %#v", msg.Usage)
	}
}

func TestCodexResponsesStreamToolCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = w.Write([]byte(`data: {"type":"response.output_item.done","item":{"type":"function_call","call_id":"call_abc","name":"bash","arguments":"{\"command\":\"ls\"}","status":"completed"}}` + "\n\n"))
		flusher.Flush()
		_, _ = w.Write([]byte(`data: {"type":"response.completed","response":{"status":"completed"}}` + "\n\n"))
		flusher.Flush()
	}))
	defer srv.Close()

	var textDeltas []string
	client := NewCodexClient(srv.URL, "token", "gpt-5-codex")
	msg, err := client.ChatStream(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: StringContent("run ls")}},
	}, StreamCallbacks{
		OnText: func(s string) { textDeltas = append(textDeltas, s) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(textDeltas) != 0 {
		t.Fatalf("unexpected text deltas during tool call: %v", textDeltas)
	}
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].Function.Name != "bash" {
		t.Fatalf("tool calls = %#v", msg.ToolCalls)
	}
}

func TestCodexResponsesStreamReasoningDeltas(t *testing.T) {
	body := strings.Join([]string{
		`data: {"type":"response.reasoning_summary_text.delta","delta":"think"}`,
		"",
		`data: {"type":"response.output_text.delta","delta":"answer"}`,
		"",
		`data: {"type":"response.completed","response":{"status":"completed"}}`,
		"",
	}, "\n")

	state, err := consumeCodexResponsesSSE(strings.NewReader(body), StreamCallbacks{
		OnThinking: func(s string) {},
		OnText:     func(s string) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := messageFromCodexStreamState(state)
	if err != nil {
		t.Fatal(err)
	}
	if msg.ReasoningContent == nil || *msg.ReasoningContent != "think" {
		t.Fatalf("reasoning = %#v", msg.ReasoningContent)
	}
	if len(msg.Content) == 0 || ContentString(msg) != "answer" {
		t.Fatalf("content = %s", ContentString(msg))
	}
}

func TestCodexResponsesStreamFailed(t *testing.T) {
	body := `data: {"type":"response.failed","response":{"status":"failed","error":{"code":"overloaded","message":"Slow down"}}}` + "\n\n"
	state, err := consumeCodexResponsesSSE(strings.NewReader(body), StreamCallbacks{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = messageFromCodexStreamState(state)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "Slow down") {
		t.Fatalf("err = %v", err)
	}
}

func TestMessagesToResponsesInputWithImage(t *testing.T) {
	blocks := []ContentBlock{
		{Type: "text", Text: "image text description"},
		{Type: "image_url", ImageURL: &ContentImageURL{URL: "data:image/png;base64,iVBOR"}},
	}
	msgs := []Message{
		{
			Role:       "tool",
			ToolCallID: "call_abc",
			Content:    BlocksContent(blocks),
		},
	}

	items := messagesToResponsesInput(msgs)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	item, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("item is not map[string]any: %#v", items[0])
	}
	if item["type"] != "function_call_output" || item["call_id"] != "call_abc" {
		t.Errorf("unexpected item fields: %+v", item)
	}

	output, ok := item["output"].([]map[string]any)
	if !ok {
		t.Fatalf("output is not []map[string]any: %#v", item["output"])
	}
	if len(output) != 2 {
		t.Fatalf("expected 2 output blocks, got %d", len(output))
	}
	if output[0]["type"] != "input_text" || output[0]["text"] != "image text description" {
		t.Errorf("unexpected first output block: %+v", output[0])
	}
	if output[1]["type"] != "input_image" || output[1]["image_url"] != "data:image/png;base64,iVBOR" {
		t.Errorf("unexpected second output block: %+v", output[1])
	}
}

func TestMessagesToResponsesInputUserWithImage(t *testing.T) {
	blocks := []ContentBlock{
		{Type: "text", Text: "user query text"},
		{Type: "image_url", ImageURL: &ContentImageURL{URL: "data:image/png;base64,iVBOR"}},
	}
	msgs := []Message{
		{
			Role:    "user",
			Content: BlocksContent(blocks),
		},
	}

	items := messagesToResponsesInput(msgs)
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	item, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("item is not map[string]any: %#v", items[0])
	}
	if item["role"] != "user" {
		t.Errorf("unexpected item role: %+v", item["role"])
	}

	content, ok := item["content"].([]map[string]any)
	if !ok {
		t.Fatalf("content is not []map[string]any: %#v", item["content"])
	}
	if len(content) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(content))
	}
	if content[0]["type"] != "input_text" || content[0]["text"] != "user query text" {
		t.Errorf("unexpected first content block: %+v", content[0])
	}
	if content[1]["type"] != "input_image" || content[1]["image_url"] != "data:image/png;base64,iVBOR" || content[1]["detail"] != "auto" {
		t.Errorf("unexpected second content block: %+v", content[1])
	}
}

