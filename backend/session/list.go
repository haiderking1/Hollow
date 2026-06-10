package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/enough/enough/backend/opencode"
)

// Info summarizes a session JSONL file for listing and resume pickers.
type Info struct {
	Path         string
	ID           string
	CWD          string
	Modified     time.Time
	Created      time.Time
	MessageCount int
	FirstMessage string
}

// Open loads a specific session file.
func Open(path string) (*Manager, error) {
	path = filepath.Clean(path)
	m := &Manager{sessionDir: filepath.Dir(path)}
	if err := m.openFile(path); err != nil {
		return nil, err
	}
	if m.cwd == "" {
		var header Header
		_ = json.Unmarshal(m.entries[0], &header)
		m.cwd = header.CWD
	}
	return m, nil
}

func (m *Manager) SessionDir() string {
	return m.sessionDir
}

// ListForCWD returns sessions for a project directory, newest first.
func ListForCWD(cwd string) ([]Info, error) {
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	dir, err := SessionDir(cwd)
	if err != nil {
		return nil, err
	}
	return ListDir(dir)
}

// ListAll returns sessions across every project directory, newest first.
func ListAll() ([]Info, error) {
	agentDir, err := HomeAgentDir()
	if err != nil {
		return nil, err
	}
	root := filepath.Join(agentDir, SessionsSubdir)
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var out []Info
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		infos, err := ListDir(filepath.Join(root, e.Name()))
		if err != nil {
			continue
		}
		out = append(out, infos...)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Modified.After(out[j].Modified)
	})
	return out, nil
}

// ListDir lists valid session files in a directory.
func ListDir(dir string) ([]Info, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var out []Info
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		info, err := ScanInfo(path)
		if err != nil || info == nil {
			continue
		}
		out = append(out, *info)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].Modified.After(out[j].Modified)
	})
	return out, nil
}

// ScanInfo reads session metadata from a JSONL file without loading all messages.
func ScanInfo(path string) (*Info, error) {
	if !validSessionFile(path) {
		return nil, fmt.Errorf("invalid session file")
	}

	stat, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var header Header
	headerOK := false
	messageCount := 0
	firstMessage := ""

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		switch peekLineType(line) {
		case "session":
			if !headerOK {
				if json.Unmarshal([]byte(line), &header) == nil && header.Type == "session" && header.ID != "" {
					headerOK = true
				}
			}
		case "message":
			messageCount++
			if firstMessage == "" && (strings.Contains(line, `"role":"user"`) || strings.Contains(line, `"role": "user"`)) {
				var entry MessageEntry
				if json.Unmarshal([]byte(line), &entry) == nil {
					text := strings.TrimSpace(opencode.ContentString(entry.Message))
					if text != "" {
						firstMessage = text
					}
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if !headerOK {
		return nil, fmt.Errorf("missing session header")
	}

	created := stat.ModTime()
	if t, err := time.Parse(time.RFC3339Nano, header.Timestamp); err == nil {
		created = t
	}

	if firstMessage == "" {
		firstMessage = "(no messages)"
	}

	return &Info{
		Path:         path,
		ID:           header.ID,
		CWD:          header.CWD,
		Modified:     stat.ModTime(),
		Created:      created,
		MessageCount: messageCount,
		FirstMessage: firstMessage,
	}, nil
}

func peekLineType(line string) string {
	if strings.Contains(line, `"type":"session"`) || strings.Contains(line, `"type": "session"`) {
		return "session"
	}
	if strings.Contains(line, `"type":"message"`) || strings.Contains(line, `"type": "message"`) {
		return "message"
	}
	return ""
}

func FormatRelative(t time.Time) string {
	diff := time.Since(t)
	switch {
	case diff < time.Minute:
		return "now"
	case diff < time.Hour:
		return fmt.Sprintf("%dm", int(diff.Minutes()))
	case diff < 24*time.Hour:
		return fmt.Sprintf("%dh", int(diff.Hours()))
	case diff < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(diff.Hours()/24))
	case diff < 30*24*time.Hour:
		return fmt.Sprintf("%dw", int(diff.Hours()/24/7))
	case diff < 365*24*time.Hour:
		return fmt.Sprintf("%dmo", int(diff.Hours()/24/30))
	default:
		return fmt.Sprintf("%dy", int(diff.Hours()/24/365))
	}
}

func ShortenPath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || path == "" {
		return path
	}
	if strings.HasPrefix(path, home) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}
