package session

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// DeleteResult reports how a session file was removed.
type DeleteResult struct {
	Method string // "trash" or "unlink"
}

// Delete removes a session JSONL file, trying the trash CLI first like Flame.
func Delete(path string) (DeleteResult, error) {
	path = filepath.Clean(path)
	if path == "" {
		return DeleteResult{}, errors.New("empty session path")
	}
	if _, err := os.Stat(path); err != nil {
		return DeleteResult{}, err
	}

	args := []string{path}
	if strings.HasPrefix(filepath.Base(path), "-") {
		args = []string{"--", path}
	}

	trashErr := exec.Command("trash", args...).Run()
	if trashErr == nil {
		return DeleteResult{Method: "trash"}, nil
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return DeleteResult{Method: "trash"}, nil
	}

	if err := os.Remove(path); err != nil {
		if trashErr != nil {
			return DeleteResult{Method: "unlink"}, fmt.Errorf("%w (trash: %v)", err, trashErr)
		}
		return DeleteResult{Method: "unlink"}, err
	}
	return DeleteResult{Method: "unlink"}, nil
}
