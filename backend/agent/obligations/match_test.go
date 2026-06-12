package obligations

import "testing"

func TestExtractTaskVerifyCommands(t *testing.T) {
	prompt := `Fix the API so curl -sf http://localhost:8080/health returns 200.
Then run ` + "`go test ./pkg/...`" + ` to confirm.`
	cmds := ExtractTaskVerifyCommands(prompt)
	if len(cmds) != 2 {
		t.Fatalf("got %d commands, want 2: %v", len(cmds), cmds)
	}
}

func TestCommandMatchesCurlURL(t *testing.T) {
	pattern := "curl http://localhost:8080/health"
	run := "curl -sf http://localhost:8080/health | jq ."
	if !commandMatchesPattern(run, pattern) {
		t.Fatal("expected URL-based curl match")
	}
}

func TestIsVerifyCommand(t *testing.T) {
	if !IsVerifyCommand("go test ./...", "go test ./...", nil) {
		t.Fatal("project verify should match")
	}
	if IsVerifyCommand("ls -la", "", nil) {
		t.Fatal("ls should not count as verify")
	}
}

func TestTaskVerifyClosesObligation(t *testing.T) {
	r := NewRegistry("t1", "", []string{"curl http://localhost:8080/health"}, true, false)
	r.NoteMutation()
	if !r.NoteCommandRun("curl -sf http://localhost:8080/health", 0, "ev_1", false) {
		t.Fatal("task curl with URL match should close verify")
	}
	if !r.VerifyClosed() {
		t.Fatal("task curl should close verify obligation")
	}
}
