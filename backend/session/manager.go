package session

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/enough/enough/backend/opencode"
)

// Manager persists conversation entries as append-only JSONL, matching Flame's layout.
type Manager struct {
	cwd         string
	sessionDir  string
	sessionFile string
	sessionID   string

	entries []json.RawMessage
	leafID  *string
	flushed bool

	// storedSystemPrompt is the cached system prompt persisted for this
	// session (TypeSystemPrompt entry). Replayed verbatim on resume so the
	// upstream prefix cache stays warm.
	storedSystemPrompt string

	labelsById          map[string]string
	labelTimestampsById map[string]string

	fpOnce       sync.Once
	fingerprints *FingerprintStore
}

// ContinueRecent opens the newest session for cwd or starts a fresh one.
func ContinueRecent(cwd string) (*Manager, error) {
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	cwd, err := filepath.Abs(cwd)
	if err != nil {
		return nil, err
	}

	dir, err := SessionDir(cwd)
	if err != nil {
		return nil, err
	}

	m := &Manager{cwd: cwd, sessionDir: dir}
	if recent := findMostRecent(dir); recent != "" {
		if err := m.openFile(recent); err != nil {
			return nil, err
		}
		return m, nil
	}

	return m, m.newSession()
}

// StartNew begins a fresh session file for cwd without resuming the newest file.
func StartNew(cwd string) (*Manager, error) {
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	cwd, err := filepath.Abs(cwd)
	if err != nil {
		return nil, err
	}

	dir, err := SessionDir(cwd)
	if err != nil {
		return nil, err
	}

	m := &Manager{cwd: cwd, sessionDir: dir}
	return m, m.newSession()
}

func (m *Manager) CWD() string        { return m.cwd }
func (m *Manager) SessionID() string  { return m.sessionID }
func (m *Manager) SessionFile() string { return m.sessionFile }
func (m *Manager) LeafID() *string    { return m.leafID }

// ParsedEntries returns the session entries parsed into FileEntry structs.
func (m *Manager) ParsedEntries() []FileEntry {
	out := make([]FileEntry, 0, len(m.entries))
	for _, raw := range m.entries {
		var entry FileEntry
		if err := json.Unmarshal(raw, &entry); err == nil {
			out = append(out, entry)
		}
	}
	return out
}

// GetBranch returns all entries from root to leafID in path order on the active branch.
func (m *Manager) GetBranch(leafID *string) []FileEntry {
	return GetBranch(m.ParsedEntries(), leafID)
}

// BuildSessionContext resolves the messages and settings on the active branch.
func (m *Manager) BuildSessionContext() SessionContext {
	return BuildSessionContext(m.ParsedEntries(), m.leafID)
}

// Messages returns LLM messages stored in the session (no system prompt).
func (m *Manager) Messages() []opencode.Message {
	return m.BuildSessionContext().Messages
}

// ChatLines returns displayable history for the TUI on the active branch.
func (m *Manager) ChatLines() []ChatLine {
	branch := m.GetBranch(m.leafID)
	var out []ChatLine

	for i := 0; i < len(branch); i++ {
		entry := branch[i]

		if entry.Type == TypeCompaction {
			out = append(out, ChatLine{
				Role:         "compactionSummary",
				Text:         entry.Summary,
				TokensBefore: entry.TokensBefore,
			})
			continue
		}

		if entry.Type == TypeBranchSummary {
			out = append(out, ChatLine{
				Role: "branchSummary",
				Text: entry.Summary,
			})
			continue
		}

		if entry.Type == TypeCustomMessage {
			var text string
			if s, ok := entry.Content.(string); ok {
				text = s
			}
			display := true
			if entry.Display != nil {
				display = *entry.Display
			}
			if display {
				out = append(out, ChatLine{
					Role: "user",
					Text: text,
				})
			}
			continue
		}

		if entry.Type != TypeMessage || entry.Message == nil {
			continue
		}

		msg := *entry.Message

		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			thinking := ""
			if msg.ReasoningContent != nil {
				thinking = strings.TrimSpace(*msg.ReasoningContent)
			}
			text := strings.TrimSpace(opencode.ContentString(msg))
			if text != "" || thinking != "" {
				out = append(out, ChatLine{Role: "assistant", Text: text, Thinking: thinking})
			}
			for _, tc := range msg.ToolCalls {
				line := ChatLine{
					Role:     "tool",
					ToolName: tc.Function.Name,
					ToolArgs: tc.Function.Arguments,
				}
				for j := i + 1; j < len(branch); j++ {
					tm := branch[j]
					if tm.Type == TypeMessage && tm.Message != nil && tm.Message.Role == "tool" && tm.Message.ToolCallID == tc.ID {
						line.ToolResult = strings.TrimSpace(opencode.ContentString(*tm.Message))
						line.ToolDetails = tm.ToolDetails
						break
					}
				}
				out = append(out, line)
			}
			continue
		}

		if msg.Role == "tool" {
			continue
		}

		if line, ok := messageToChatLine(msg); ok {
			out = append(out, line)
		}
	}
	return out
}

func messageToChatLine(msg opencode.Message) (ChatLine, bool) {
	switch msg.Role {
	case "user":
		text := strings.TrimSpace(opencode.ContentString(msg))
		var chatImages []ChatImage
		blocks := opencode.ContentBlocks(msg)
		for _, b := range blocks {
			if b.Type == "image_url" && b.ImageURL != nil {
				chatImages = append(chatImages, ChatImage{
					URL: b.ImageURL.URL,
				})
			}
		}
		if text == "" && len(chatImages) == 0 {
			return ChatLine{}, false
		}
		return ChatLine{Role: "user", Text: text, Images: chatImages}, true

	case "assistant":
		thinking := ""
		if msg.ReasoningContent != nil {
			thinking = strings.TrimSpace(*msg.ReasoningContent)
		}
		if len(msg.ToolCalls) > 0 {
			return ChatLine{}, false
		}
		text := strings.TrimSpace(opencode.ContentString(msg))
		if text == "" && thinking == "" {
			return ChatLine{}, false
		}
		return ChatLine{Role: "assistant", Text: text, Thinking: thinking}, true

	case "tool":
		return ChatLine{}, false

	default:
		return ChatLine{}, false
	}
}

// AppendMessageWithDetails appends a message entry with optional tool details and persists it to JSONL.
func (m *Manager) AppendMessageWithDetails(msg opencode.Message, toolDetails string) error {
	if msg.Role == "system" {
		return nil
	}

	parent := m.leafID
	id := newID()
	entry := FileEntry{
		SessionEntry: SessionEntry{
			Type:        TypeMessage,
			ID:          id,
			ParentID:    parent,
			Timestamp:   nowISO(),
			Message:     &msg,
			ToolDetails: toolDetails,
		},
	}

	raw, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	m.entries = append(m.entries, raw)
	m.leafID = &id
	return m.persistEntry(raw, msg.Role == "assistant")
}

// AppendMessage appends a message entry and persists it to JSONL.
func (m *Manager) AppendMessage(msg opencode.Message) error {
	return m.AppendMessageWithDetails(msg, "")
}

// AppendCompaction appends a compaction entry and persists it to JSONL.
func (m *Manager) AppendCompaction(summary string, firstKeptEntryID string, tokensBefore int, details any, fromHook bool) error {
	parent := m.leafID
	id := newID()
	entry := FileEntry{
		SessionEntry: SessionEntry{
			Type:             TypeCompaction,
			ID:               id,
			ParentID:         parent,
			Timestamp:        nowISO(),
			Summary:          summary,
			FirstKeptEntryID: firstKeptEntryID,
			TokensBefore:     tokensBefore,
			Details:          details,
			FromHook:         fromHook,
		},
	}

	raw, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	m.entries = append(m.entries, raw)
	m.leafID = &id
	return m.persistEntry(raw, true)
}

// BranchWithSummary branches to a given entry and appends a branch summary entry.
func (m *Manager) BranchWithSummary(branchFromId *string, summary string, details any, fromHook bool) (string, error) {
	m.leafID = branchFromId
	id := newID()
	fromID := "root"
	if branchFromId != nil {
		fromID = *branchFromId
	}
	entry := FileEntry{
		SessionEntry: SessionEntry{
			Type:      TypeBranchSummary,
			ID:        id,
			ParentID:  branchFromId,
			Timestamp: nowISO(),
			FromID:    fromID,
			Summary:   summary,
			Details:   details,
			FromHook:  fromHook,
		},
	}

	raw, err := json.Marshal(entry)
	if err != nil {
		return "", err
	}

	m.entries = append(m.entries, raw)
	m.leafID = &id
	if err := m.persistEntry(raw, true); err != nil {
		return "", err
	}
	return id, nil
}

// NewSession starts a new JSONL file in the same cwd session directory.
func (m *Manager) NewSession() error {
	return m.newSession()
}

func (m *Manager) newSession() error {
	m.sessionID = newID()
	ts := time.Now().UTC()
	header := Header{
		Type:      "session",
		Version:   CurrentVersion,
		ID:        m.sessionID,
		Timestamp: ts.Format(time.RFC3339Nano),
		CWD:       m.cwd,
	}

	raw, err := json.Marshal(header)
	if err != nil {
		return err
	}

	m.entries = []json.RawMessage{raw}
	m.leafID = nil
	m.flushed = false
	m.sessionFile = filepath.Join(m.sessionDir, fmt.Sprintf("%s_%s.jsonl", fileTimestamp(ts), m.sessionID))
	m.flushed = false
	m.storedSystemPrompt = ""
	m.labelsById = make(map[string]string)
	m.labelTimestampsById = make(map[string]string)
	return nil
}

// StoredSystemPrompt returns the cached system prompt persisted for this
// session, or "" when none was stored.
func (m *Manager) StoredSystemPrompt() string {
	return m.storedSystemPrompt
}

// SetSystemPrompt persists the session's cached system prompt. The entry does
// not advance the message leaf (same pattern as labels), so it never enters
// the LLM context — it is metadata replayed as the system message on resume.
func (m *Manager) SetSystemPrompt(prompt string) error {
	if prompt == m.storedSystemPrompt {
		return nil
	}
	id := newID()
	entry := FileEntry{
		SessionEntry: SessionEntry{
			Type:      TypeSystemPrompt,
			ID:        id,
			ParentID:  m.leafID,
			Timestamp: nowISO(),
			Content:   prompt,
		},
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	m.entries = append(m.entries, raw)
	m.storedSystemPrompt = prompt
	return m.persistEntry(raw, false)
}

func (m *Manager) openFile(path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var entries []json.RawMessage
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		entries = append(entries, json.RawMessage(line))
	}
	if len(entries) == 0 {
		return m.newSession()
	}

	var header Header
	if err := json.Unmarshal(entries[0], &header); err != nil || header.Type != "session" || header.ID == "" {
		return m.newSession()
	}

	m.sessionFile = path
	m.sessionID = header.ID
	m.entries = entries
	m.leafID = nil
	m.flushed = true

	m.labelsById = make(map[string]string)
	m.labelTimestampsById = make(map[string]string)

	for _, raw := range entries[1:] {
		var entry FileEntry
		if json.Unmarshal(raw, &entry) == nil && entry.Type != TypeSession && entry.ID != "" {
			// Don't set leafID to TypeLabel, TypeSessionInfo or
			// TypeSystemPrompt as active message leaf
			if entry.Type != TypeLabel && entry.Type != TypeSessionInfo && entry.Type != TypeSystemPrompt {
				m.leafID = &entry.ID
			}
			if entry.Type == TypeSystemPrompt {
				if s, ok := entry.Content.(string); ok {
					m.storedSystemPrompt = s
				}
			}
			if entry.Type == TypeLabel {
				if entry.Label != "" {
					m.labelsById[entry.TargetID] = entry.Label
					m.labelTimestampsById[entry.TargetID] = entry.Timestamp
				} else {
					delete(m.labelsById, entry.TargetID)
					delete(m.labelTimestampsById, entry.TargetID)
				}
			}
		}
	}

	return nil
}

func (m *Manager) persistEntry(entry json.RawMessage, isAssistant bool) error {
	if m.sessionFile == "" {
		return errors.New("session file not initialized")
	}

	if !m.hasAssistant() && !isAssistant {
		return nil
	}

	if !m.flushed {
		return m.rewriteFile()
	}

	f, err := os.OpenFile(m.sessionFile, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.WriteString(f, string(entry)+"\n")
	if err == nil {
		m.flushed = true
	}
	return err
}

func (m *Manager) hasAssistant() bool {
	for _, raw := range m.entries {
		var entry FileEntry
		if json.Unmarshal(raw, &entry) == nil && entry.Type == TypeMessage && entry.Message != nil && entry.Message.Role == "assistant" {
			return true
		}
	}
	return false
}

func (m *Manager) rewriteFile() error {
	if m.sessionFile == "" {
		return errors.New("session file not initialized")
	}

	if err := os.MkdirAll(filepath.Dir(m.sessionFile), 0o700); err != nil {
		return err
	}

	var b strings.Builder
	for i, raw := range m.entries {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.Write(raw)
	}
	if len(m.entries) > 0 {
		b.WriteByte('\n')
	}

	if err := os.WriteFile(m.sessionFile, []byte(b.String()), 0o600); err != nil {
		return err
	}
	m.flushed = true
	return nil
}

func findMostRecent(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}

	var bestPath string
	var bestTime time.Time

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if !validSessionFile(path) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if bestPath == "" || info.ModTime().After(bestTime) {
			bestPath = path
			bestTime = info.ModTime()
		}
	}

	return bestPath
}

func validSessionFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	line, err := bufio.NewReader(f).ReadString('\n')
	if err != nil && err != io.EOF {
		return false
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return false
	}

	var header Header
	return json.Unmarshal([]byte(line), &header) == nil && header.Type == "session" && header.ID != ""
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

type SessionTreeNode struct {
	Entry          FileEntry
	Children       []*SessionTreeNode
	Label          string
	LabelTimestamp string
}

func (m *Manager) GetTree() []*SessionTreeNode {
	entries := m.ParsedEntries()
	nodeMap := make(map[string]*SessionTreeNode)
	var roots []*SessionTreeNode

	for _, entry := range entries {
		if entry.ID == "" || entry.Type == TypeSession {
			continue
		}
		label := m.labelsById[entry.ID]
		labelTS := m.labelTimestampsById[entry.ID]
		nodeMap[entry.ID] = &SessionTreeNode{
			Entry:          entry,
			Children:       []*SessionTreeNode{},
			Label:          label,
			LabelTimestamp: labelTS,
		}
	}

	for _, entry := range entries {
		if entry.ID == "" || entry.Type == TypeSession {
			continue
		}
		node := nodeMap[entry.ID]
		if entry.ParentID == nil || *entry.ParentID == "" || *entry.ParentID == entry.ID {
			roots = append(roots, node)
		} else {
			parent := nodeMap[*entry.ParentID]
			if parent != nil {
				parent.Children = append(parent.Children, node)
			} else {
				roots = append(roots, node)
			}
		}
	}

	var sortTree func(*SessionTreeNode)
	sortTree = func(node *SessionTreeNode) {
		sort.Slice(node.Children, func(i, j int) bool {
			ti, _ := time.Parse(time.RFC3339Nano, node.Children[i].Entry.Timestamp)
			tj, _ := time.Parse(time.RFC3339Nano, node.Children[j].Entry.Timestamp)
			return ti.Before(tj)
		})
		for _, child := range node.Children {
			sortTree(child)
		}
	}

	sort.Slice(roots, func(i, j int) bool {
		ti, _ := time.Parse(time.RFC3339Nano, roots[i].Entry.Timestamp)
		tj, _ := time.Parse(time.RFC3339Nano, roots[j].Entry.Timestamp)
		return ti.Before(tj)
	})

	for _, root := range roots {
		sortTree(root)
	}

	return roots
}

func (m *Manager) AppendLabelChange(targetID string, label string) (string, error) {
	id := newID()
	entry := FileEntry{
		SessionEntry: SessionEntry{
			Type:      TypeLabel,
			ID:        id,
			ParentID:  m.leafID,
			Timestamp: nowISO(),
			TargetID:  targetID,
			Label:     label,
		},
	}

	raw, err := json.Marshal(entry)
	if err != nil {
		return "", err
	}

	m.entries = append(m.entries, raw)
	if err := m.persistEntry(raw, false); err != nil {
		return "", err
	}
	if label != "" {
		if m.labelsById == nil {
			m.labelsById = make(map[string]string)
		}
		if m.labelTimestampsById == nil {
			m.labelTimestampsById = make(map[string]string)
		}
		m.labelsById[targetID] = label
		m.labelTimestampsById[targetID] = entry.Timestamp
	} else {
		delete(m.labelsById, targetID)
		delete(m.labelTimestampsById, targetID)
	}
	return id, nil
}

func (m *Manager) Branch(branchFromId string) {
	m.leafID = &branchFromId
}

func (m *Manager) ResetLeaf() {
	m.leafID = nil
}
