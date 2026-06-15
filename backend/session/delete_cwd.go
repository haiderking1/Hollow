package session

import "path/filepath"

// DeleteForCWD removes all session JSONL files for a project directory.
// skipPath, if non-empty, is left on disk (e.g. the active session).
func DeleteForCWD(cwd string, skipPath string) (int, error) {
	infos, err := ListForCWD(cwd)
	if err != nil {
		return 0, err
	}

	skipPath = filepath.Clean(skipPath)
	deleted := 0
	for _, info := range infos {
		if skipPath != "" && filepath.Clean(info.Path) == skipPath {
			continue
		}
		if _, err := Delete(info.Path); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}
