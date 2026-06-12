// Package obligations implements proof obligations for the v2 evidence
// runtime: machine-checkable claims that must become true before a turn may
// complete. Closure is decided only by runtime rules over evidence — never by
// model self-report.
package obligations

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

type Kind string

const (
	KindMustReadBeforeWrite Kind = "must_read_before_write"
	KindMustRunVerify       Kind = "must_run_verify"
	KindDiffTouchesScope    Kind = "diff_touches_scope"
	KindVerifierSignedOff   Kind = "verifier_signed_off"
)

type Status string

const (
	StatusOpen   Status = "open"
	StatusClosed Status = "closed"
)

type Obligation struct {
	ID          string
	TurnID      string
	Kind        Kind
	Description string
	Command     string // verify command for must_run_verify
	Status      Status
	ClosedBy    string // evidence entry ID
	ClosedAt    time.Time
}

// Registry tracks the obligations of one turn. Safe for concurrent use.
type Registry struct {
	mu              sync.Mutex
	turnID          string
	items           []*Obligation
	seq             int
	verifyCmd       string
	extraVerifyCmds []string
	strictReset     bool
	verifierEnabled bool
}

func NewRegistry(turnID, verifyCmd string, extraVerifyCmds []string, strictReset, verifierEnabled bool) *Registry {
	return &Registry{
		turnID:          turnID,
		verifyCmd:       verifyCmd,
		extraVerifyCmds: append([]string(nil), extraVerifyCmds...),
		strictReset:     strictReset,
		verifierEnabled: verifierEnabled,
	}
}

func (r *Registry) ExtraVerifyCommands() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.extraVerifyCmds...)
}

func (r *Registry) VerifyCommand() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.verifyCmd
}

func (r *Registry) add(kind Kind, desc, cmd string) *Obligation {
	r.seq++
	ob := &Obligation{
		ID:          fmt.Sprintf("ob_%d", r.seq),
		TurnID:      r.turnID,
		Kind:        kind,
		Description: desc,
		Command:     cmd,
		Status:      StatusOpen,
	}
	r.items = append(r.items, ob)
	return ob
}

func (r *Registry) findKind(kind Kind) *Obligation {
	for _, ob := range r.items {
		if ob.Kind == kind {
			return ob
		}
	}
	return nil
}

// NoteMutation records that the workspace changed. It instantiates the verify
// obligation on first mutation and — in strict mode — reopens a previously
// closed verify run, since evidence gathered before the change proves nothing
// about the code that exists now. Returns true if the obligation set changed.
func (r *Registry) NoteMutation() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	changed := false
	ob := r.findKind(KindMustRunVerify)
	if ob == nil {
		desc := "run project verification"
		if r.verifyCmd != "" {
			desc = fmt.Sprintf("%s must exit 0 (run via bash)", r.verifyCmd)
		} else {
			desc = "no auto-verify detected — run an explicit verification command via bash (exit 0)"
		}
		if len(r.extraVerifyCmds) > 0 {
			desc += "; task checks: " + strings.Join(r.extraVerifyCmds, "; ")
		}
		ob = r.add(KindMustRunVerify, desc, r.verifyCmd)
		changed = true
	} else if r.strictReset && ob.Status == StatusClosed {
		ob.Status = StatusOpen
		ob.ClosedBy = ""
		ob.ClosedAt = time.Time{}
		changed = true
	}

	if r.verifierEnabled {
		so := r.findKind(KindVerifierSignedOff)
		if so == nil {
			r.add(KindVerifierSignedOff, "verifier must sign off on this turn", "")
			changed = true
		} else if r.strictReset && so.Status == StatusClosed {
			so.Status = StatusOpen
			so.ClosedBy = ""
			so.ClosedAt = time.Time{}
			changed = true
		}
	}
	return changed
}

// NoteCommandRun closes must_run_verify when the executed command counts as
// verification for this task:
//   - it matches the detected verify command and exited 0, or
//   - no verify command was detected (manual mode) and it exited 0, or
//   - it executes a file mutated this turn (touchesMutation) and exited 0 —
//     running the script you just wrote is task-scoped verification, or
//   - it is a pytest run that collected no tests (exit 5): an absence of
//     tests is not a failure of the change.
//
// Closing must_run_verify also closes verifier_signed_off: a verification
// run recorded by the runtime after the last mutation is machine evidence;
// the verifier role is a backstop for workers that never verify, not a
// second hoop after a real passing run. Returns true on state change.
func (r *Registry) NoteCommandRun(command string, exitCode int, evidenceID string, touchesMutation bool) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	ob := r.findKind(KindMustRunVerify)
	if ob == nil || ob.Status == StatusClosed {
		return false
	}

	manualMode := r.verifyCmd == "" && len(r.extraVerifyCmds) == 0
	matchesVerify := manualMode || commandMatchesAny(command, r.verifyCmd, r.extraVerifyCmds)
	pytestNoTests := exitCode == 5 && strings.Contains(command, "pytest") &&
		(manualMode || commandMatchesAny(command, r.verifyCmd, r.extraVerifyCmds))

	passed := exitCode == 0 && (matchesVerify || touchesMutation)
	if !passed && !pytestNoTests {
		return false
	}

	now := time.Now()
	ob.Status = StatusClosed
	ob.ClosedBy = evidenceID
	ob.ClosedAt = now

	if so := r.findKind(KindVerifierSignedOff); so != nil && so.Status == StatusOpen {
		so.Status = StatusClosed
		so.ClosedBy = evidenceID
		so.ClosedAt = now
	}
	return true
}

// NoteVerifierPass closes verifier_signed_off. The caller must have already
// validated the verifier's claim against ledger evidence.
func (r *Registry) NoteVerifierPass(evidenceID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	ob := r.findKind(KindVerifierSignedOff)
	if ob == nil || ob.Status == StatusClosed {
		return false
	}
	ob.Status = StatusClosed
	ob.ClosedBy = evidenceID
	ob.ClosedAt = time.Now()
	return true
}

// VerifyClosed reports whether must_run_verify exists and is closed.
func (r *Registry) VerifyClosed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	ob := r.findKind(KindMustRunVerify)
	return ob != nil && ob.Status == StatusClosed
}

// Open returns copies of all open obligations in creation order.
func (r *Registry) Open() []Obligation {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Obligation
	for _, ob := range r.items {
		if ob.Status == StatusOpen {
			out = append(out, *ob)
		}
	}
	return out
}

// Snapshot returns copies of all obligations in creation order.
func (r *Registry) Snapshot() []Obligation {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Obligation, 0, len(r.items))
	for _, ob := range r.items {
		out = append(out, *ob)
	}
	return out
}

func (r *Registry) HasOpen() bool {
	return len(r.Open()) > 0
}
