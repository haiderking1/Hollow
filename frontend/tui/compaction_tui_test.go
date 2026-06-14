package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/enough/enough/backend/agent"
	"github.com/enough/enough/backend/config"
	"github.com/enough/enough/backend/core"
	"github.com/enough/enough/backend/secrets"
)

func TestCompactionTUIEventsAndQueue(t *testing.T) {
	tempHome, err := os.MkdirTemp("", "enough-home-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempHome)

	oldHome := os.Getenv("HOME")
	os.Setenv("HOME", tempHome)
	defer os.Setenv("HOME", oldHome)

	credPath := filepath.Join(tempHome, ".config", "enough", "credentials")
	os.Setenv("ENOUGH_CREDENTIALS_FILE", credPath)
	defer os.Unsetenv("ENOUGH_CREDENTIALS_FILE")

	err = secrets.SetAPIKey("test-key")
	if err != nil {
		t.Fatal(err)
	}

	app := &App{
		styles: NewStyles(),
		editor: NewEditor(512),
	}

	// 1. Test compaction start sets compacting and logs
	app.handleAgentEvent(core.Event{
		Kind: core.EventCompactionStart,
		Data: core.CompactionStartEvent{Reason: "threshold"},
	})
	if !app.compacting {
		t.Fatal("expected app.compacting to be true after EventCompactionStart")
	}
	if app.compactionLabel != "Auto-compacting..." {
		t.Fatalf("expected compaction label, got %q", app.compactionLabel)
	}
	loader := app.renderCompactionLoader()
	if !strings.Contains(loader, "Auto-compacting") || !strings.Contains(loader, "escape to cancel") {
		t.Fatalf("expected compaction loader, got %q", loader)
	}

	// 2. Test submitting messages during compaction queues them
	app.editor.SetValue("hello while compacting")
	app.handleSubmit()
	if len(app.compactionQueuedMessages) != 1 || app.compactionQueuedMessages[0].text != "hello while compacting" {
		t.Fatalf("expected queued message, got %v", app.compactionQueuedMessages)
	}

	// 3. Test compaction end resets compacting (queue already verified above;
	// clear it so we don't spawn a background agent mid-test).
	app.compactionQueuedMessages = nil
	app.handleAgentEvent(core.Event{
		Kind: core.EventCompactionEnd,
		Data: core.CompactionEndEvent{
			Reason:    "threshold",
			WillRetry: false,
		},
	})
	if app.compacting {
		t.Fatal("expected app.compacting to be false after EventCompactionEnd")
	}

	// 4. Queued messages drain one at a time when idle.
	app.compactionQueuedMessages = []queuedMessage{
		{text: "first"},
		{text: "second", attachments: []agent.UserAttachment{{MIMEType: "image/png", Data: []byte("x")}}},
	}
	app.tryDrainCompactionQueue()
	if !app.running {
		t.Fatal("expected running after draining first queued message")
	}
	if len(app.compactionQueuedMessages) != 1 {
		t.Fatalf("expected one queued message left, got %d", len(app.compactionQueuedMessages))
	}
	if app.compactionQueuedMessages[0].text != "second" {
		t.Fatalf("expected second message to remain queued, got %+v", app.compactionQueuedMessages[0])
	}
	if len(app.compactionQueuedMessages[0].attachments) != 1 {
		t.Fatalf("expected attachments preserved on second queued message, got %+v", app.compactionQueuedMessages[0].attachments)
	}

	app.running = true
	app.tryDrainCompactionQueue()
	if len(app.compactionQueuedMessages) != 1 {
		t.Fatal("expected drain to no-op while agent is running")
	}
	app.running = false
	app.tryDrainCompactionQueue()
	if len(app.compactionQueuedMessages) != 0 {
		t.Fatalf("expected queue empty after second drain, got %d", len(app.compactionQueuedMessages))
	}

	// 5. Test escape cancellation routing
	cfg := config.Runtime{}
	app.mu.Lock()
	app.agent = agent.New(cfg, "", nil)
	app.mu.Unlock()

	// Trigger compaction again
	app.setCompacting(true, "Compacting context...")
	app.handleInterrupt() // should call agent.AbortCompaction() without panic
}
