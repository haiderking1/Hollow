package enoughhome

import (
	"os"
	"path/filepath"
)

// PortableGitDir returns the path where PortableGit is provisioned on Windows.
func PortableGitDir() string {
	if la := os.Getenv("LOCALAPPDATA"); la != "" {
		return filepath.Join(la, "enough", "git")
	}
	return filepath.Join(HomeDir(), "git") // fallback
}
