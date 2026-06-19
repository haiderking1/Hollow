package workflow

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/enough/enough/backend/enoughhome"
)

type SavedWorkflow struct {
	Name    string
	Path    string
	Project bool
	Meta    Meta
}

var savedNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

func ScanSaved(workDir string) []SavedWorkflow {
	byName := map[string]SavedWorkflow{}
	homeRoot := filepath.Join(enoughhome.HomeDir(), "workflows", "saved")
	scanSavedRoot(homeRoot, false, byName)
	scanSavedRoot(filepath.Join(workDir, ".enough", "workflows", "saved"), true, byName)
	out := make([]SavedWorkflow, 0, len(byName))
	for _, item := range byName {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func scanSavedRoot(root string, project bool, out map[string]SavedWorkflow) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		path := filepath.Join(root, name, "workflow.js")
		if _, err := os.Stat(path); err != nil {
			continue
		}
		item := SavedWorkflow{Name: name, Path: path, Project: project}
		if data, err := os.ReadFile(filepath.Join(root, name, "meta.json")); err == nil {
			_ = json.Unmarshal(data, &item.Meta)
		}
		out[name] = item
	}
}

func SaveWorkflow(scriptPath, name, workDir string, project bool) (SavedWorkflow, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if !savedNamePattern.MatchString(name) {
		return SavedWorkflow{}, errors.New("workflow name must use lowercase letters, digits, and hyphens")
	}
	data, err := os.ReadFile(scriptPath)
	if err != nil {
		return SavedWorkflow{}, err
	}
	root := filepath.Join(enoughhome.HomeDir(), "workflows", "saved")
	if project {
		root = filepath.Join(workDir, ".enough", "workflows", "saved")
	}
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return SavedWorkflow{}, err
	}
	dst := filepath.Join(dir, "workflow.js")
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return SavedWorkflow{}, err
	}
	meta, _ := Inspect(scriptPath)
	meta.Name = name
	metaData, _ := json.MarshalIndent(meta, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), metaData, 0o644); err != nil {
		return SavedWorkflow{}, err
	}
	return SavedWorkflow{Name: name, Path: dst, Project: project, Meta: meta}, nil
}
