package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/enough/enough/backend/agent/evidence"
	"github.com/enough/enough/backend/agent/obligations"
	"github.com/enough/enough/backend/core"
	"github.com/enough/enough/backend/session"
)

// sessionFingerprints adapts the session store to the evidence seeder.
func sessionFingerprints(sm *session.Manager) []evidence.Fingerprint {
	var out []evidence.Fingerprint
	for _, fp := range sm.Fingerprints().List() {
		out = append(out, evidence.Fingerprint{Path: fp.Path, AfterHash: fp.AfterHash})
	}
	return out
}

// evidenceLedger returns the agent's per-turn ledger, lazily creating one.
// Swarm workers are fresh Agent structs, so each worker gets its own ledger.
func (a *Agent) evidenceLedger() *evidence.Ledger {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ledger == nil {
		a.ledger = evidence.NewLedger("")
	}
	return a.ledger
}

func (a *Agent) resetEvidenceLedger(turnID string) {
	a.mu.Lock()
	a.ledger = evidence.NewLedger(turnID)
	a.mu.Unlock()
}

func (a *Agent) evidenceEnabled() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfg.Evidence.Enabled
}

// guardTool enforces hard rules before a tool runs. A non-nil result is the
// rejection returned to the model; the tool is never executed.
//
// Rules:
//   - allowlisted agents (the verifier) may only call their permitted tools
//   - write_file/edit_file on an existing path P is rejected unless the
//     ledger holds a read_file entry for P from this turn. Writing a path
//     that does not exist yet is allowed — there is nothing to read.
func (a *Agent) guardTool(name, argsJSON string) *toolResult {
	if a.allowedTools != nil && !a.allowedTools[name] {
		return &toolResult{
			output: fmt.Sprintf("REJECTED: tool '%s' is not permitted for this role", name),
			isErr:  true,
		}
	}

	if name != "write_file" && name != "edit_file" {
		return nil
	}
	if !a.evidenceEnabled() {
		return nil
	}

	path, ok := toolPathArg(argsJSON)
	if !ok {
		return nil // malformed args; let the tool produce its own error
	}
	abs, err := a.resolvePath(path)
	if err != nil {
		return nil // path errors are the tool's to report
	}
	if _, statErr := os.Stat(abs); os.IsNotExist(statErr) {
		return nil // creating a new file requires no prior read
	}

	if !a.evidenceLedger().HasRead(abs) {
		return &toolResult{
			output: fmt.Sprintf("REJECTED: edit/write blocked — read_file '%s' not in evidence ledger this turn", path),
			isErr:  true,
		}
	}
	return nil
}

// recordEvidence appends a ledger entry for a successful tool execution.
// beforeHash carries the pre-mutation content hash captured by executeTool.
func (a *Agent) recordEvidence(name, argsJSON string, beforeHash string) {
	if !a.evidenceEnabled() {
		return
	}

	path, ok := toolPathArg(argsJSON)
	if !ok {
		return
	}
	abs, err := a.resolvePath(path)
	if err != nil {
		return
	}

	var entry evidence.Entry
	var appendErr error
	switch name {
	case "read_file":
		data, err := os.ReadFile(abs)
		if err != nil {
			return
		}
		lines := strings.Count(string(data), "\n")
		if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
			lines++
		}
		entry, appendErr = a.evidenceLedger().Append(evidence.KindReadFile, evidence.ReadFilePayload{
			Path:        abs,
			ContentHash: evidence.HashBytes(data),
			LineCount:   lines,
		})

	case "write_file", "edit_file":
		kind := evidence.KindWriteFile
		if name == "edit_file" {
			kind = evidence.KindEditFile
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return
		}
		afterHash := evidence.HashBytes(data)
		entry, appendErr = a.evidenceLedger().Append(kind, evidence.MutationPayload{
			Path:       abs,
			BeforeHash: beforeHash,
			AfterHash:  afterHash,
		})
		if appendErr == nil {
			// In-turn author credit: the agent knows what it just wrote, so a
			// follow-up edit this turn must not demand a redundant read.
			a.evidenceLedger().NoteAuthorCredit(abs, afterHash)

			// Cross-turn continuity: persist the mutation fingerprint so the
			// next turn can seed read credit if the file is still unchanged.
			a.mu.Lock()
			sm := a.session
			a.mu.Unlock()
			if sm != nil {
				sm.Fingerprints().Upsert(session.FileFingerprint{
					Path:      abs,
					AfterHash: afterHash,
					Kind:      name,
					TurnID:    a.evidenceLedger().TurnID(),
					Timestamp: time.Now(),
				})
			}
		}

	default:
		return
	}

	if appendErr != nil {
		return
	}
	a.emitEvidence(entry.Kind, abs)

	if name == "write_file" || name == "edit_file" {
		a.noteMutation()
	}
}

// recordCommandRun appends command evidence (success or failure — exit codes
// are facts either way) and closes the verify obligation on a matching pass.
func (a *Agent) recordCommandRun(command string, exitCode int, output string, duration time.Duration) {
	if !a.evidenceEnabled() {
		return
	}

	entry, err := a.evidenceLedger().Append(evidence.KindCommandRun, evidence.CommandRunPayload{
		Command:    command,
		Cwd:        a.workDir,
		ExitCode:   exitCode,
		OutputHash: evidence.HashBytes([]byte(output)),
		DurationMs: duration.Milliseconds(),
	})
	if err != nil {
		return
	}
	a.emitEvidence(entry.Kind, command)

	if reg := a.obligationRegistry(); reg != nil {
		touches := commandTouchesMutation(command, a.evidenceLedger().MutatedPaths())
		if reg.NoteCommandRun(command, exitCode, entry.ID, touches) {
			a.noteVerifySuccess()
			a.emitObligations()
		} else if obligations.IsVerifyCommand(command, reg.VerifyCommand(), reg.ExtraVerifyCommands()) {
			pytestNoTests := exitCode == 5 && strings.Contains(command, "pytest")
			if exitCode != 0 && !pytestNoTests {
				a.noteVerifyFailure()
			}
		}
	}
}

// commandTouchesMutation reports whether the command references a file
// mutated this turn — e.g. `python3 hello.py` after writing hello.py. Such a
// run is task-scoped verification of the change itself.
func commandTouchesMutation(command string, mutated []string) bool {
	for _, p := range mutated {
		if base := filepath.Base(p); base != "" && strings.Contains(command, base) {
			return true
		}
	}
	return false
}

// noteMutation tells the registry the workspace changed: instantiates the
// verify obligation and, in strict mode, reopens a previously closed one.
func (a *Agent) noteMutation() {
	if reg := a.obligationRegistry(); reg != nil {
		if reg.NoteMutation() {
			a.emitObligations()
		}
	}
}

func (a *Agent) obligationRegistry() *obligations.Registry {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.obligations
}

func (a *Agent) emitEvidence(kind evidence.Kind, path string) {
	if a.emit == nil {
		return
	}
	a.emit(core.Event{
		Kind: core.EventEvidenceAppend,
		Data: core.EvidenceEvent{
			Kind:  string(kind),
			Path:  path,
			Count: a.evidenceLedger().Count(),
		},
	})
}

func (a *Agent) emitObligations() {
	reg := a.obligationRegistry()
	if a.emit == nil || reg == nil {
		return
	}
	snap := reg.Snapshot()
	ev := core.ObligationEvent{}
	for _, ob := range snap {
		item := core.ObligationItem{
			Kind:        string(ob.Kind),
			Description: ob.Description,
			Closed:      ob.Status == obligations.StatusClosed,
		}
		ev.Items = append(ev.Items, item)
		if item.Closed {
			ev.Closed++
		} else {
			ev.Open++
		}
	}
	a.emit(core.Event{Kind: core.EventObligationUpdate, Data: ev})
}

// fileHashIfExists returns the content hash of the tool's target path before
// it runs, or "" when the file does not exist or the args carry no path.
func (a *Agent) fileHashIfExists(argsJSON string) string {
	path, ok := toolPathArg(argsJSON)
	if !ok {
		return ""
	}
	abs, err := a.resolvePath(path)
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return ""
	}
	return evidence.HashBytes(data)
}

func toolPathArg(argsJSON string) (string, bool) {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil || args.Path == "" {
		return "", false
	}
	return args.Path, true
}
