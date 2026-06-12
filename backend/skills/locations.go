package skills

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/enough/enough/backend/config"
	"github.com/enough/enough/backend/enoughhome"
)

type SearchDir struct {
	Path          string
	Source        string // "project" | "user" | "path" | "flame"
	IncludeRootMD bool   // false for .agents/skills only
}

func isBlockedSkillRoot(path string) bool {
	clean := filepath.Clean(path)
	return strings.Contains(clean, filepath.Join(".cursor", "skills-cursor"))
}

func hasExcludedComponent(path string) bool {
	clean := filepath.Clean(path)
	parts := strings.Split(clean, string(filepath.Separator))
	for _, part := range parts {
		if ExcludedSkillDirs[part] {
			return true
		}
	}
	return false
}

// SearchLocations returns every directory to scan for skills, in STRICT
// precedence order (first match wins on name collision).
func SearchLocations(workDir string, cfg config.Runtime, agentDirOverride string) []SearchDir {
	var dirs []SearchDir
	seen := make(map[string]bool)

	addDir := func(path, source string, includeRootMD bool) {
		abs, err := filepath.Abs(path)
		if err != nil {
			abs = filepath.Clean(path)
		}
		if seen[abs] {
			return
		}
		if isBlockedSkillRoot(abs) || hasExcludedComponent(abs) {
			return
		}
		seen[abs] = true
		dirs = append(dirs, SearchDir{
			Path:          abs,
			Source:        source,
			IncludeRootMD: includeRootMD,
		})
	}

	// 1 & 2. Project: cwd→gitRoot (or FS root if no git repo)
	current := workDir
	gitRoot := ""
	for {
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
			gitRoot = current
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}

	current = workDir
	for {
		// .enough/skills
		addDir(filepath.Join(current, ".enough", "skills"), "project", true)
		// .agents/skills
		addDir(filepath.Join(current, ".agents", "skills"), "project", false)

		if gitRoot != "" && current == gitRoot {
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}

	// cwd .cursor/skills
	addDir(filepath.Join(workDir, ".cursor", "skills"), "project", true)

	// 3. Global user: ~/.enough/skills, ~/.enough/agent/skills, ~/.agents/skills, ~/.cursor/skills
	home := enoughhome.HomeDir()
	addDir(filepath.Join(home, "skills"), "user", true)
	if agentDirOverride != "" {
		addDir(filepath.Join(agentDirOverride, "skills"), "user", true)
	} else {
		addDir(filepath.Join(home, "agent", "skills"), "user", true)
	}

	userHome, err := os.UserHomeDir()
	if err == nil {
		addDir(filepath.Join(userHome, ".agents", "skills"), "user", false)
		addDir(filepath.Join(userHome, ".cursor", "skills"), "user", true)
	}

	// 4. Optional read-only: ~/.flame/skills (if exists)
	if userHome != "" {
		flameSkills := filepath.Join(userHome, ".flame", "skills")
		if fi, err := os.Stat(flameSkills); err == nil && fi.IsDir() {
			addDir(flameSkills, "flame", true)
		}
	}

	// 5. Config explicit paths (cfg.Skills.Paths, minus ! exclusions)
	for _, p := range cfg.Skills.Paths {
		if strings.HasPrefix(p, "!") {
			continue
		}
		absPath := p
		if !filepath.IsAbs(absPath) {
			if strings.HasPrefix(absPath, "~") && userHome != "" {
				absPath = filepath.Join(userHome, absPath[1:])
			} else {
				absPath = filepath.Join(workDir, absPath)
			}
		}
		addDir(absPath, "path", true)
	}

	// 6. External directories from config (Hermes semantics)
	for _, extDir := range getExternalSkillsDirs(cfg) {
		addDir(extDir, "user", true)
	}

	return dirs
}

func getExternalSkillsDirs(cfg config.Runtime) []string {
	userHome, _ := os.UserHomeDir()
	home := enoughhome.HomeDir()
	localSkills, _ := filepath.Abs(filepath.Join(home, "skills"))

	var out []string
	seen := make(map[string]bool)

	for _, entry := range cfg.Skills.ExternalDirs {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		// Expand env variables
		expanded := os.ExpandEnv(entry)
		// Expand ~
		if strings.HasPrefix(expanded, "~") && userHome != "" {
			expanded = filepath.Join(userHome, expanded[1:])
		}

		// Resolve relative paths against ENOUGH_HOME (home)
		abs := expanded
		if !filepath.IsAbs(abs) {
			abs = filepath.Join(home, abs)
		}
		abs, err := filepath.Abs(abs)
		if err != nil {
			abs = filepath.Clean(abs)
		}

		if abs == localSkills {
			continue
		}
		if seen[abs] {
			continue
		}

		fi, err := os.Stat(abs)
		if err == nil && fi.IsDir() {
			seen[abs] = true
			out = append(out, abs)
		}
	}
	return out
}
