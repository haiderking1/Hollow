package approval

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/enough/enough/backend/config"
	"github.com/enough/enough/backend/enoughhome"
)

const (
	SubsystemMemory = "memory"
	SubsystemSkills = "skills"
)

type PendingRecord struct {
	ID        string                 `json:"id"`
	Subsystem string                 `json:"subsystem"`
	Action    string                 `json:"action"`
	Summary   string                 `json:"summary"`
	Origin    string                 `json:"origin"`
	CreatedAt float64                `json:"created_at"`
	Payload   map[string]interface{} `json:"payload"`
}

func PendingDir(subsystem string) string {
	return filepath.Join(enoughhome.HomeDir(), "pending", subsystem)
}

func generateID() string {
	bytes := make([]byte, 4)
	_, _ = rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func StageWrite(subsystem string, payload map[string]interface{}, summary string, origin string) (PendingRecord, error) {
	pid := generateID()
	record := PendingRecord{
		ID:        pid,
		Subsystem: subsystem,
		Action:    getStringField(payload, "action"),
		Summary:   strings.TrimSpace(summary),
		Origin:    origin,
		CreatedAt: float64(time.Now().UnixNano()) / 1e9,
		Payload:   payload,
	}

	d := PendingDir(subsystem)
	if err := os.MkdirAll(d, 0o700); err != nil {
		return record, err
	}

	path := filepath.Join(d, pid+".json")
	tmpPath := path + ".tmp"

	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return record, err
	}

	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return record, err
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return record, err
	}

	return record, nil
}

func ListPending(subsystem string) ([]PendingRecord, error) {
	d := PendingDir(subsystem)
	if _, err := os.Stat(d); os.IsNotExist(err) {
		return nil, nil
	}

	entries, err := os.ReadDir(d)
	if err != nil {
		return nil, err
	}

	var records []PendingRecord
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(d, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var r PendingRecord
		if err := json.Unmarshal(data, &r); err == nil {
			records = append(records, r)
		}
	}

	sort.Slice(records, func(i, j int) bool {
		return records[i].CreatedAt < records[j].CreatedAt
	})

	return records, nil
}

func GetPending(subsystem string, pendingID string) (*PendingRecord, error) {
	path := filepath.Join(PendingDir(subsystem), pendingID+".json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r PendingRecord
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func DiscardPending(subsystem string, pendingID string) (bool, error) {
	path := filepath.Join(PendingDir(subsystem), pendingID+".json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return false, nil
	}
	if err := os.Remove(path); err != nil {
		return false, err
	}
	return true, nil
}

func PendingCount(subsystem string) int {
	d := PendingDir(subsystem)
	entries, err := os.ReadDir(d)
	if err != nil {
		return 0
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			count++
		}
	}
	return count
}

func WriteApprovalEnabled(subsystem string, cfg config.Runtime) bool {
	if subsystem == SubsystemSkills {
		return cfg.Skills.WriteApproval
	}
	if subsystem == SubsystemMemory {
		return cfg.Memory.WriteApproval
	}
	return false
}

type GateResult struct {
	Allow   bool
	Blocked bool
	Stage   bool
	Message string
}

func EvaluateGate(subsystem string, isBackground bool, cfg config.Runtime) GateResult {
	if !WriteApprovalEnabled(subsystem, cfg) {
		return GateResult{Allow: true}
	}

	if subsystem == SubsystemSkills || isBackground {
		where := "/skills pending"
		if subsystem == SubsystemMemory {
			where = "/memory pending"
		}
		return GateResult{
			Stage: true,
			Message: fmt.Sprintf("Staged for approval (%s.write_approval is on). Not yet saved — review with %s.", subsystem, where),
		}
	}

	// For memory + foreground in TUI, we don't do inline CLI prompts in Go TUI,
	// so we stage it.
	return GateResult{
		Stage: true,
		Message: "Staged for approval (memory.write_approval is on). Not yet saved — review with /memory pending.",
	}
}

func SkillGist(action, name, content, filePath, oldString, newString string) string {
	if (action == "create" || action == "edit") && content != "" {
		desc := extractDescriptionQuick(content)
		size := ""
		if len(content) >= 1024 {
			size = fmt.Sprintf("%d KB", len(content)/1024+1)
		} else {
			size = fmt.Sprintf("%d chars", len(content))
		}
		verb := "create"
		if action == "edit" {
			verb = "rewrite"
		}
		if desc != "" {
			return fmt.Sprintf("%s '%s' — %s (%s)", verb, name, desc, size)
		}
		return fmt.Sprintf("%s '%s' (%s)", verb, name, size)
	}
	if action == "patch" {
		target := filePath
		if target == "" {
			target = "SKILL.md"
		}
		removed := strings.Count(oldString, "\n")
		if oldString != "" {
			removed++
		}
		added := strings.Count(newString, "\n")
		if newString != "" {
			added++
		}
		return fmt.Sprintf("patch '%s' %s (+%d/-%d lines)", name, target, added, removed)
	}
	if action == "write_file" {
		return fmt.Sprintf("write %s in '%s'", filePath, name)
	}
	if action == "remove_file" {
		return fmt.Sprintf("remove %s from '%s'", filePath, name)
	}
	if action == "delete" {
		return fmt.Sprintf("delete skill '%s'", name)
	}
	return fmt.Sprintf("%s '%s'", action, name)
}

func extractDescriptionQuick(content string) string {
	re := regexp.MustCompile(`(?m)^description:\s*(.+)$`)
	m := re.FindStringSubmatch(content)
	if len(m) < 2 {
		return ""
	}
	desc := strings.TrimSpace(m[1])
	desc = strings.Trim(desc, `'"`)
	if len(desc) > 140 {
		desc = desc[:137] + "..."
	}
	return desc
}

func SkillPendingDiff(record PendingRecord) string {
	payload := record.Payload
	action := getStringField(payload, "action")
	name := getStringField(payload, "name")

	if action == "create" {
		return getStringField(payload, "content")
	}

	// Diff logic
	current := ""
	targetLabel := "SKILL.md"

	// Resolve local skill path
	home := enoughhome.HomeDir()
	skillsRoot := filepath.Join(home, "skills")
	// Search in skills directories
	skillDir := ""
	_ = filepath.Walk(skillsRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() && info.Name() == name {
			skillDir = path
			return io.EOF // stop walking
		}
		return nil
	})

	if skillDir != "" {
		p := filepath.Join(skillDir, "SKILL.md")
		if action == "edit" {
			p = filepath.Join(skillDir, "SKILL.md")
		} else if action == "patch" || action == "write_file" {
			rel := getStringField(payload, "file_path")
			if rel == "" {
				rel = "SKILL.md"
			}
			p = filepath.Join(skillDir, rel)
			targetLabel = rel
		}

		if data, err := os.ReadFile(p); err == nil {
			current = string(data)
		}
	}

	newContent := ""
	if action == "edit" {
		newContent = getStringField(payload, "content")
	} else if action == "patch" {
		oldS := getStringField(payload, "old_string")
		newS := getStringField(payload, "new_string")
		if current != "" {
			newContent = strings.ReplaceAll(current, oldS, newS)
		} else {
			newContent = fmt.Sprintf("(patch %q → %q)", oldS, newS)
		}
	} else if action == "write_file" {
		newContent = getStringField(payload, "file_content")
	} else if action == "remove_file" {
		return fmt.Sprintf("remove file: %s from skill '%s'", getStringField(payload, "file_path"), name)
	} else if action == "delete" {
		return fmt.Sprintf("delete skill '%s'", name)
	} else {
		return fmt.Sprintf("(%s on '%s')", action, name)
	}

	return diffLines(current, newContent, targetLabel)
}

func diffLines(orig, newText, label string) string {
	origLines := strings.Split(orig, "\n")
	newLines := strings.Split(newText, "\n")

	// Standard simple diff generator since Go doesn't have difflib in std
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("--- a/%s\n+++ b/%s\n", label, label))

	// Extremely simple line diff algorithm (LCS-based is better, but this is a helper)
	// Let's do a basic comparison
	maxL := len(origLines)
	if len(newLines) > maxL {
		maxL = len(newLines)
	}

	for i := 0; i < maxL; i++ {
		if i < len(origLines) && i < len(newLines) {
			if origLines[i] == newLines[i] {
				// unchanged line
				// we can print it, or skip to save space
			} else {
				sb.WriteString(fmt.Sprintf("-%s\n+%s\n", origLines[i], newLines[i]))
			}
		} else if i < len(origLines) {
			sb.WriteString(fmt.Sprintf("-%s\n", origLines[i]))
		} else if i < len(newLines) {
			sb.WriteString(fmt.Sprintf("+%s\n", newLines[i]))
		}
	}

	return sb.String()
}

func getStringField(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	if val, ok := m[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}
