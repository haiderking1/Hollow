package skills

import (
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

func parseFrontmatterYAML(yamlStr string) map[string]interface{} {
	var m map[string]interface{}
	err := yaml.Unmarshal([]byte(yamlStr), &m)
	if err != nil {
		return make(map[string]interface{})
	}
	return m
}

func ParseFrontmatter(content string) (map[string]interface{}, string) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(content, "---") {
		return nil, content
	}

	parts := strings.SplitN(content, "---", 3)
	if len(parts) < 3 {
		return nil, content
	}

	fm := parseFrontmatterYAML(parts[1])
	body := parts[2]
	return fm, body
}

func SkillMatchesPlatform(fm map[string]interface{}) bool {
	return skillMatchesPlatform(fm)
}

func skillMatchesPlatform(fm map[string]interface{}) bool {
	platformsVal, ok := fm["platforms"]
	if !ok || platformsVal == nil {
		return true
	}
	var list []string
	switch v := platformsVal.(type) {
	case string:
		list = []string{v}
	case []string:
		list = v
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok {
				list = append(list, s)
			}
		}
	}
	if len(list) == 0 {
		return true
	}
	current := runtime.GOOS
	for _, platform := range list {
		normalized := strings.ToLower(strings.TrimSpace(platform))
		mapped := PlatformMap[normalized]
		if mapped == "" {
			mapped = normalized
		}
		if mapped == "win32" {
			mapped = "windows"
		}
		if strings.HasPrefix(current, mapped) {
			return true
		}
	}
	return false
}

func toStringList(val interface{}) []string {
	if val == nil {
		return nil
	}
	switch v := val.(type) {
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	case []string:
		return v
	case []interface{}:
		var out []string
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func extractSkillConditions(fm map[string]interface{}) SkillConditions {
	var cond SkillConditions
	metaVal, ok := fm["metadata"]
	if !ok {
		return cond
	}
	metaObj, ok := metaVal.(map[string]interface{})
	if !ok {
		return cond
	}
	hermesVal, ok := metaObj["hermes"]
	if !ok {
		return cond
	}
	hermesObj, ok := hermesVal.(map[string]interface{})
	if !ok {
		return cond
	}
	cond.FallbackForToolsets = toStringList(hermesObj["fallback_for_toolsets"])
	cond.RequiresToolsets = toStringList(hermesObj["requires_toolsets"])
	cond.FallbackForTools = toStringList(hermesObj["fallback_for_tools"])
	cond.RequiresTools = toStringList(hermesObj["requires_tools"])
	return cond
}

func extractSkillDescription(fm map[string]interface{}) string {
	rawDesc, ok := fm["description"]
	if !ok || rawDesc == nil {
		return ""
	}
	desc := strings.TrimSpace(strings.Trim(strings.TrimSpace(rawDesc.(string)), `"'`))
	if len(desc) > PromptIndexDescriptionMax {
		return desc[:PromptIndexDescriptionMax-3] + "..."
	}
	return desc
}

func extractSkillTags(fm map[string]interface{}) []string {
	metaVal, ok := fm["metadata"]
	if !ok {
		return nil
	}
	metaObj, ok := metaVal.(map[string]interface{})
	if !ok {
		return nil
	}
	hermesVal, ok := metaObj["hermes"]
	if !ok {
		return nil
	}
	hermesObj, ok := hermesVal.(map[string]interface{})
	if !ok {
		return nil
	}
	return toStringList(hermesObj["tags"])
}

func extractRelatedSkills(fm map[string]interface{}) []string {
	metaVal, ok := fm["metadata"]
	if !ok {
		return nil
	}
	metaObj, ok := metaVal.(map[string]interface{})
	if !ok {
		return nil
	}
	hermesVal, ok := metaObj["hermes"]
	if !ok {
		return nil
	}
	hermesObj, ok := hermesVal.(map[string]interface{})
	if !ok {
		return nil
	}
	return toStringList(hermesObj["related_skills"])
}

func normalizePlatforms(fm map[string]interface{}) []string {
	platformsVal, ok := fm["platforms"]
	if !ok || platformsVal == nil {
		return nil
	}
	list := toStringList(platformsVal)
	var out []string
	for _, p := range list {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func skillShouldShow(conditions SkillConditions, availableTools map[string]bool, availableToolsets map[string]bool) bool {
	if availableTools == nil && availableToolsets == nil {
		return true
	}

	for _, ts := range conditions.FallbackForToolsets {
		if availableToolsets != nil && availableToolsets[ts] {
			return false
		}
	}
	for _, t := range conditions.FallbackForTools {
		if availableTools != nil && availableTools[t] {
			return false
		}
	}
	for _, ts := range conditions.RequiresToolsets {
		if availableToolsets == nil || !availableToolsets[ts] {
			return false
		}
	}
	for _, t := range conditions.RequiresTools {
		if availableTools == nil || !availableTools[t] {
			return false
		}
	}
	return true
}

func computeSkillCategory(skillFilePath, skillsRoot string) string {
	rel, err := filepath.Rel(skillsRoot, skillFilePath)
	if err != nil {
		return "general"
	}
	rel = filepath.ToSlash(rel)
	parts := strings.Split(rel, "/")
	var cleaned []string
	for _, p := range parts {
		if p != "" {
			cleaned = append(cleaned, p)
		}
	}
	if len(cleaned) >= 2 {
		if len(cleaned) > 2 {
			return strings.Join(cleaned[:len(cleaned)-2], "/")
		}
		return cleaned[0]
	}
	return "general"
}

func buildSnapshotEntry(skillFile, skillsDir string, fm map[string]interface{}, description string) SkillSnapshotEntry {
	rel, err := filepath.Rel(skillsDir, skillFile)
	var skillName string
	var category string
	if err == nil {
		rel = filepath.ToSlash(rel)
		parts := strings.Split(rel, "/")
		var cleaned []string
		for _, p := range parts {
			if p != "" {
				cleaned = append(cleaned, p)
			}
		}
		if len(cleaned) >= 2 {
			skillName = cleaned[len(cleaned)-2]
			if len(cleaned) > 2 {
				category = strings.Join(cleaned[:len(cleaned)-2], "/")
			} else {
				category = cleaned[0]
			}
		} else {
			category = "general"
			if len(cleaned) > 0 {
				skillName = strings.TrimSuffix(cleaned[len(cleaned)-1], ".md")
			} else {
				skillName = "unknown"
			}
		}
	} else {
		category = "general"
		skillName = "unknown"
	}

	fmNameVal, _ := fm["name"].(string)
	frontmatterName := fmNameVal
	if frontmatterName == "" {
		frontmatterName = skillName
	}

	platforms := normalizePlatforms(fm)
	if platforms == nil {
		platforms = []string{}
	}

	envs := toStringList(fm["environments"])
	if envs == nil {
		envs = []string{}
	}

	return SkillSnapshotEntry{
		SkillName:       skillName,
		Category:        category,
		FrontmatterName: frontmatterName,
		Description:     description,
		Platforms:       platforms,
		Conditions:      extractSkillConditions(fm),
		Environments:    envs,
	}
}
