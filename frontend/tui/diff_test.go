package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/enough/enough/backend/core"
)

func TestFinalizeFileToolDiffSingleLineEdit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SOUL.md")
	before := "# SOUL.md — Smoke's identity\n\nYou are Smoke, a coding agent.\n"
	if err := os.WriteFile(path, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Chdir(dir)

	after := "# SOUL.md — Shadow's identity\n\nYou are Shadow, a coding agent.\n"
	if err := os.WriteFile(path, []byte(after), 0o644); err != nil {
		t.Fatal(err)
	}

	args := `{"path":"SOUL.md","old_string":"ignored","new_string":""}`
	added, removed := finalizeFileToolDiff("edit_file", args, before, false)
	if added != 2 || removed != 2 {
		t.Fatalf("got +%d -%d, want +2 -2", added, removed)
	}
}

func TestFinalizeFileToolDiffIgnoresStaleOldString(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SOUL.md")
	before := "# title\nline1\nline2\n"
	if err := os.WriteFile(path, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	// Model passes the pre-edit file body as old_string with empty new_string — the
	// old preview logic showed +0-13 for this even when the on-disk edit only
	// changed one line.
	if err := os.WriteFile(path, []byte("# title\nline1\nline2 changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	args := `{"path":"SOUL.md","old_string":"stale","new_string":""}`
	added, removed := finalizeFileToolDiff("edit_file", args, before, false)
	if added != 1 || removed != 1 {
		t.Fatalf("got +%d -%d, want +1 -1", added, removed)
	}
}

func TestFinalizeFileToolDiffSequentialEdits(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SOUL.md")
	original := "# SOUL.md — Smoke's identity\n\nYou are Smoke, a coding agent.\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	// First edit: title only.
	mid := "# SOUL.md — Shadow's identity\n\nYou are Smoke, a coding agent.\n"
	if err := os.WriteFile(path, []byte(mid), 0o644); err != nil {
		t.Fatal(err)
	}
	args1 := `{"path":"SOUL.md","old_string":"Smoke's identity","new_string":"Shadow's identity"}`
	added, removed := finalizeFileToolDiff("edit_file", args1, original, false)
	if added != 1 || removed != 1 {
		t.Fatalf("first edit: got +%d -%d, want +1 -1", added, removed)
	}

	// Second edit: persona line, snapshot taken after first edit already applied.
	final := "# SOUL.md — Shadow's identity\n\nYou are Shadow, a coding agent.\n"
	if err := os.WriteFile(path, []byte(final), 0o644); err != nil {
		t.Fatal(err)
	}
	args2 := `{"path":"SOUL.md","old_string":"stale","new_string":""}`
	added, removed = finalizeFileToolDiff("edit_file", args2, mid, false)
	if added != 1 || removed != 1 {
		t.Fatalf("second edit: got +%d -%d, want +1 -1", added, removed)
	}
}

func TestFinalizeFileToolDiffWriteNewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.txt")
	content := "a\nb\nc\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	args := `{"path":"new.txt","content":"a\nb\nc\n"}`
	added, removed := finalizeFileToolDiff("write_file", args, "", false)
	if added != 3 || removed != 0 {
		t.Fatalf("got +%d -%d, want +3 -0", added, removed)
	}
}

func TestFinalizeFileToolDiffError(t *testing.T) {
	added, removed := finalizeFileToolDiff("edit_file", `{"path":"missing.txt"}`, "before", true)
	if added != 0 || removed != 0 {
		t.Fatalf("got +%d -%d, want +0 -0 on error", added, removed)
	}
}

func TestResolveToolFilePathExpandsHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	got := resolveToolFilePath("~/.enough/SOUL.md")
	want := filepath.Clean(filepath.Join(home, ".enough", "SOUL.md"))
	if got != want {
		t.Fatalf("resolveToolFilePath(~/.enough/SOUL.md) = %q, want %q", got, want)
	}
}

func TestHandleToolEditDiffFromSnapshot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	before := "alpha\nbeta\n"
	if err := os.WriteFile(path, []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	a := &App{styles: NewStyles()}
	args := `{"path":"file.txt","old_string":"beta","new_string":"gamma"}`
	a.handleToolStart(core.ToolCallEvent{ID: "t1", Name: "edit_file", Args: args})

	last := a.messages[len(a.messages)-1]
	if !last.toolDiffSnapshotted || last.toolBeforeContent != before {
		t.Fatalf("snapshot missing: snapshotted=%v content=%q", last.toolDiffSnapshotted, last.toolBeforeContent)
	}
	if last.toolAdded != 0 || last.toolRemoved != 0 {
		t.Fatalf("diff should not be set at start, got +%d -%d", last.toolAdded, last.toolRemoved)
	}

	if err := os.WriteFile(path, []byte("alpha\ngamma\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.handleToolResult(core.ToolCallEvent{ID: "t1", Result: "edited file.txt (1 replacement(s))"})

	last = a.messages[len(a.messages)-1]
	if last.toolAdded != 1 || last.toolRemoved != 1 {
		t.Fatalf("after result got +%d -%d, want +1 -1", last.toolAdded, last.toolRemoved)
	}
}
