package workflow

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectSavedWorkflowWinsCollision(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	t.Setenv("ENOUGH_HOME", home)
	personal := filepath.Join(home, "workflows", "saved", "audit")
	project := filepath.Join(work, ".enough", "workflows", "saved", "audit")
	for _, dir := range []string{personal, project} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "workflow.js"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	items := ScanSaved(work)
	if len(items) != 1 || !items[0].Project || items[0].Path != filepath.Join(project, "workflow.js") {
		t.Fatalf("items = %#v", items)
	}
}
