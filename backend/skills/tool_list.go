package skills

import (
	"encoding/json"
	"os"
	"sort"
	"strings"

	"github.com/enough/enough/backend/config"
)

type skillItem struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
}

type listResult struct {
	Success    bool        `json:"success"`
	Skills     []skillItem `json:"skills"`
	Categories []string    `json:"categories"`
	Count      int         `json:"count"`
	Message    string      `json:"message,omitempty"`
	Hint       string      `json:"hint"`
}

func ExecuteSkillsList(argsJSON string, workDir string, cfg config.Runtime) (string, bool) {
	var args struct {
		Category string `json:"category"`
	}
	_ = json.Unmarshal([]byte(argsJSON), &args)

	skillsDir := SkillsDir()
	if _, err := os.Stat(skillsDir); os.IsNotExist(err) {
		_ = os.MkdirAll(skillsDir, 0o700)
	}

	allSkills, _ := DiscoverAllSkills(workDir, cfg)

	categoryFilter := strings.TrimSpace(args.Category)
	var filtered []Skill
	categorySet := make(map[string]bool)

	for _, sk := range allSkills {
		if categoryFilter != "" && sk.Category != categoryFilter {
			continue
		}
		filtered = append(filtered, sk)
		categorySet[sk.Category] = true
	}

	var list []skillItem
	for _, sk := range filtered {
		list = append(list, skillItem{
			Name:        sk.Name,
			Description: sk.Description,
			Category:    sk.Category,
		})
	}

	var categories []string
	for c := range categorySet {
		categories = append(categories, c)
	}
	sort.Strings(categories)

	res := listResult{
		Success:    true,
		Skills:     list,
		Categories: categories,
		Count:      len(list),
		Hint:       "Use skill_view(name) to see full content, tags, and linked files",
	}

	if len(list) == 0 {
		res.Message = "No skills found. Skills live in ~/.enough/skills/<category>/<name>/SKILL.md"
	}

	outBytes, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return `{"success": false, "error": "json marshal error"}`, true
	}
	return string(outBytes), false
}
