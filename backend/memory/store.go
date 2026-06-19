// Package memory provides Enough's bounded, file-backed persistent memory.
//
// Two stores live under ~/.enough/memories/:
//   - MEMORY.md: agent's personal notes (environment facts, project
//     conventions, tool quirks, things learned)
//   - USER.md: what the agent knows about the user (preferences,
//     communication style, expectations, workflow habits)
//
// Both are injected into the system prompt as a frozen snapshot at session
// start. Mid-session writes update files on disk immediately (durable) but do
// NOT change the system prompt — this preserves the prefix cache for the
// entire session. The snapshot refreshes on the next session start.
//
// Entry delimiter: § (section sign). Entries can be multiline.
// Character limits (not tokens) because char counts are model-independent.
package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/enough/enough/backend/enoughhome"
	"github.com/enough/enough/backend/fslock"
)

const EntryDelimiter = "\n§\n"

const (
	TargetMemory = "memory"
	TargetUser   = "user"
)

// Dir returns the memories directory (resolved dynamically so ENOUGH_HOME
// overrides are always respected).
func Dir() string {
	return filepath.Join(enoughhome.HomeDir(), "memories")
}

// PathFor maps a target to its on-disk file.
func PathFor(target string) string {
	if target == TargetUser {
		return filepath.Join(Dir(), "USER.md")
	}
	return filepath.Join(Dir(), "MEMORY.md")
}

// Result is the structured outcome of a memory operation, serialized into the
// tool response.
type Result struct {
	Success        bool     `json:"success"`
	Target         string   `json:"target,omitempty"`
	Entries        []string `json:"entries,omitempty"`
	Usage          string   `json:"usage,omitempty"`
	EntryCount     int      `json:"entry_count,omitempty"`
	Message        string   `json:"message,omitempty"`
	Error          string   `json:"error,omitempty"`
	CurrentEntries []string `json:"current_entries,omitempty"`
	Matches        []string `json:"matches,omitempty"`
	DriftBackup    string   `json:"drift_backup,omitempty"`
	Remediation    string   `json:"remediation,omitempty"`
}

// Store is bounded curated memory with file persistence. One instance per
// Agent (shared with background-review forks).
//
// It maintains two parallel states:
//   - snapshot: frozen at LoadFromDisk(), used for system prompt injection.
//     Never mutated mid-session. Keeps the prefix cache stable.
//   - memoryEntries / userEntries: live state, mutated by tool calls and
//     persisted to disk immediately. Tool responses always reflect this.
type Store struct {
	mu sync.Mutex

	memoryEntries []string
	userEntries   []string

	memoryCharLimit int
	userCharLimit   int

	// snapshot holds the rendered system-prompt blocks frozen at load time.
	snapshot map[string]string
}

func NewStore(memoryCharLimit, userCharLimit int) *Store {
	if memoryCharLimit <= 0 {
		memoryCharLimit = 2200
	}
	if userCharLimit <= 0 {
		userCharLimit = 1375
	}
	return &Store{
		memoryCharLimit: memoryCharLimit,
		userCharLimit:   userCharLimit,
		snapshot:        map[string]string{TargetMemory: "", TargetUser: ""},
	}
}

// LoadFromDisk loads entries from MEMORY.md and USER.md and captures the
// frozen system-prompt snapshot.
//
// Each entry is threat-scanned at snapshot-build time — any hit replaces the
// entry text in the snapshot with a "[BLOCKED: …]" placeholder, so a
// poisoned-on-disk memory file cannot inject into the system prompt. The live
// entry lists keep the original text so the user can still SEE poisoned
// entries via memory(action=read) and remove them — silently dropping them
// would hide the attack from the user.
func (s *Store) LoadFromDisk() {
	s.mu.Lock()
	defer s.mu.Unlock()

	_ = os.MkdirAll(Dir(), 0o700)

	s.memoryEntries = dedupe(readEntries(PathFor(TargetMemory)))
	s.userEntries = dedupe(readEntries(PathFor(TargetUser)))

	s.snapshot = map[string]string{
		TargetMemory: s.renderBlock(TargetMemory, sanitizeForSnapshot(s.memoryEntries, "MEMORY.md")),
		TargetUser:   s.renderBlock(TargetUser, sanitizeForSnapshot(s.userEntries, "USER.md")),
	}
}

// FormatForSystemPrompt returns the frozen snapshot block for target.
// This is the state captured at LoadFromDisk() time, NOT the live state —
// mid-session writes do not affect it. Empty string when there were no
// entries at load time.
func (s *Store) FormatForSystemPrompt(target string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshot[target]
}

// Add appends a new entry. Errors if it would exceed the char limit.
func (s *Store) Add(target, content string) Result {
	content = strings.TrimSpace(content)
	if content == "" {
		return Result{Success: false, Error: "Content cannot be empty."}
	}
	if msg := FirstThreatMessage(content); msg != "" {
		return Result{Success: false, Error: msg}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return s.withFileLock(target, func() Result {
		if bak := s.reloadTarget(target); bak != "" {
			return driftError(target, bak)
		}

		entries := s.entriesFor(target)
		limit := s.charLimit(target)

		for _, e := range entries {
			if e == content {
				return s.successResponse(target, "Entry already exists (no duplicate added).")
			}
		}

		newEntries := append(append([]string(nil), entries...), content)
		newTotal := len(strings.Join(newEntries, EntryDelimiter))
		if newTotal > limit {
			current := s.charCount(target)
			return Result{
				Success: false,
				Error: fmt.Sprintf(
					"Memory at %s/%s chars. Adding this entry (%d chars) would exceed the limit. "+
						"Consolidate now: use 'replace' to merge overlapping entries into shorter ones "+
						"or 'remove' stale or less important entries (see current_entries below), "+
						"then retry this add — all in this turn.",
					formatThousands(current), formatThousands(limit), len(content)),
				CurrentEntries: entries,
				Usage:          fmt.Sprintf("%s/%s", formatThousands(current), formatThousands(limit)),
			}
		}

		s.setEntries(target, newEntries)
		if err := s.saveToDisk(target); err != nil {
			return Result{Success: false, Error: err.Error()}
		}
		return s.successResponse(target, "Entry added.")
	})
}

// Replace finds the entry containing oldText as a substring and replaces it
// with newContent.
func (s *Store) Replace(target, oldText, newContent string) Result {
	oldText = strings.TrimSpace(oldText)
	newContent = strings.TrimSpace(newContent)
	if oldText == "" {
		return Result{Success: false, Error: "match text cannot be empty."}
	}
	if newContent == "" {
		return Result{Success: false, Error: "replacement cannot be empty. Use 'remove' to delete entries."}
	}
	if msg := FirstThreatMessage(newContent); msg != "" {
		return Result{Success: false, Error: msg}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return s.withFileLock(target, func() Result {
		if bak := s.reloadTarget(target); bak != "" {
			return driftError(target, bak)
		}

		entries := s.entriesFor(target)
		idx, errRes := matchSingle(entries, oldText)
		if errRes != nil {
			return *errRes
		}

		limit := s.charLimit(target)
		test := append([]string(nil), entries...)
		test[idx] = newContent
		newTotal := len(strings.Join(test, EntryDelimiter))
		if newTotal > limit {
			current := s.charCount(target)
			return Result{
				Success: false,
				Error: fmt.Sprintf(
					"Replacement would put memory at %s/%s chars. Shorten the new content, or "+
						"'remove' other stale or less important entries to make room (see "+
						"current_entries below), then retry — all in this turn.",
					formatThousands(newTotal), formatThousands(limit)),
				CurrentEntries: entries,
				Usage:          fmt.Sprintf("%s/%s", formatThousands(current), formatThousands(limit)),
			}
		}

		s.setEntries(target, test)
		if err := s.saveToDisk(target); err != nil {
			return Result{Success: false, Error: err.Error()}
		}
		return s.successResponse(target, "Entry replaced.")
	})
}

// Remove deletes the entry containing oldText as a substring.
func (s *Store) Remove(target, oldText string) Result {
	oldText = strings.TrimSpace(oldText)
	if oldText == "" {
		return Result{Success: false, Error: "match text cannot be empty."}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	return s.withFileLock(target, func() Result {
		if bak := s.reloadTarget(target); bak != "" {
			return driftError(target, bak)
		}

		entries := s.entriesFor(target)
		idx, errRes := matchSingle(entries, oldText)
		if errRes != nil {
			return *errRes
		}

		entries = append(entries[:idx], entries[idx+1:]...)
		s.setEntries(target, entries)
		if err := s.saveToDisk(target); err != nil {
			return Result{Success: false, Error: err.Error()}
		}
		return s.successResponse(target, "Entry removed.")
	})
}

// Read returns the live state for the target (not the frozen snapshot).
func (s *Store) Read(target string) Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.successResponse(target, "")
}

// -- internal helpers (callers hold s.mu) --

func (s *Store) entriesFor(target string) []string {
	if target == TargetUser {
		return s.userEntries
	}
	return s.memoryEntries
}

func (s *Store) setEntries(target string, entries []string) {
	if target == TargetUser {
		s.userEntries = entries
	} else {
		s.memoryEntries = entries
	}
}

func (s *Store) charLimit(target string) int {
	if target == TargetUser {
		return s.userCharLimit
	}
	return s.memoryCharLimit
}

func (s *Store) charCount(target string) int {
	entries := s.entriesFor(target)
	if len(entries) == 0 {
		return 0
	}
	return len(strings.Join(entries, EntryDelimiter))
}

// matchSingle finds the unique entry containing needle. Returns the index, or
// a Result describing why the match failed. Identical duplicate matches
// collapse to the first one.
func matchSingle(entries []string, needle string) (int, *Result) {
	var idxs []int
	for i, e := range entries {
		if strings.Contains(e, needle) {
			idxs = append(idxs, i)
		}
	}
	if len(idxs) == 0 {
		return 0, &Result{Success: false, Error: fmt.Sprintf("No entry matched '%s'.", needle)}
	}
	if len(idxs) > 1 {
		unique := make(map[string]bool)
		for _, i := range idxs {
			unique[entries[i]] = true
		}
		if len(unique) > 1 {
			var previews []string
			for _, i := range idxs {
				e := entries[i]
				if len(e) > 80 {
					e = e[:80] + "..."
				}
				previews = append(previews, e)
			}
			return 0, &Result{
				Success: false,
				Error:   fmt.Sprintf("Multiple entries matched '%s'. Be more specific.", needle),
				Matches: previews,
			}
		}
		// All identical — operate on the first.
	}
	return idxs[0], nil
}

func (s *Store) successResponse(target, message string) Result {
	entries := s.entriesFor(target)
	current := s.charCount(target)
	limit := s.charLimit(target)
	pct := 0
	if limit > 0 {
		pct = current * 100 / limit
		if pct > 100 {
			pct = 100
		}
	}
	return Result{
		Success:    true,
		Target:     target,
		Entries:    append([]string(nil), entries...),
		Usage:      fmt.Sprintf("%d%% — %s/%s chars", pct, formatThousands(current), formatThousands(limit)),
		EntryCount: len(entries),
		Message:    message,
	}
}

// renderBlock renders a system-prompt block with header and usage indicator.
func (s *Store) renderBlock(target string, entries []string) string {
	if len(entries) == 0 {
		return ""
	}
	limit := s.charLimit(target)
	content := strings.Join(entries, EntryDelimiter)
	current := len(content)
	pct := 0
	if limit > 0 {
		pct = current * 100 / limit
		if pct > 100 {
			pct = 100
		}
	}
	var header string
	if target == TargetUser {
		header = fmt.Sprintf("USER PROFILE (who the user is) [%d%% — %s/%s chars]", pct, formatThousands(current), formatThousands(limit))
	} else {
		header = fmt.Sprintf("MEMORY (your personal notes) [%d%% — %s/%s chars]", pct, formatThousands(current), formatThousands(limit))
	}
	separator := strings.Repeat("═", 46)
	return fmt.Sprintf("%s\n%s\n%s\n%s", separator, header, separator, content)
}

// sanitizeForSnapshot replaces any threat-matching entry with a "[BLOCKED: …]"
// placeholder — the placeholder enters the snapshot, the original entry stays
// in live state for the user to inspect and delete.
func sanitizeForSnapshot(entries []string, filename string) []string {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry == "" || strings.HasPrefix(entry, "[BLOCKED:") {
			out = append(out, entry)
			continue
		}
		if ids := threatPatternIDs(entry, ScopeStrict); len(ids) > 0 {
			out = append(out, fmt.Sprintf(
				"[BLOCKED: %s entry contained threat pattern(s): %s. Removed from system prompt; "+
					"use memory(action=read) to inspect and memory(action=remove) to delete the original.]",
				filename, strings.Join(ids, ", ")))
		} else {
			out = append(out, entry)
		}
	}
	return out
}

// reloadTarget re-reads entries from disk into the live state (under the file
// lock, so other-session writes are picked up before mutating). Returns the
// backup path when external drift was detected — the caller must abort the
// mutation, since flushing would discard the un-roundtrippable content.
func (s *Store) reloadTarget(target string) string {
	bak := s.detectExternalDrift(target)
	fresh := dedupe(readEntries(PathFor(target)))
	s.setEntries(target, fresh)
	return bak
}

func (s *Store) saveToDisk(target string) error {
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return fmt.Errorf("failed to create memories dir: %w", err)
	}
	content := strings.Join(s.entriesFor(target), EntryDelimiter)
	if err := atomicWrite(PathFor(target), []byte(content)); err != nil {
		return fmt.Errorf("failed to write memory file %s: %w", PathFor(target), err)
	}
	return nil
}

// detectExternalDrift returns a backup-path string when the on-disk content
// shows external drift: either a round-trip mismatch (re-parsing and
// re-serializing doesn't reproduce the bytes) or a single parsed entry
// exceeding the store's whole-file char limit (an external writer appended
// free-form content). The file is snapshotted to .bak.<ts> so the operator
// can recover whatever the external writer added.
func (s *Store) detectExternalDrift(target string) string {
	path := PathFor(target)
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	raw := string(data)
	if strings.TrimSpace(raw) == "" {
		return ""
	}

	parsed := readEntriesFromString(raw)
	roundtrip := strings.Join(parsed, EntryDelimiter)

	limit := s.charLimit(target)
	maxEntryLen := 0
	for _, e := range parsed {
		if len(e) > maxEntryLen {
			maxEntryLen = len(e)
		}
	}

	if strings.TrimSpace(raw) == roundtrip && maxEntryLen <= limit {
		return ""
	}

	bakPath := fmt.Sprintf("%s.bak.%d", path, time.Now().Unix())
	if err := os.WriteFile(bakPath, data, 0o600); err != nil {
		return bakPath + " (BACKUP FAILED — file unchanged on disk)"
	}
	return bakPath
}

func driftError(target, bakPath string) Result {
	name := filepath.Base(PathFor(target))
	return Result{
		Success: false,
		Error: fmt.Sprintf(
			"Refusing to write %s: file on disk has content that wouldn't round-trip through the "+
				"memory tool (likely added by a manual edit, a shell append, or a concurrent session). "+
				"A snapshot was saved to %s. Resolve the drift first — either rewrite the file as a "+
				"clean §-delimited list of entries, or move the extra content out — then retry. "+
				"This guard exists to prevent silent data loss.",
			name, bakPath),
		DriftBackup: bakPath,
		Remediation: "Open the .bak file, integrate the missing entries into the memory tool one at " +
			"a time via memory(action=add, content=...), then remove or rewrite the original file " +
			"to a clean state.",
	}
}

// withFileLock acquires an exclusive lock on a sidecar .lock file for the
// duration of a read-modify-write so concurrent sessions don't clobber each
// other. The memory file itself is replaced atomically.
func (s *Store) withFileLock(target string, f func() Result) Result {
	path := PathFor(target)
	lockPath := path + ".lock"
	_ = os.MkdirAll(filepath.Dir(lockPath), 0o700)

	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return Result{Success: false, Error: "failed to create lock file: " + err.Error()}
	}
	defer func() { _ = file.Close() }()

	if err := fslock.Lock(file); err != nil {
		return Result{Success: false, Error: "failed to acquire file lock: " + err.Error()}
	}
	defer func() { _ = fslock.Unlock(file) }()

	return f()
}

func readEntries(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return readEntriesFromString(string(data))
}

func readEntriesFromString(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	for _, e := range strings.Split(raw, EntryDelimiter) {
		e = strings.TrimSpace(e)
		if e != "" {
			out = append(out, e)
		}
	}
	return out
}

func dedupe(entries []string) []string {
	seen := make(map[string]bool, len(entries))
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	return out
}

func atomicWrite(filename string, data []byte) error {
	dir := filepath.Dir(filename)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".mem_*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, filename)
}

func formatThousands(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	parts = append([]string{s}, parts...)
	return strings.Join(parts, ",")
}
