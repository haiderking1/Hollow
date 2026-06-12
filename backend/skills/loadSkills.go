package skills

import (
	"path/filepath"
	"strings"

	"github.com/enough/enough/backend/config"
)

type LoadSkillsOptions struct {
	Cwd             string
	AgentDir        string // ~/.enough/agent
	SkillPaths      []string
	IncludeDefaults bool
}

type LoadSkillsResult struct {
	Skills      []Skill
	Diagnostics []string
}

func LoadSkills(opts LoadSkillsOptions) LoadSkillsResult {
	cfg := config.Runtime{
		Skills: config.SkillsSettings{
			Enabled:             true,
			EnableSkillCommands: true,
			Paths:               opts.SkillPaths,
		},
	}

	var dirs []SearchDir
	if opts.IncludeDefaults {
		dirs = SearchLocations(opts.Cwd, cfg, opts.AgentDir)
	} else {
		seen := make(map[string]bool)
		for _, p := range opts.SkillPaths {
			if strings.HasPrefix(p, "!") {
				continue
			}
			abs, err := filepath.Abs(p)
			if err != nil {
				abs = filepath.Clean(p)
			}
			if seen[abs] {
				continue
			}
			seen[abs] = true
			dirs = append(dirs, SearchDir{
				Path:          abs,
				Source:        "path",
				IncludeRootMD: true,
			})
		}
	}

	skillsList, diags := LoadSkillsFromDirs(opts.Cwd, dirs, cfg)
	return LoadSkillsResult{
		Skills:      skillsList,
		Diagnostics: diags,
	}
}
