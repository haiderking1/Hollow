package agent

import (
	"path/filepath"
	"testing"

	"github.com/enough/enough/backend/config"
	"github.com/enough/enough/backend/session"
)

func TestLoadSessionUpdatesWorkDir(t *testing.T) {
	root := t.TempDir()
	projA := filepath.Join(root, "a")
	projB := filepath.Join(root, "b")
	t.Setenv("HOME", root)

	smA, err := session.StartNew(projA)
	if err != nil {
		t.Fatal(err)
	}
	smB, err := session.StartNew(projB)
	if err != nil {
		t.Fatal(err)
	}

	cfg := config.Runtime{}
	a := New(cfg, smA.CWD(), smA)
	if a.WorkDir() != projA {
		t.Fatalf("initial workDir = %q, want %q", a.WorkDir(), projA)
	}

	a.LoadSession(smB)
	if a.WorkDir() != projB {
		t.Fatalf("workDir after switch = %q, want %q", a.WorkDir(), projB)
	}
}
