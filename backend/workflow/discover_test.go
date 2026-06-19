package workflow

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFindLatestScript(t *testing.T) {
	dir := t.TempDir()
	oldDir := filepath.Join(dir, ".enough", "workflows", "old")
	newDir := filepath.Join(dir, ".enough", "workflows", "new")
	for _, runDir := range []string{oldDir, newDir} {
		if err := os.MkdirAll(runDir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	oldPath := filepath.Join(oldDir, "workflow.js")
	newPath := filepath.Join(newDir, "workflow.js")
	if err := os.WriteFile(oldPath, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(newPath, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := FindLatestScript(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != newPath {
		t.Fatalf("latest = %q want %q", got, newPath)
	}
}

func TestResolveScriptPath(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, ".enough", "workflows", "abc", "workflow.js")
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveScriptPath(dir, ".enough/workflows/abc/workflow.js")
	if err != nil {
		t.Fatal(err)
	}
	if got != script {
		t.Fatalf("resolved = %q want %q", got, script)
	}
}
