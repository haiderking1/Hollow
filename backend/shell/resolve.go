package shell

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/enough/enough/backend/config"
	"github.com/enough/enough/backend/enoughhome"
)

// ResolveBash returns the path to the resolved bash executable.
// It follows the resolution order specified in B1 of the handoff spec.
func ResolveBash() (string, error) {
	if runtime.GOOS != "windows" {
		// On non-Windows, look up bash on PATH or fall back to standard locations
		if p, err := exec.LookPath("bash"); err == nil {
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
				return p, nil
			}
		}
		for _, candidate := range []string{"/usr/bin/bash", "/bin/bash"} {
			if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
				return candidate, nil
			}
		}
		if shellEnv := os.Getenv("SHELL"); shellEnv != "" {
			if fi, err := os.Stat(shellEnv); err == nil && !fi.IsDir() {
				return shellEnv, nil
			}
		}
		return "/bin/sh", nil
	}

	// Windows resolution order
	localAppData := os.Getenv("LOCALAPPDATA")

	// 1. shell_path in ~/.enough/config.json
	if cfg, err := config.Load(); err == nil && cfg.ShellPath != "" {
		if fi, err := os.Stat(cfg.ShellPath); err == nil && !fi.IsDir() {
			return cfg.ShellPath, nil
		}
	}

	// 2. ENOUGH_GIT_BASH_PATH env
	if custom := os.Getenv("ENOUGH_GIT_BASH_PATH"); custom != "" {
		if fi, err := os.Stat(custom); err == nil && !fi.IsDir() {
			return custom, nil
		}
	}

	// 3. %LOCALAPPDATA%\enough\git\bin\bash.exe
	// 4. %LOCALAPPDATA%\enough\git\usr\bin\bash.exe
	pGitDir := enoughhome.PortableGitDir()
	candidates := []string{
		filepath.Join(pGitDir, "bin", "bash.exe"),
		filepath.Join(pGitDir, "usr", "bin", "bash.exe"),
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c, nil
		}
	}

	// 5. exec.LookPath("bash") / where bash.exe (and verify file exists)
	if p, err := exec.LookPath("bash"); err == nil {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, nil
		}
	}

	// 6. %ProgramFiles%\Git\bin\bash.exe
	// 7. %ProgramFiles(x86)%\Git\bin\bash.exe
	// 8. %LOCALAPPDATA%\Programs\Git\bin\bash.exe
	var systemCandidates []string
	if pf := os.Getenv("ProgramFiles"); pf != "" {
		systemCandidates = append(systemCandidates, filepath.Join(pf, "Git", "bin", "bash.exe"))
	} else {
		systemCandidates = append(systemCandidates, `C:\Program Files\Git\bin\bash.exe`)
	}
	if pf86 := os.Getenv("ProgramFiles(x86)"); pf86 != "" {
		systemCandidates = append(systemCandidates, filepath.Join(pf86, "Git", "bin", "bash.exe"))
	} else {
		systemCandidates = append(systemCandidates, `C:\Program Files (x86)\Git\bin\bash.exe`)
	}
	if localAppData != "" {
		systemCandidates = append(systemCandidates, filepath.Join(localAppData, "Programs", "Git", "bin", "bash.exe"))
	}
	for _, c := range systemCandidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c, nil
		}
	}

	// 9. Derive from git.exe on PATH
	if gitPath, err := exec.LookPath("git"); err == nil {
		if absGitPath, err := filepath.Abs(gitPath); err == nil {
			parent := filepath.Dir(absGitPath)
			gitRoot := filepath.Dir(parent)
			derivedCandidates := []string{
				filepath.Join(gitRoot, "bin", "bash.exe"),
				filepath.Join(gitRoot, "usr", "bin", "bash.exe"),
			}
			for _, c := range derivedCandidates {
				if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
					return c, nil
				}
			}
		}
	}

	return "", errors.New("Git Bash not found. Enough requires Git for Windows on Windows.\n" +
		"Install it using the following command in PowerShell:\n" +
		"  irm https://raw.githubusercontent.com/haiderking1/Enough/main/scripts/install-windows.ps1 | iex\n" +
		"Or set the ENOUGH_GIT_BASH_PATH environment variable.")
}
