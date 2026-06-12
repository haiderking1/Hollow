package skills

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/enough/enough/backend/config"
)

var skillInvalidChars = regexp.MustCompile(`[^a-z0-9-]`)
var skillMultiHyphen = regexp.MustCompile(`-{2,}`)

func SkillNameToSlashSlug(name string) string {
	cmd := strings.ToLower(name)
	cmd = strings.ReplaceAll(cmd, " ", "-")
	cmd = strings.ReplaceAll(cmd, "_", "-")
	cmd = skillInvalidChars.ReplaceAllString(cmd, "")
	cmd = skillMultiHyphen.ReplaceAllString(cmd, "-")
	cmd = strings.Trim(cmd, "-")
	return cmd
}

func BuildSkillInvocationMessage(loadedSkill map[string]interface{}, skillDir, userInstruction, sessionId string, cfg config.Runtime) string {
	name, _ := loadedSkill["name"].(string)
	if name == "" {
		name = "skill"
	}

	activationNote := fmt.Sprintf("[IMPORTANT: The user has invoked the %q skill. Follow the skill instructions below as your primary guidance for this turn.]", name)
	content, _ := loadedSkill["content"].(string)

	// Preprocess content
	content = PreprocessSkillContent(content, skillDir, sessionId, cfg.Skills.InlineShell, cfg.Skills.InlineShellTimeout)

	var parts []string
	parts = append(parts, activationNote, "", strings.TrimSpace(content))

	if skillDir != "" {
		parts = append(parts, "", fmt.Sprintf("[Skill directory: %s]", skillDir),
			"Resolve any relative paths in this skill (e.g. `scripts/foo.js`, `templates/config.yaml`) against that directory, then run them with the terminal tool using the absolute path.")
	}

	var supporting []string
	if linkedFilesVal, ok := loadedSkill["linked_files"]; ok && linkedFilesVal != nil {
		if linkedMap, ok := linkedFilesVal.(map[string][]string); ok {
			for _, entries := range linkedMap {
				supporting = append(supporting, entries...)
			}
		}
	}

	if len(supporting) == 0 && skillDir != "" {
		if _, err := os.Stat(skillDir); err == nil {
			for _, subdir := range []string{"references", "templates", "scripts", "assets"} {
				subdirPath := filepath.Join(skillDir, subdir)
				if _, err := os.Stat(subdirPath); err == nil {
					_ = filepath.Walk(subdirPath, func(path string, info os.FileInfo, err error) error {
						if err != nil {
							return nil
						}
						if !info.IsDir() {
							rel, err := filepath.Rel(skillDir, path)
							if err == nil {
								supporting = append(supporting, filepath.ToSlash(rel))
							}
						}
						return nil
					})
				}
			}
		}
	}

	if len(supporting) > 0 && skillDir != "" {
		skillViewTarget := filepath.Base(skillDir)
		skillsRoot := SkillsDir()
		if rel, err := filepath.Rel(skillsRoot, skillDir); err == nil {
			skillViewTarget = filepath.ToSlash(rel)
		}

		parts = append(parts, "", "[This skill has supporting files:]")
		for _, sf := range supporting {
			parts = append(parts, fmt.Sprintf("- %s  ->  %s", sf, filepath.Join(skillDir, sf)))
		}
		parts = append(parts, fmt.Sprintf("\nLoad any of these with skill_view(name=%q, file_path=%q), or run scripts directly by absolute path.", skillViewTarget, "<path>"))
	}

	if strings.TrimSpace(userInstruction) != "" {
		parts = append(parts, "", fmt.Sprintf("The user has provided the following instruction alongside the skill invocation: %s", strings.TrimSpace(userInstruction)))
	}

	return strings.Join(parts, "\n")
}

func ExpandSkillSlashCommand(skillName, userArgs, workDir string, cfg config.Runtime, sessionId string) (string, string, error) {
	// Execute executeSkillView logic (preprocess = false)
	viewRes := executeSkillViewInternal(skillName, "", workDir, cfg, sessionId, false)
	if !viewRes.Success {
		return "", "", errors.New(viewRes.Error)
	}

	loadedSkill := map[string]interface{}{
		"name":         viewRes.Name,
		"content":      viewRes.RawContent,
		"linked_files": viewRes.LinkedFiles,
	}

	message := BuildSkillInvocationMessage(loadedSkill, viewRes.SkillDir, userArgs, sessionId, cfg)
	cleanBody := PreprocessSkillContent(viewRes.RawContent, viewRes.SkillDir, sessionId, cfg.Skills.InlineShell, cfg.Skills.InlineShellTimeout)
	return message, cleanBody, nil
}

type ReloadDiff struct {
	Added     []map[string]string `json:"added"`
	Removed   []map[string]string `json:"removed"`
	Unchanged []string            `json:"unchanged"`
	Total     int                 `json:"total"`
	Commands  int                 `json:"commands"`
}

func ReloadSkills(workDir string, cfg config.Runtime) (ReloadDiff, error) {
	before := make(map[string]string)
	snap := SnapshotPath()
	if dataBytes, err := os.ReadFile(snap); err == nil {
		var snapshot SkillsPromptSnapshot
		if err := json.Unmarshal(dataBytes, &snapshot); err == nil {
			for _, entry := range snapshot.Skills {
				before[entry.SkillName] = entry.Description
			}
		}
	}

	discovered, _ := DiscoverAllSkills(workDir, cfg)
	after := make(map[string]string)
	var afterEntries []SkillSnapshotEntry

	for _, sk := range discovered {
		after[sk.Name] = sk.Description
		afterEntries = append(afterEntries, SkillSnapshotEntry{
			SkillName:              sk.Name,
			Category:               sk.Category,
			FrontmatterName:        sk.Name,
			Description:            sk.Description,
			Platforms:              sk.Platforms,
			Conditions:             sk.Conditions,
			DisableModelInvocation: sk.DisableModelInvocation,
			Environments:           sk.Environments,
		})
	}

	dirs := SearchLocations(workDir, cfg, "")
	manifest := buildFullManifest(dirs)
	categoryDescs := readAllCategoryDescriptions(dirs)
	writeSkillsSnapshot(manifest, afterEntries, categoryDescs)

	var added []map[string]string
	var removed []map[string]string
	var unchanged []string

	for name, desc := range after {
		if _, ok := before[name]; !ok {
			added = append(added, map[string]string{"name": name, "description": desc})
		} else {
			unchanged = append(unchanged, name)
		}
	}

	for name, desc := range before {
		if _, ok := after[name]; !ok {
			removed = append(removed, map[string]string{"name": name, "description": desc})
		}
	}

	sort.Strings(unchanged)
	sort.Slice(added, func(i, j int) bool { return added[i]["name"] < added[j]["name"] })
	sort.Slice(removed, func(i, j int) bool { return removed[i]["name"] < removed[j]["name"] })

	return ReloadDiff{
		Added:     added,
		Removed:   removed,
		Unchanged: unchanged,
		Total:     len(after),
		Commands:  len(after) * 2,
	}, nil
}

func BuildPreloadedSkillsPrompt(skillIdentifiers []string, workDir, sessionId string, cfg config.Runtime) (string, []string, []string, error) {
	var promptParts []string
	var loadedNames []string
	var missing []string
	seen := make(map[string]bool)

	for _, rawIdentifier := range skillIdentifiers {
		identifier := strings.TrimSpace(rawIdentifier)
		if identifier == "" || seen[identifier] {
			continue
		}
		seen[identifier] = true

		res := executeSkillViewInternal(identifier, "", workDir, cfg, sessionId, false)
		if !res.Success {
			missing = append(missing, identifier)
			continue
		}

		BumpUse(res.Name)

		activationNote := fmt.Sprintf("[IMPORTANT: The user launched this CLI session with the %q skill preloaded. Treat its instructions as active guidance for the duration of this session unless the user overrides them.]", res.Name)

		// Preprocess content
		content := PreprocessSkillContent(res.RawContent, res.SkillDir, sessionId, cfg.Skills.InlineShell, cfg.Skills.InlineShellTimeout)

		var parts []string
		parts = append(parts, activationNote, "", strings.TrimSpace(content))

		if res.SkillDir != "" {
			parts = append(parts, "", fmt.Sprintf("[Skill directory: %s]", res.SkillDir),
				"Resolve any relative paths in this skill (e.g. `scripts/foo.js`, `templates/config.yaml`) against that directory, then run them with the terminal tool using the absolute path.")
		}

		if res.SetupNeeded && res.Setup != nil && res.Setup.Help != nil && *res.Setup.Help != "" {
			parts = append(parts, "", fmt.Sprintf("[Skill setup note: %s]", *res.Setup.Help))
		}

		var supporting []string
		for _, entries := range res.LinkedFiles {
			supporting = append(supporting, entries...)
		}
		if len(supporting) == 0 && res.SkillDir != "" {
			for _, subdir := range []string{"references", "templates", "scripts", "assets"} {
				subdirPath := filepath.Join(res.SkillDir, subdir)
				if _, err := os.Stat(subdirPath); err == nil {
					_ = filepath.Walk(subdirPath, func(path string, info os.FileInfo, err error) error {
						if err != nil {
							return nil
						}
						if !info.IsDir() {
							rel, err := filepath.Rel(res.SkillDir, path)
							if err == nil {
								supporting = append(supporting, filepath.ToSlash(rel))
							}
						}
						return nil
					})
				}
			}
		}

		if len(supporting) > 0 && res.SkillDir != "" {
			skillViewTarget := filepath.Base(res.SkillDir)
			skillsRoot := SkillsDir()
			if rel, err := filepath.Rel(skillsRoot, res.SkillDir); err == nil {
				skillViewTarget = filepath.ToSlash(rel)
			}

			parts = append(parts, "", "[This skill has supporting files:]")
			for _, sf := range supporting {
				parts = append(parts, fmt.Sprintf("- %s  ->  %s", sf, filepath.Join(res.SkillDir, sf)))
			}
			parts = append(parts, fmt.Sprintf("\nLoad any of these with skill_view(name=%q, file_path=%q), or run scripts directly by absolute path.", skillViewTarget, "<path>"))
		}

		promptParts = append(promptParts, strings.Join(parts, "\n"))
		loadedNames = append(loadedNames, res.Name)
	}

	return strings.Join(promptParts, "\n\n"), loadedNames, missing, nil
}

