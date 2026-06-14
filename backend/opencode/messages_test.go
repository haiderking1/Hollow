package opencode

import (
	"encoding/json"
	"testing"
)

func TestRepairToolMessagesAddsMissingToolReplies(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: StringContent("hi")},
		{Role: "assistant", ToolCalls: []ToolCall{
			{ID: "call_a", Type: "function", Function: ToolCallFunction{Name: "read_file", Arguments: `{}`}},
			{ID: "call_b", Type: "function", Function: ToolCallFunction{Name: "bash", Arguments: `{}`}},
		}},
		{Role: "tool", ToolCallID: "call_a", Name: "read_file", Content: StringContent("ok")},
	}

	fixed := RepairToolMessages(msgs)
	if len(fixed) != 4 {
		t.Fatalf("got %d messages, want 4", len(fixed))
	}
	last := fixed[len(fixed)-1]
	if last.Role != "tool" || last.ToolCallID != "call_b" {
		t.Fatalf("expected stub for call_b, got %+v", last)
	}
	if ContentString(last) != toolIncompleteMsg {
		t.Fatalf("stub content = %q", ContentString(last))
	}
}

func TestRepairToolMessagesAssignsMissingToolCallIDs(t *testing.T) {
	msgs := []Message{
		{Role: "assistant", ToolCalls: []ToolCall{
			{Type: "function", Function: ToolCallFunction{Name: "bash", Arguments: `{}`}},
		}},
	}

	fixed := RepairToolMessages(msgs)
	if len(fixed) != 2 {
		t.Fatalf("got %d messages, want 2", len(fixed))
	}
	if fixed[0].ToolCalls[0].ID == "" {
		t.Fatal("expected synthetic tool call id")
	}
	if fixed[1].ToolCallID != fixed[0].ToolCalls[0].ID {
		t.Fatalf("tool reply id mismatch: %q vs %q", fixed[1].ToolCallID, fixed[0].ToolCalls[0].ID)
	}
}

func TestStripResponseFieldsRemovesUsage(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: StringContent("hi")},
		{Role: "assistant", Content: StringContent("hello"), Usage: &Usage{Input: 10, Output: 5}},
	}
	out := StripResponseFields(msgs)
	if out[1].Usage != nil {
		t.Fatal("expected usage stripped from assistant message")
	}
	if msgs[1].Usage == nil {
		t.Fatal("StripResponseFields should not mutate input slice")
	}
}

func TestPrepareRequestMessagesStripsUsage(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: StringContent("hi")},
		{Role: "assistant", Content: StringContent("hello"), Usage: &Usage{Input: 10, Output: 5}},
	}
	out := PrepareRequestMessages(msgs, "deepseek-v4-flash")
	if out[1].Usage != nil {
		t.Fatal("expected usage stripped")
	}
}

func TestMessageMarshalUnmarshalBlocksRoundTrip(t *testing.T) {
	blocks := []ContentBlock{
		{Type: "text", Text: "Read image file [image/png]\n800x600"},
		{Type: "image_url", ImageURL: &ContentImageURL{URL: "data:image/png;base64,iVBOR"}},
	}
	msg := Message{
		Role:    "tool",
		Content: BlocksContent(blocks),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}

	var roundTripped Message
	if err := json.Unmarshal(data, &roundTripped); err != nil {
		t.Fatal(err)
	}

	parsedBlocks := ContentBlocks(roundTripped)
	if len(parsedBlocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(parsedBlocks))
	}
	if parsedBlocks[0].Type != "text" || parsedBlocks[0].Text != "Read image file [image/png]\n800x600" {
		t.Errorf("unexpected first block: %+v", parsedBlocks[0])
	}
	if parsedBlocks[1].Type != "image_url" || parsedBlocks[1].ImageURL == nil || parsedBlocks[1].ImageURL.URL != "data:image/png;base64,iVBOR" {
		t.Errorf("unexpected second block: %+v", parsedBlocks[1])
	}
}

func TestPrepareRequestMessagesStripsImagesForNonVisionModels(t *testing.T) {
	blocks := []ContentBlock{
		{Type: "text", Text: "text content"},
		{Type: "image_url", ImageURL: &ContentImageURL{URL: "data:image/png;base64,iVBOR"}},
	}
	msgs := []Message{
		{
			Role:    "tool",
			Content: BlocksContent(blocks),
		},
	}

	// 1. With vision model, images must stay
	withVision := PrepareRequestMessages(msgs, "gpt-5")
	if len(withVision) != 1 {
		t.Fatalf("expected 1 message, got %d", len(withVision))
	}
	parsedVisionBlocks := ContentBlocks(withVision[0])
	if len(parsedVisionBlocks) != 2 {
		t.Errorf("expected 2 blocks for vision model, got %d", len(parsedVisionBlocks))
	}

	// 2. With non-vision model, images must be stripped
	noVision := PrepareRequestMessages(msgs, "deepseek-v4-flash")
	if len(noVision) != 1 {
		t.Fatalf("expected 1 message, got %d", len(noVision))
	}
	parsedNoVisionBlocks := ContentBlocks(noVision[0])
	if len(parsedNoVisionBlocks) != 1 {
		t.Errorf("expected 1 block for non-vision model, got %d", len(parsedNoVisionBlocks))
	}
	expectedText := "text content\n[1 image(s) omitted — current model does not support images.]"
	if parsedNoVisionBlocks[0].Type != "text" || parsedNoVisionBlocks[0].Text != expectedText {
		t.Errorf("expected text block with omission notice, got %+v", parsedNoVisionBlocks[0])
	}
}
