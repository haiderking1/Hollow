package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/enough/enough/backend/opencode"
)

func TestTreeTraversalAndBranching(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "enough-test-tree-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	cwd := filepath.Join(tmpDir, "project")
	_ = os.MkdirAll(cwd, 0700)

	// Create manager
	m, err := ContinueRecent(cwd)
	if err != nil {
		t.Fatal(err)
	}

	// Append messages
	err = m.AppendMessage(opencode.Message{Role: "user", Content: opencode.StringContent("Hello 1")})
	if err != nil {
		t.Fatal(err)
	}
	id1 := *m.LeafID()

	err = m.AppendMessage(opencode.Message{Role: "assistant", Content: opencode.StringContent("Hi 1")})
	if err != nil {
		t.Fatal(err)
	}
	id2 := *m.LeafID()

	// Branch from id1
	m.Branch(id1)
	err = m.AppendMessage(opencode.Message{Role: "assistant", Content: opencode.StringContent("Hi alternative")})
	if err != nil {
		t.Fatal(err)
	}
	idAlt := *m.LeafID()

	// Check branch nodes
	branch := m.GetBranch(&idAlt)
	if len(branch) != 2 {
		t.Fatalf("expected 2 entries on alternative branch, got %d", len(branch))
	}
	if branch[0].ID != id1 || branch[1].ID != idAlt {
		t.Fatal("unexpected branch entries order/ID")
	}

	// Check tree structures
	roots := m.GetTree()
	if len(roots) != 1 {
		t.Fatalf("expected 1 root, got %d", len(roots))
	}
	if len(roots[0].Children) != 2 {
		t.Fatalf("expected 2 children for root, got %d", len(roots[0].Children))
	}

	// Branch with summary
	summaryDetails := BranchSummaryDetails{
		ReadFiles:     []string{"main.go"},
		ModifiedFiles: []string{"main.go"},
	}
	sumID, err := m.BranchWithSummary(&id2, "Summary text", summaryDetails, false)
	if err != nil {
		t.Fatal(err)
	}

	branchWithSum := m.GetBranch(&sumID)
	if len(branchWithSum) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(branchWithSum))
	}
	if branchWithSum[2].Type != TypeBranchSummary || branchWithSum[2].Summary != "Summary text" {
		t.Fatalf("expected branch summary entry at the end, got %v", branchWithSum[2])
	}
}

func TestRepeatedCompactionsKeptBoundary(t *testing.T) {
	h := &entryHelper{}
	u1 := h.createMessageEntry(createUserMessage("1"))
	a1 := h.createMessageEntry(createAssistantMessage("a", createMockUsage(100, 50, 0, 0)))
	u2 := h.createMessageEntry(createUserMessage("2"))
	a2 := h.createMessageEntry(createAssistantMessage("b", createMockUsage(200, 100, 0, 0)))
	u3 := h.createMessageEntry(createUserMessage("3"))

	// Create compaction entry with kept boundary at u2
	comp := h.createCompactionEntry("Compaction 1", u2.ID)

	// Message after compaction
	a3 := h.createMessageEntry(createAssistantMessage("c", createMockUsage(300, 150, 0, 0)))

	path := []FileEntry{u1, a1, u2, a2, u3, comp, a3}

	ctxRes := BuildSessionContext(path, &a3.ID)
	if len(ctxRes.Messages) != 5 {
		t.Fatalf("expected 5 messages in reconstructed context, got %d", len(ctxRes.Messages))
	}
	// Messages should be: [CompactionSummary] [u2] [a2] [u3] [a3]
	if ctxRes.Messages[0].Role != "compactionSummary" {
		t.Fatalf("expected compactionSummary, got role %s", ctxRes.Messages[0].Role)
	}
	if opencode.ContentString(ctxRes.Messages[1]) != "2" {
		t.Fatalf("expected message 2, got %s", opencode.ContentString(ctxRes.Messages[1]))
	}
	if opencode.ContentString(ctxRes.Messages[4]) != "c" {
		t.Fatalf("expected message c, got %s", opencode.ContentString(ctxRes.Messages[4]))
	}
}

func TestCollectEntriesForBranchSummary(t *testing.T) {
	h := &entryHelper{}
	u1 := h.createMessageEntry(createUserMessage("1"))
	a1 := h.createMessageEntry(createAssistantMessage("a", createMockUsage(100, 50, 0, 0)))
	u2 := h.createMessageEntry(createUserMessage("2"))

	// Fork alternative branch at a1
	h.lastID = &a1.ID
	uAlt := h.createMessageEntry(createUserMessage("alt"))
	aAlt := h.createMessageEntry(createAssistantMessage("altHi", createMockUsage(100, 50, 0, 0)))

	entries := []FileEntry{u1, a1, u2, uAlt, aAlt}

	// We want to navigate from targetID (aAlt.ID) back to current active (u2.ID).
	// So oldLeafID is u2.ID, targetID is aAlt.ID.
	res := CollectEntriesForBranchSummary(entries, &u2.ID, aAlt.ID)
	if res.CommonAncestorID == nil || *res.CommonAncestorID != a1.ID {
		t.Fatalf("expected common ancestor to be %s, got %v", a1.ID, res.CommonAncestorID)
	}

	// Entries to summarize should be entries on the old branch from u2 back to common ancestor (a1).
	// So it should contain only u2.
	if len(res.Entries) != 1 {
		t.Fatalf("expected 1 entry to summarize, got %d", len(res.Entries))
	}
	if res.Entries[0].ID != u2.ID {
		t.Fatalf("expected entry to be %s, got %s", u2.ID, res.Entries[0].ID)
	}
}

func TestGetLatestCompactionEntry(t *testing.T) {
	h := &entryHelper{}
	u1 := h.createMessageEntry(createUserMessage("1"))
	comp := h.createCompactionEntry("Compaction 1", u1.ID)
	u2 := h.createMessageEntry(createUserMessage("2"))

	path := []FileEntry{u1, comp, u2}
	latest := GetLatestCompactionEntry(path)
	if latest == nil || latest.ID != comp.ID {
		t.Fatalf("expected latest compaction entry to be %s, got %v", comp.ID, latest)
	}
}
