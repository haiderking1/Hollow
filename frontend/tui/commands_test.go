package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/enough/enough/backend/secrets"
	"github.com/enough/enough/backend/session"
)

func TestStartCompactShowsLoaderImmediately(t *testing.T) {
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

	if err := secrets.SetAPIKey("test-key"); err != nil {
		t.Fatal(err)
	}

	sm, err := session.ContinueRecent(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	app := &App{
		styles:  NewStyles(),
		editor:  NewTaskEditor(),
		session: sm,
	}

	app.startCompact("")
	if !app.compacting {
		t.Fatal("expected compacting=true immediately after /compact")
	}
	loader := app.renderCompactionLoader()
	if !strings.Contains(loader, "Compacting context") {
		t.Fatalf("expected loader visible before async work, got %q", loader)
	}
	if len(app.messages) == 0 || app.messages[len(app.messages)-1].text != "/compact" {
		t.Fatalf("expected /compact in chat, got %#v", app.messages)
	}

	// Let the background compact task finish (likely "session too small" on empty session).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		app.mu.Lock()
		ch := app.agentCh
		app.mu.Unlock()
		if ch == nil {
			break
		}
		select {
		case e, ok := <-ch:
			if !ok {
				app.mu.Lock()
				app.running = false
				app.agentCh = nil
				app.mu.Unlock()
				goto done
			}
			app.handleAgentEvent(e)
		case <-time.After(50 * time.Millisecond):
		}
	}
done:
	if app.compacting {
		t.Fatal("expected compacting=false after compact task finished")
	}
}

func TestStartCompactRequiresConnection(t *testing.T) {
	app := &App{styles: NewStyles(), editor: NewTaskEditor()}
	app.startCompact("")
	if app.compacting {
		t.Fatal("expected compacting=false when not connected")
	}
	if len(app.messages) == 0 || app.messages[0].role != "error" {
		t.Fatal("expected connection error message")
	}
}

func TestStartCompactRequiresSession(t *testing.T) {
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

	if err := secrets.SetAPIKey("test-key"); err != nil {
		t.Fatal(err)
	}

	app := &App{styles: NewStyles(), editor: NewTaskEditor()}
	app.startCompact("")
	if app.compacting {
		t.Fatal("expected compacting=false without session")
	}
}
