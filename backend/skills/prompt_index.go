package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/enough/enough/backend/config"
	"github.com/enough/enough/backend/enoughhome"
	"github.com/enough/enough/backend/toolsets"
)

var (
	promptCache     = make(map[string]string)
	promptCacheKeys []string
	promptCacheMu   sync.Mutex
)

func ClearSkillsPromptCache() {
	promptCacheMu.Lock()
	defer promptCacheMu.Unlock()
	promptCache = make(map[string]string)
	promptCacheKeys = nil

	snap := SnapshotPath()
	if _, err := os.Stat(snap); err == nil {
		_ = os.Remove(snap)
	}
}

func getFromCache(key string) (string, bool) {
	promptCacheMu.Lock()
	defer promptCacheMu.Unlock()
	val, ok := promptCache[key]
	if ok {
		// Move key to the end of keys (most recently used)
		for i, k := range promptCacheKeys {
			if k == key {
				promptCacheKeys = append(promptCacheKeys[:i], promptCacheKeys[i+1:]...)
				break
			}
		}
		promptCacheKeys = append(promptCacheKeys, key)
	}
	return val, ok
}

func setToCache(key, val string) {
	promptCacheMu.Lock()
	defer promptCacheMu.Unlock()

	if _, ok := promptCache[key]; ok {
		promptCache[key] = val
		for i, k := range promptCacheKeys {
			if k == key {
				promptCacheKeys = append(promptCacheKeys[:i], promptCacheKeys[i+1:]...)
				break
			}
		}
		promptCacheKeys = append(promptCacheKeys, key)
		return
	}

	if len(promptCache) >= SkillsPromptCacheMax {
		oldest := promptCacheKeys[0]
		promptCacheKeys = promptCacheKeys[1:]
		delete(promptCache, oldest)
	}
	promptCache[key] = val
	promptCacheKeys = append(promptCacheKeys, key)
}

func buildFullManifest(dirs []SearchDir) map[string][2]int64 {
	manifest := make(map[string][2]int64)
	for _, dir := range dirs {
		if _, err := os.Stat(dir.Path); err != nil {
			continue
		}
		for _, filename := range []string{"SKILL.md", "DESCRIPTION.md"} {
			for _, fp := range IterSkillIndexFiles(dir.Path, filename) {
				fi, err := os.Stat(fp)
				if err != nil {
					continue
				}
				abs, err := filepath.Abs(fp)
				if err != nil {
					abs = fp
				}
				manifest[filepath.ToSlash(abs)] = [2]int64{fi.ModTime().UnixNano(), fi.Size()}
			}
		}
	}
	return manifest
}

func getManifestHashString(manifest map[string][2]int64) string {
	var keys []string
	for k := range manifest {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for _, k := range keys {
		val := manifest[k]
		fmt.Fprintf(&sb, "%s:%d:%d;", k, val[0], val[1])
	}
	return sb.String()
}

func buildPromptCacheKey(workDir string, cfg config.Runtime, toolNames []string, manifest map[string][2]int64) string {
	var sb strings.Builder
	sb.WriteString(enoughhome.HomeDir())
	sb.WriteString("|")
	sb.WriteString(workDir)
	sb.WriteString("|")
	disabled := make([]string, len(cfg.Skills.Disabled))
	copy(disabled, cfg.Skills.Disabled)
	sort.Strings(disabled)
	sb.WriteString(strings.Join(disabled, ","))
	sb.WriteString("|")
	paths := make([]string, len(cfg.Skills.Paths))
	copy(paths, cfg.Skills.Paths)
	sort.Strings(paths)
	sb.WriteString(strings.Join(paths, ","))
	sb.WriteString("|")
	tools := make([]string, len(toolNames))
	copy(tools, toolNames)
	sort.Strings(tools)
	sb.WriteString(strings.Join(tools, ","))
	sb.WriteString("|")
	activeToolsets := toolsets.GetAvailableToolsets(toolNames)
	sb.WriteString(strings.Join(activeToolsets, ","))
	sb.WriteString("|")
	sb.WriteString(resolvePlatform())
	sb.WriteString("|")
	sb.WriteString(cfg.Agent.CodingContext)
	sb.WriteString("|")
	sb.WriteString(getManifestHashString(manifest))
	return sb.String()
}

func loadSkillsSnapshot(manifest map[string][2]int64) *SkillsPromptSnapshot {
	snap := SnapshotPath()
	dataBytes, err := os.ReadFile(snap)
	if err != nil {
		return nil
	}
	var snapshot SkillsPromptSnapshot
	if err := json.Unmarshal(dataBytes, &snapshot); err != nil {
		return nil
	}
	if snapshot.Version != SkillsSnapshotVersion {
		return nil
	}

	if len(manifest) != len(snapshot.Manifest) {
		return nil
	}
	for k, v := range manifest {
		old, ok := snapshot.Manifest[k]
		if !ok || old[0] != v[0] || old[1] != v[1] {
			return nil
		}
	}

	return &snapshot
}

func writeSkillsSnapshot(manifest map[string][2]int64, skills []SkillSnapshotEntry, categoryDescs map[string]string) {
	snap := SnapshotPath()
	snapshot := SkillsPromptSnapshot{
		Version:              SkillsSnapshotVersion,
		Manifest:             manifest,
		Skills:               skills,
		CategoryDescriptions: categoryDescs,
	}
	dataBytes, err := json.MarshalIndent(snapshot, "", "  ")
	if err == nil {
		_ = atomicWrite(snap, dataBytes)
	}
}

func readCategoryDescriptions(dir string) map[string]string {
	descriptions := make(map[string]string)
	resolvedRoot, err := filepath.Abs(dir)
	if err != nil {
		resolvedRoot = dir
	}

	for _, fp := range IterSkillIndexFiles(dir, "DESCRIPTION.md") {
		data, err := os.ReadFile(fp)
		if err != nil {
			continue
		}
		fm, _ := ParseFrontmatter(string(data))
		if fm == nil {
			continue
		}
		descVal, ok := fm["description"].(string)
		if !ok || descVal == "" {
			continue
		}
		rel, err := filepath.Rel(resolvedRoot, fp)
		if err != nil {
			continue
		}
		rel = filepath.ToSlash(rel)
		parts := strings.Split(rel, "/")
		cat := "general"
		if len(parts) > 1 {
			cat = strings.Join(parts[:len(parts)-1], "/")
		}
		descriptions[cat] = strings.Trim(strings.TrimSpace(descVal), `"'`)
	}
	return descriptions
}

func readAllCategoryDescriptions(dirs []SearchDir) map[string]string {
	descriptions := make(map[string]string)
	for _, dir := range dirs {
		if _, err := os.Stat(dir.Path); err != nil {
			continue
		}
		for cat, desc := range readCategoryDescriptions(dir.Path) {
			if _, ok := descriptions[cat]; !ok {
				descriptions[cat] = desc
			}
		}
	}
	return descriptions
}

func BuildIndexPrompt(workDir string, cfg config.Runtime, toolNames []string) string {
	dirs := SearchLocations(workDir, cfg, "")
	manifest := buildFullManifest(dirs)
	cacheKey := buildPromptCacheKey(workDir, cfg, toolNames, manifest)

	if cached, ok := getFromCache(cacheKey); ok {
		return cached
	}

	toolsSet := make(map[string]bool)
	for _, tn := range toolNames {
		toolsSet[tn] = true
	}

	var categoryDescs map[string]string
	var skillsToRender []SkillSnapshotEntry

	snapshot := loadSkillsSnapshot(manifest)
	if snapshot != nil {
		skillsToRender = snapshot.Skills
		categoryDescs = snapshot.CategoryDescriptions
	} else {
		discovered, _ := DiscoverAllSkills(workDir, cfg)

		var entries []SkillSnapshotEntry
		for _, sk := range discovered {
			entries = append(entries, SkillSnapshotEntry{
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

		categoryDescs = readAllCategoryDescriptions(dirs)
		writeSkillsSnapshot(manifest, entries, categoryDescs)
		skillsToRender = entries
	}

	toolsetsList := toolsets.GetAvailableToolsets(toolNames)
	toolsetsSet := make(map[string]bool)
	for _, ts := range toolsetsList {
		toolsetsSet[ts] = true
	}

	skillsByCategory := make(map[string]map[string]string)
	seenNames := make(map[string]bool)

	for _, entry := range skillsToRender {
		if entry.DisableModelInvocation {
			continue
		}
		fmDummy := map[string]interface{}{
			"platforms":    entry.Platforms,
			"environments": entry.Environments,
		}
		if !skillMatchesPlatform(fmDummy) {
			continue
		}
		if !SkillMatchesEnvironment(fmDummy) {
			continue
		}
		if !skillShouldShow(entry.Conditions, toolsSet, toolsetsSet) {
			continue
		}

		name := entry.FrontmatterName
		if seenNames[name] {
			continue
		}
		seenNames[name] = true

		cat := entry.Category
		if cat == "" {
			cat = "general"
		}
		if _, ok := skillsByCategory[cat]; !ok {
			skillsByCategory[cat] = make(map[string]string)
		}

		desc := entry.Description
		if len(desc) > PromptIndexDescriptionMax {
			desc = desc[:PromptIndexDescriptionMax-3] + "..."
		}
		skillsByCategory[cat][name] = desc
	}

	if len(skillsByCategory) == 0 {
		return ""
	}

	isCoding := isCodingContext(workDir, cfg)
	configMode := strings.TrimSpace(strings.ToLower(cfg.Agent.CodingContext))
	if configMode == "" {
		configMode = "auto"
	}
	hasDemoted := false

	var indexLines []string
	var categories []string
	for cat := range skillsByCategory {
		categories = append(categories, cat)
	}
	sort.Strings(categories)

	for _, cat := range categories {
		var skillNames []string
		for name := range skillsByCategory[cat] {
			skillNames = append(skillNames, name)
		}
		sort.Strings(skillNames)

		if shouldDemoteCategory(cat, isCoding, configMode) {
			hasDemoted = true
			indexLines = append(indexLines, fmt.Sprintf("  %s [names only]: %s", cat, strings.Join(skillNames, ", ")))
			continue
		}

		desc := categoryDescs[cat]
		if desc != "" {
			indexLines = append(indexLines, "  "+cat+": "+desc)
		} else {
			indexLines = append(indexLines, "  "+cat+":")
		}

		for _, name := range skillNames {
			desc := skillsByCategory[cat][name]
			if desc != "" {
				indexLines = append(indexLines, "    - "+name+": "+desc)
			} else {
				indexLines = append(indexLines, "    - "+name)
			}
		}
	}

	hiddenNote := ""
	if hasDemoted {
		hiddenNote = "\n(Categories marked [names only] are outside the current coding context, so their descriptions are omitted — the skills work normally and load with skill_view(name) as usual.)"
	}

	result := SkillsIndexHeader + "\n" +
		"<available_skills>\n" +
		strings.Join(indexLines, "\n") +
		"\n</available_skills>\n" +
		SkillsIndexFooter + hiddenNote

	setToCache(cacheKey, result)
	return result
}

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

func isCodingContext(workDir string, cfg config.Runtime) bool {
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

func shouldDemoteCategory(cat string, isCoding bool, configMode string) bool {
	if !isCoding || configMode != "focus" {
		return false
	}
	parts := strings.SplitN(cat, "/", 2)
	return NonCodingCategories[parts[0]]
}
