package obligations

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestMutationCreatesVerifyAndSignoffObligations(t *testing.T) {
	r := NewRegistry("t1", "go test ./...", nil, true, true)
	if r.HasOpen() {
		t.Fatal("fresh registry has open obligations")
	}

	if !r.NoteMutation() {
		t.Fatal("first mutation did not change obligation set")
	}
	open := r.Open()
	if len(open) != 2 {
		t.Fatalf("open = %d, want 2 (verify + signoff)", len(open))
	}
	if open[0].Kind != KindMustRunVerify || open[1].Kind != KindVerifierSignedOff {
		t.Fatalf("unexpected kinds: %s, %s", open[0].Kind, open[1].Kind)
	}

	// Repeat mutation while already open: no duplicates.
	r.NoteMutation()
	if len(r.Snapshot()) != 2 {
		t.Fatalf("duplicate obligations created: %d", len(r.Snapshot()))
	}
}

func TestVerifierDisabledSkipsSignoff(t *testing.T) {
	r := NewRegistry("t1", "go test ./...", nil, true, false)
	r.NoteMutation()
	for _, ob := range r.Snapshot() {
		if ob.Kind == KindVerifierSignedOff {
			t.Fatal("signoff obligation created with verifier disabled")
		}
	}
}

func TestCommandRunClosesVerify(t *testing.T) {
	r := NewRegistry("t1", "go test ./...", nil, true, false)
	r.NoteMutation()

	if r.NoteCommandRun("go build ./...", 0, "ev_1", false) {
		t.Fatal("non-matching command closed verify")
	}
	if r.NoteCommandRun("go test ./...", 1, "ev_2", false) {
		t.Fatal("failing run closed verify")
	}
	if !r.NoteCommandRun("go test ./... -count=1", 0, "ev_3", false) {
		t.Fatal("matching exit-0 run did not close verify")
	}
	if !r.VerifyClosed() {
		t.Fatal("verify not closed")
	}
	if r.Open() != nil {
		t.Fatalf("still open: %+v", r.Open())
	}
	if r.Snapshot()[0].ClosedBy != "ev_3" {
		t.Fatal("ClosedBy not recorded")
	}
}

func TestStrictResetReopensVerifyAfterMutation(t *testing.T) {
	r := NewRegistry("t1", "go test ./...", nil, true, true)
	r.NoteMutation()
	r.NoteCommandRun("go test ./...", 0, "ev_1", false)
	if !r.VerifyClosed() {
		t.Fatal("setup: verify not closed")
	}

	if !r.NoteMutation() {
		t.Fatal("mutation after pass did not reopen verify")
	}
	if r.VerifyClosed() {
		t.Fatal("verify still closed after mutation (strict mode)")
	}
}

func TestNonStrictKeepsVerifyClosed(t *testing.T) {
	r := NewRegistry("t1", "go test ./...", nil, false, false)
	r.NoteMutation()
	r.NoteCommandRun("go test ./...", 0, "ev_1", false)
	r.NoteMutation()
	if !r.VerifyClosed() {
		t.Fatal("non-strict mode reopened verify")
	}
}

func TestManualVerifyClosesOnAnyExitZero(t *testing.T) {
	r := NewRegistry("t1", "", nil, true, false)
	r.NoteMutation()
	if !r.NoteCommandRun("./run_checks.sh", 0, "ev_1", false) {
		t.Fatal("manual verify did not close on explicit exit-0 command")
	}
}

// A real passing verify run closes sign-off too: machine evidence beats a
// second verifier hoop. NoteVerifierPass remains for the backstop path.
func TestVerifyRunAutoClosesSignoff(t *testing.T) {
	r := NewRegistry("t1", "go test ./...", nil, true, true)
	r.NoteMutation()
	if !r.NoteCommandRun("go test ./...", 0, "ev_1", false) {
		t.Fatal("verify run did not close")
	}
	if r.HasOpen() {
		t.Fatalf("signoff not auto-closed by verify evidence: %+v", r.Open())
	}
	if r.NoteVerifierPass("ev_2") {
		t.Fatal("NoteVerifierPass changed state after auto-close")
	}
}

func TestVerifierPassClosesSignoffBackstop(t *testing.T) {
	r := NewRegistry("t1", "go test ./...", nil, true, true)
	r.NoteMutation()
	if !r.NoteVerifierPass("ev_2") {
		t.Fatal("verifier pass did not close signoff")
	}
	if r.VerifyClosed() {
		t.Fatal("verifier pass must not close must_run_verify")
	}
}

// Running a file mutated this turn is task-scoped verification — it closes
// verify even when a repo-wide verify command exists.
func TestMutatedScriptRunClosesVerify(t *testing.T) {
	r := NewRegistry("t1", "pytest", nil, true, true)
	r.NoteMutation()
	if r.NoteCommandRun("python3 hello.py", 0, "ev_1", false) {
		t.Fatal("unrelated command closed verify")
	}
	if !r.NoteCommandRun("python3 hello.py", 0, "ev_2", true) {
		t.Fatal("running the mutated script did not close verify")
	}
	if r.HasOpen() {
		t.Fatalf("still open: %+v", r.Open())
	}
}

// pytest exit 5 means "no tests collected" — not a failure of the change.
func TestPytestNoTestsCollectedClosesVerify(t *testing.T) {
	r := NewRegistry("t1", "pytest", nil, true, false)
	r.NoteMutation()
	if r.NoteCommandRun("pytest", 1, "ev_1", false) {
		t.Fatal("pytest exit 1 closed verify")
	}
	if !r.NoteCommandRun("pytest", 5, "ev_2", false) {
		t.Fatal("pytest exit 5 (no tests) did not close verify")
	}
}

func TestDetectVerifyCommand(t *testing.T) {
	write := func(t *testing.T, dir, name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cases := map[string]struct {
		setup func(t *testing.T, dir string)
		want  string
	}{
		"go":             {func(t *testing.T, d string) { write(t, d, "go.mod", "module x\n") }, "go test ./..."},
		"node with test": {func(t *testing.T, d string) { write(t, d, "package.json", `{"scripts":{"test":"jest"}}`) }, "npm test"},
		"node no test":   {func(t *testing.T, d string) { write(t, d, "package.json", `{"scripts":{}}`) }, ""},
		"rust":           {func(t *testing.T, d string) { write(t, d, "Cargo.toml", "[package]\n") }, "cargo test"},
		"python":         {func(t *testing.T, d string) { write(t, d, "pyproject.toml", "") }, "pytest"},
		"none":           {func(t *testing.T, d string) {}, ""},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			tc.setup(t, dir)
			if got := DetectVerifyCommand(dir); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestObligationJSONRoundtrip(t *testing.T) {
	r := NewRegistry("t1", "go test ./...", nil, true, true)
	r.NoteMutation()
	b, err := json.Marshal(r.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	var out []Obligation
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("roundtrip lost obligations: %d", len(out))
	}
}
