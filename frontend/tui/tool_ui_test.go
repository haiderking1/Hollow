package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestRenderWriteLine(t *testing.T) {
	styles := NewStyles()
	line := renderWriteLine(styles, toolRow{Kind: toolKindWrite, Target: "main.py", Added: 151, Removed: 0})
	plain := ansi.Strip(line)

	for _, want := range []string{"Write main.py", "+151", "-0"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("missing %q in %q", want, plain)
		}
	}
}

func TestRenderEditLine(t *testing.T) {
	styles := NewStyles()
	line := renderEditLine(styles, toolRow{Kind: toolKindEdit, Target: "globals.css", Added: 3, Removed: 0})
	plain := ansi.Strip(line)

	for _, want := range []string{"Edited globals.css", "+3", "-0", ">"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("missing %q in %q", want, plain)
		}
	}
}

func TestRenderReadBlock(t *testing.T) {
	styles := NewStyles()
	lines := renderReadBlock(styles, toolRow{
		Kind:   toolKindRead,
		Target: "~/proj/Toolbar.tsx",
		Lines:  229,
	})
	plain := ansi.Strip(strings.Join(lines, "\n"))

	if strings.Contains(plain, "+229") {
		t.Fatalf("read should not show +line count: %q", plain)
	}
	if !strings.Contains(plain, "Read ~/proj/Toolbar.tsx") {
		t.Fatalf("missing header: %q", plain)
	}
	if !strings.Contains(plain, "Read 229 lines") {
		t.Fatalf("missing summary: %q", plain)
	}
}

func TestRenderBashBlock(t *testing.T) {
	styles := NewStyles()
	lines := renderBashBlock(styles, toolRow{
		Kind:   toolKindBash,
		Target: "git push -u origin main",
		Output: "branch 'main' set up to track 'origin/main'.\nTo https://github.com/example/repo.git",
	}, 80, false)
	plain := ansi.Strip(strings.Join(lines, "\n"))

	if !strings.Contains(plain, "Bash git push") {
		t.Fatalf("missing header: %q", plain)
	}
	if !strings.HasPrefix(strings.Split(plain, "\n")[1], "└ branch") {
		t.Fatalf("first output line should use └: %q", plain)
	}
}

func TestParseToolRowEditFile(t *testing.T) {
	row := parseToolRow(chatMsg{
		toolName:    "edit_file",
		toolArgs:    `{"path":"src/foo.go","old_string":"a","new_string":"b"}`,
		toolAdded:   1,
		toolRemoved: 1,
	})
	if row.Action != "Edited" || row.Kind != toolKindEdit || row.Target != "src/foo.go" {
		t.Fatalf("got %+v", row)
	}
}

func TestParseToolRowWriteLines(t *testing.T) {
	row := parseToolRow(chatMsg{
		toolName:    "write_file",
		toolArgs:    `{"path":"src/foo.go","content":"line1\nline2\nline3"}`,
		toolAdded:   3,
		toolRemoved: 0,
	})
	if row.Action != "Write" || row.Added != 3 || row.Removed != 0 || row.Target != "src/foo.go" {
		t.Fatalf("got %+v", row)
	}
}

func TestLineDiff(t *testing.T) {
	added, removed := lineDiff("a\nb\nc", "a\nx\nc\nd")
	if added != 2 || removed != 1 {
		t.Fatalf("got +%d -%d", added, removed)
	}
}

func TestFormatToolCall(t *testing.T) {
	got := formatToolCall("write_file", `{"path":"main.py","content":"x"}`)
	if got != "Write main.py" {
		t.Fatalf("got %q", got)
	}
}

func TestSingleToolNoHeader(t *testing.T) {
	styles := NewStyles()
	out := renderToolGroup(styles, []chatMsg{{
		toolName: "write_file",
		toolArgs: `{"path":"main.py","content":"a"}`,
	}}, 80, false, 0)
	if strings.Contains(out, "Updated") {
		t.Fatalf("single tool should not show group header: %q", out)
	}
}

func TestRenderBrowserListUsesDetails(t *testing.T) {
	styles := NewStyles()
	row := toolRow{
		Name:     "browser",
		Action:   "list",
		Output:   "TAB1 [page] Example - https://example.com",
		Metadata: `{"action":"list","tabs":[{"id":"TAB1","title":"Example","url":"https://example.com","type":"page"}]}`,
	}
	lines := renderBrowserBlock(styles, row, 80, false)
	plain := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(plain, "1 tab(s)") {
		t.Errorf("expected output to contain '1 tab(s)', got:\n%s", plain)
	}
}

