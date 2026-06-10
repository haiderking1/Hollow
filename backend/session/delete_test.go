package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDeleteUnlinkFallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"session","id":"abc"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := Delete(path)
	if err != nil {
		t.Fatal(err)
	}
	if result.Method != "unlink" && result.Method != "trash" {
		t.Fatalf("unexpected method %q", result.Method)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file still exists: %v", err)
	}
}
