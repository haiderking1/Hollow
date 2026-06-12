package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/enough/enough/backend/config"
)

func TestDetectIsCoding(t *testing.T) {
	tempDir := t.TempDir()

	cfg := config.Runtime{
		Agent: config.AgentSettings{
			CodingContext: "auto",
		},
	}

	// 1. Initially it should be false (no git, no project markers)
	t.Setenv("ENOUGH_PLATFORM", "cli")
	if DetectIsCoding(tempDir, cfg) {
		t.Fatal("expected coding context to be false for empty directory under auto")
	}

	// 2. Setting it to "on" should force it to true
	cfgOn := cfg
	cfgOn.Agent.CodingContext = "on"
	if !DetectIsCoding(tempDir, cfgOn) {
		t.Fatal("expected coding context to be true when explicitly 'on'")
	}

	// 3. Creating a git repo should make it true under auto
	if err := os.MkdirAll(filepath.Join(tempDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !DetectIsCoding(tempDir, cfg) {
		t.Fatal("expected coding context to be true when a .git folder exists")
	}

	// 4. Dotfiles repo at $HOME should not trigger coding context
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	if err := os.MkdirAll(filepath.Join(homeDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Under homeDir directly, it shouldn't be coding context because it is $HOME
	if DetectIsCoding(homeDir, cfg) {
		t.Fatal("expected dotfiles repo at HOME to not trigger coding context")
	}

	// 5. Project markers should trigger coding context
	projDir := filepath.Join(homeDir, "myproject")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projDir, "go.mod"), []byte("module foo"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !DetectIsCoding(projDir, cfg) {
		t.Fatal("expected project marker go.mod to trigger coding context")
	}
}
