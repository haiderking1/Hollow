package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type scriptCandidate struct {
	path    string
	modTime int64
}

// FindLatestScript returns the most recently modified workflow.js under workDir/.enough/workflows/.
func FindLatestScript(workDir string) (string, error) {
	root := filepath.Join(workDir, ".enough", "workflows")
	matches, err := filepath.Glob(filepath.Join(root, "*", "workflow.js"))
	if err != nil {
		return "", err
	}
	var candidates []scriptCandidate
	for _, path := range matches {
		info, statErr := os.Stat(path)
		if statErr != nil || info.IsDir() {
			continue
		}
		candidates = append(candidates, scriptCandidate{path: path, modTime: info.ModTime().UnixNano()})
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("no workflow scripts found under %s", root)
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].modTime > candidates[j].modTime
	})
	return candidates[0].path, nil
}

// ResolveScriptPath resolves a workflow script path relative to workDir when needed.
func ResolveScriptPath(workDir, arg string) (string, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return "", fmt.Errorf("empty workflow path")
	}
	path := arg
	if !filepath.IsAbs(path) {
		path = filepath.Join(workDir, path)
	}
	path = filepath.Clean(path)
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		path = filepath.Join(path, "workflow.js")
		info, err = os.Stat(path)
		if err != nil {
			return "", err
		}
	}
	if info.IsDir() {
		return "", fmt.Errorf("workflow path is a directory: %s", path)
	}
	return path, nil
}
