package agent

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/enough/enough/backend/config"
)

var projectMarkers = []string{
	"pyproject.toml", "setup.py", "setup.cfg", "requirements.txt",
	"package.json", "tsconfig.json", "deno.json",
	"Cargo.toml", "go.mod", "pom.xml", "build.gradle", "build.gradle.kts",
	"Gemfile", "composer.json", "mix.exs", "pubspec.yaml",
	"CMakeLists.txt", "Makefile", "Dockerfile",
	"AGENTS.md", "CLAUDE.md", ".cursorrules",
}

var interactiveCodingPlatforms = map[string]bool{
	"cli":     true,
	"tui":     true,
	"acp":     true,
	"desktop": true,
	"":        true,
}

var NonCodingCategories = map[string]bool{
	"apple":         true,
	"communication": true,
	"cooking":       true,
	"creative":      true,
	"email":         true,
	"finance":       true,
	"gaming":        true,
	"gifs":          true,
	"health":        true,
	"media":         true,
	"music":         true,
	"note-taking":   true,
	"productivity":  true,
	"shopping":      true,
	"smart-home":    true,
	"social-media":  true,
	"travel":        true,
	"yuanbao":       true,
}

func isGitRoot(dir string) bool {
	gitDir := filepath.Join(dir, ".git")
	fi, err := os.Stat(gitDir)
	return err == nil && fi.IsDir()
}

func findGitRoot(cwd string) string {
	curr := filepath.Clean(cwd)
	for {
		if isGitRoot(curr) {
			return curr
		}
		parent := filepath.Dir(curr)
		if parent == curr {
			break
		}
		curr = parent
	}
	return ""
}

func findMarkerRoot(cwd string) string {
	curr := filepath.Clean(cwd)
	home, _ := os.UserHomeDir()
	for depth := 0; depth <= 6; depth++ {
		if curr == home {
			break
		}
		for _, marker := range projectMarkers {
			if _, err := os.Stat(filepath.Join(curr, marker)); err == nil {
				return curr
			}
		}
		parent := filepath.Dir(curr)
		if parent == curr {
			break
		}
		curr = parent
	}
	return ""
}

func resolvePlatform() string {
	if p := os.Getenv("ENOUGH_PLATFORM"); p != "" {
		return p
	}
	if p := os.Getenv("HERMES_PLATFORM"); p != "" {
		return p
	}
	if p := os.Getenv("ENOUGH_SESSION_PLATFORM"); p != "" {
		return p
	}
	if p := os.Getenv("HERMES_SESSION_PLATFORM"); p != "" {
		return p
	}
	return "cli"
}

// DetectIsCoding reports whether the agent is currently in a coding context.
func DetectIsCoding(workDir string, cfg config.Runtime) bool {
	mode := strings.TrimSpace(strings.ToLower(cfg.Agent.CodingContext))
	if mode == "" {
		mode = "auto"
	}
	if mode == "off" || mode == "false" || mode == "never" {
		return false
	}
	if mode == "on" || mode == "true" || mode == "always" {
		return true
	}

	platform := resolvePlatform()
	if !interactiveCodingPlatforms[strings.ToLower(platform)] {
		return false
	}

	home, _ := os.UserHomeDir()
	gitRoot := findGitRoot(workDir)
	if gitRoot != "" && gitRoot == home {
		gitRoot = ""
	}

	if gitRoot != "" || findMarkerRoot(workDir) != "" {
		return true
	}

	return false
}

// ShouldDemoteCategory reports whether a skill category should be demoted to names-only format.
func ShouldDemoteCategory(cat string, isCoding bool, configMode string) bool {
	if !isCoding || configMode != "focus" {
		return false
	}
	parts := strings.SplitN(cat, "/", 2)
	return NonCodingCategories[parts[0]]
}
