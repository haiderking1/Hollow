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
	"strings"
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

func (m *Manager) CWD() string        { return m.cwd }
func (m *Manager) SessionID() string  { return m.sessionID }
func (m *Manager) SessionFile() string { return m.sessionFile }

// Messages returns LLM messages stored in the session (no system prompt).
func (m *Manager) Messages() []opencode.Message {
	var out []opencode.Message
	for _, raw := range m.entries {
		var peek struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(raw, &peek) != nil || peek.Type != "message" {
			continue
		}
		var entry MessageEntry
		if json.Unmarshal(raw, &entry) != nil {
			continue
		}
		if entry.Message.Role == "system" {
			continue
		}
		out = append(out, entry.Message)
	}
	return out
}

// ChatLines returns displayable history for the TUI.
func (m *Manager) ChatLines() []ChatLine {
	var out []ChatLine
	for _, msg := range m.Messages() {
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
		if text == "" {
			return ChatLine{}, false
		}
		return ChatLine{Role: "user", Text: text}, true

	case "assistant":
		thinking := ""
		if msg.ReasoningContent != nil {
			thinking = strings.TrimSpace(*msg.ReasoningContent)
		}
		if len(msg.ToolCalls) > 0 {
			var parts []string
			for _, tc := range msg.ToolCalls {
				parts = append(parts, fmt.Sprintf("%s(%s)", tc.Function.Name, truncate(tc.Function.Arguments, 80)))
			}
			return ChatLine{Role: "tool", Text: strings.Join(parts, ", ")}, true
		}
		text := strings.TrimSpace(opencode.ContentString(msg))
		if text == "" && thinking == "" {
			return ChatLine{}, false
		}
		return ChatLine{Role: "assistant", Text: text, Thinking: thinking}, true

	case "tool":
		name := msg.Name
		if name == "" {
			name = "tool"
		}
		text := truncate(strings.TrimSpace(opencode.ContentString(msg)), 200)
		return ChatLine{Role: "tool", Text: name + ": " + text}, true

	default:
		return ChatLine{}, false
	}
}

// AppendMessage appends a message entry and persists it to JSONL.
func (m *Manager) AppendMessage(msg opencode.Message) error {
	if msg.Role == "system" {
		return nil
	}

	parent := m.leafID
	entry := MessageEntry{
		Type:      "message",
		ID:        newID(),
		ParentID:  parent,
		Timestamp: nowISO(),
		Message:   msg,
	}

	raw, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	m.entries = append(m.entries, raw)
	m.leafID = &entry.ID
	return m.persistEntry(raw, msg.Role == "assistant")
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
	return nil
}

func (m *Manager) openFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	var entries []json.RawMessage
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		entries = append(entries, json.RawMessage(line))
	}
	if err := scanner.Err(); err != nil {
		return err
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

	for _, raw := range entries[1:] {
		var peek struct {
			Type string `json:"type"`
			ID   string `json:"id"`
		}
		if json.Unmarshal(raw, &peek) == nil && peek.Type != "session" && peek.ID != "" {
			id := peek.ID
			m.leafID = &id
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
		var entry MessageEntry
		if json.Unmarshal(raw, &entry) == nil && entry.Message.Role == "assistant" {
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

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
