package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/enough/enough/backend/config"
	"github.com/enough/enough/backend/core"
	"github.com/enough/enough/backend/opencode"
)

func TestPromptWithUserAttachments(t *testing.T) {
	a := &Agent{
		cfg: config.Runtime{
			Model:    "minimax-m3",
			Evidence: config.DefaultEvidence(),
		},
		workDir: t.TempDir(),
	}

	srv := scriptedServer(t, func(req opencode.ChatRequest) (string, []toolCallJSON) {
		return "understood", nil
	})
	defer srv.Close()

	cfg := a.cfg
	cfg.Endpoint = srv.URL
	cfg.APIKey = "k"

	attachments := []UserAttachment{
		{
			MIMEType: "image/png",
			Data:     []byte("fake-resized-png-bytes"),
		},
	}

	err := a.Prompt(context.Background(), cfg, "What's in this image?", attachments, func(core.Event) {})
	if err != nil {
		t.Fatalf("Prompt failed: %v", err)
	}

	var foundUserMsg *opencode.Message
	for i := range a.messages {
		if a.messages[i].Role == "user" {
			foundUserMsg = &a.messages[i]
			break
		}
	}

	if foundUserMsg == nil {
		t.Fatal("user message not found in agent history")
	}

	blocks := opencode.ContentBlocks(*foundUserMsg)
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks in user message, got %d", len(blocks))
	}

	if blocks[0].Type != "text" || blocks[0].Text != "What's in this image?" {
		t.Errorf("unexpected first block: %+v", blocks[0])
	}

	if blocks[1].Type != "image_url" || blocks[1].ImageURL == nil || !strings.HasPrefix(blocks[1].ImageURL.URL, "data:image/png;base64,") {
		t.Errorf("unexpected second block: %+v", blocks[1])
	}
}

func TestPromptOmitsAttachmentsForNonVisionModel(t *testing.T) {
	a := &Agent{
		cfg: config.Runtime{
			Model:    "deepseek-v4-flash",
			Evidence: config.DefaultEvidence(),
		},
		workDir: t.TempDir(),
	}

	srv := scriptedServer(t, func(req opencode.ChatRequest) (string, []toolCallJSON) {
		for _, msg := range req.Messages {
			for _, block := range opencode.ContentBlocks(msg) {
				if block.Type == "image_url" {
					t.Fatal("non-vision model request must not include image blocks")
				}
			}
		}
		return "understood", nil
	})
	defer srv.Close()

	cfg := a.cfg
	cfg.Endpoint = srv.URL
	cfg.APIKey = "k"

	err := a.Prompt(context.Background(), cfg, "What's in this image?", []UserAttachment{{
		MIMEType: "image/png",
		Data:     []byte("fake-resized-png-bytes"),
	}}, func(core.Event) {})
	if err != nil {
		t.Fatalf("Prompt failed: %v", err)
	}

	var userMsg *opencode.Message
	for i := range a.messages {
		if a.messages[i].Role == "user" && strings.Contains(opencode.ContentString(a.messages[i]), "What's in this image?") {
			userMsg = &a.messages[i]
			break
		}
	}
	if userMsg == nil {
		t.Fatal("user message not found")
	}

	blocks := opencode.ContentBlocks(*userMsg)
	if len(blocks) != 1 || !strings.Contains(blocks[0].Text, "omitted") {
		t.Fatalf("expected omission note only, got %+v", blocks)
	}
}
