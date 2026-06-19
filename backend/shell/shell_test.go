package shell

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMsysToWindowsPath(t *testing.T) {
	// 1. POSIX no-op
	if res := msysToWindowsPathInternal("/home/idk/foo", false); res != "/home/idk/foo" {
		t.Errorf("Expected '/home/idk/foo', got %q", res)
	}
	if res := msysToWindowsPathInternal("/c/Users/foo", false); res != "/c/Users/foo" {
		t.Errorf("Expected '/c/Users/foo', got %q", res)
	}

	// 2. Empty string preserved
	if res := msysToWindowsPathInternal("", true); res != "" {
		t.Errorf("Expected empty string, got %q", res)
	}

	// 3. Windows translation
	tests := []struct {
		input    string
		expected string
	}{
		{"/c/Users/foo", `C:\Users\foo`},
		{"/C/Users/foo", `C:\Users\foo`},
		{"/cygdrive/d/data", `D:\data`},
		{"/mnt/c/Users", `C:\Users`},
		{`C:\Users\foo`, `C:\Users\foo`},
		{"C:/Users/foo", "C:/Users/foo"},
		{"/c", `C:\`},
		{"/c/", `C:\`},
	}

	for _, tc := range tests {
		res := msysToWindowsPathInternal(tc.input, true)
		if res != tc.expected {
			t.Errorf("MsysToWindowsPath(%q) = %q, expected %q", tc.input, res, tc.expected)
		}
	}
}

func TestResolveSafeCwd(t *testing.T) {
	// Create a temp directory tree
	tmp, err := os.MkdirTemp("", "enough-cwd-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmp)

	child := filepath.Join(tmp, "child")
	if err := os.Mkdir(child, 0755); err != nil {
		t.Fatal(err)
	}

	nonexistent := filepath.Join(child, "nonexistent")

	// 1. Existing CWD
	if res := ResolveSafeCwd(child); res != child {
		t.Errorf("Expected existing dir %q, got %q", child, res)
	}

	// 2. Non-existent child falls back to closest existing ancestor (child)
	if res := ResolveSafeCwd(nonexistent); res != child {
		t.Errorf("Expected resolved ancestor %q, got %q", child, res)
	}

	// 3. Root fallback (should fall back to os.TempDir())
	nonexistentRoot := filepath.Join(os.TempDir(), "nonexistent-root-dir-xyz")
	if runtime.GOOS == "windows" {
		nonexistentRoot = `Z:\nonexistent\path\xyz`
	}
	res := ResolveSafeCwd(nonexistentRoot)
	if res == "" {
		t.Errorf("Expected non-empty TempDir fallback, got %q", res)
	}
}

func TestResolveBashPriority(t *testing.T) {
	// Set ENOUGH_GIT_BASH_PATH to a temp file and verify priority 2 works
	tmpFile, err := os.CreateTemp("", "enough-bash-*.exe")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	_ = tmpFile.Close()

	if runtime.GOOS == "windows" {
		oldEnv := os.Getenv("ENOUGH_GIT_BASH_PATH")
		defer os.Setenv("ENOUGH_GIT_BASH_PATH", oldEnv)

		os.Setenv("ENOUGH_GIT_BASH_PATH", tmpFile.Name())
		resolved, err := ResolveBash()
		if err != nil {
			t.Fatal(err)
		}
		if resolved != tmpFile.Name() {
			t.Errorf("Expected resolved %q, got %q", tmpFile.Name(), resolved)
		}
	}
}

func TestCommandContext(t *testing.T) {
	ctx := context.Background()
	cmd, err := CommandContext(ctx, "echo hello", false)
	if err != nil {
		// If bash isn't resolved (e.g. on Windows without Git Bash), we expect an error or resolved command
		if runtime.GOOS == "windows" {
			t.Logf("ResolveBash failed or succeeded: %v", err)
			return
		}
		t.Fatal(err)
	}

	if cmd == nil {
		t.Fatal("Command is nil")
	}

	// Verify environment variables appended
	foundTerm := false
	for _, env := range cmd.Env {
		if env == "TERM=dumb" {
			foundTerm = true
			break
		}
	}
	if !foundTerm {
		t.Error("Expected TERM=dumb env variable in command")
	}
}

func TestGitRootFromBashExe(t *testing.T) {
	var tests []struct {
		bashPath string
		expected string
	}
	if runtime.GOOS == "windows" {
		tests = []struct {
			bashPath string
			expected string
		}{
			{`C:\pgit\bin\bash.exe`, `C:\pgit`},
			{`C:\pgit\usr\bin\bash.exe`, `C:\pgit`},
			{`D:\Git\bin\bash.exe`, `D:\Git`},
		}
	} else {
		tests = []struct {
			bashPath string
			expected string
		}{
			{`/pgit/bin/bash.exe`, `/pgit`},
			{`/pgit/usr/bin/bash.exe`, `/pgit`},
			{`/Git/bin/bash.exe`, `/Git`},
		}
	}

	for _, tc := range tests {
		got := gitRootFromBashExe(tc.bashPath)
		if filepath.Clean(got) != filepath.Clean(tc.expected) {
			t.Errorf("gitRootFromBashExe(%q) = %q, expected %q", tc.bashPath, got, tc.expected)
		}
	}
}

func TestResolveBashShellPathConfig(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only test")
	}

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")

	// Create a fake shell_path file to bypass os.Stat checks
	fakeExe := filepath.Join(dir, "fake_shell.exe")
	if err := os.WriteFile(fakeExe, []byte("echo"), 0755); err != nil {
		t.Fatal(err)
	}

	// Write config {"shell_path": "<fakeExe>"}
	cfgContent := `{"shell_path": ` + jsonString(fakeExe) + `}`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Mock environment
	t.Setenv("ENOUGH_HOME", dir)

	otherExe := filepath.Join(dir, "other_shell.exe")
	if err := os.WriteFile(otherExe, []byte("echo"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ENOUGH_GIT_BASH_PATH", otherExe)

	resolved, err := ResolveBash()
	if err != nil {
		t.Fatal(err)
	}

	if resolved != fakeExe {
		t.Errorf("Expected resolved %q (from config), got %q", fakeExe, resolved)
	}
}

func TestResolveBashPortableGitDir(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-only test")
	}

	dir := t.TempDir()
	// Mock LOCALAPPDATA to point to our temp dir
	t.Setenv("LOCALAPPDATA", dir)

	pGitDir := filepath.Join(dir, "enough", "git")
	bashPath := filepath.Join(pGitDir, "bin", "bash.exe")
	if err := os.MkdirAll(filepath.Dir(bashPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bashPath, []byte("echo"), 0755); err != nil {
		t.Fatal(err)
	}

	// We must ensure shell_path config and ENOUGH_GIT_BASH_PATH are NOT set
	t.Setenv("ENOUGH_HOME", filepath.Join(dir, "nonexistent-enough-home"))
	t.Setenv("ENOUGH_GIT_BASH_PATH", "")

	resolved, err := ResolveBash()
	if err != nil {
		t.Fatal(err)
	}

	if resolved != bashPath {
		t.Errorf("Expected resolved %q (from PortableGitDir), got %q", bashPath, resolved)
	}
}

func jsonString(s string) string {
	return `"` + strings.ReplaceAll(s, `\`, `\\`) + `"`
}
