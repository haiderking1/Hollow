package skills

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/enough/enough/backend/config"
)

type ignorePattern struct {
	regex   *regexp.Regexp
	negated bool
	dirOnly bool
}

type GitIgnoreMatcher struct {
	patterns []ignorePattern
}

func (m *GitIgnoreMatcher) Clone() *GitIgnoreMatcher {
	if m == nil {
		return &GitIgnoreMatcher{}
	}
	clone := &GitIgnoreMatcher{
		patterns: make([]ignorePattern, len(m.patterns)),
	}
	copy(clone.patterns, m.patterns)
	return clone
}

func parseGitIgnore(content, prefix string) *GitIgnoreMatcher {
	matcher := &GitIgnoreMatcher{}
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		negated := false
		if strings.HasPrefix(line, "!") {
			negated = true
			line = line[1:]
		} else if strings.HasPrefix(line, `\!`) {
			line = line[1:]
		}

		dirOnly := false
		if strings.HasSuffix(line, "/") {
			dirOnly = true
			line = line[:len(line)-1]
		}

		if line == "" {
			continue
		}

		var pat string
		if prefix != "" {
			pat = prefix + "/" + line
		} else {
			pat = line
		}

		regex := gitIgnoreToRegex(pat)
		if regex != nil {
			matcher.patterns = append(matcher.patterns, ignorePattern{
				regex:   regex,
				negated: negated,
				dirOnly: dirOnly,
			})
		}
	}
	return matcher
}

func gitIgnoreToRegex(pattern string) *regexp.Regexp {
	var sb strings.Builder
	sb.WriteString("^")

	hasSlash := false
	for i := 0; i < len(pattern); i++ {
		if pattern[i] == '/' {
			hasSlash = true
			break
		}
	}
	if !hasSlash {
		sb.WriteString("(?:.*/)?")
	} else if strings.HasPrefix(pattern, "/") {
		pattern = pattern[1:]
	}

	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch c {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				sb.WriteString(".*")
				i++
				if i+1 < len(pattern) && pattern[i+1] == '/' {
					i++
				}
			} else {
				sb.WriteString("[^/]*")
			}
		case '?':
			sb.WriteString("[^/]")
		case '.', '+', '(', ')', '^', '$', '{', '}', '[', ']', '|', '\\':
			sb.WriteString("\\" + string(c))
		default:
			sb.WriteString(string(c))
		}
	}
	sb.WriteString("$")
	r, err := regexp.Compile(sb.String())
	if err != nil {
		return nil
	}
	return r
}

func (m *GitIgnoreMatcher) Matches(relPath string, isDir bool) bool {
	relPath = filepath.ToSlash(relPath)
	ignored := false
	for _, p := range m.patterns {
		if p.dirOnly && !isDir {
			continue
		}
		if p.regex.MatchString(relPath) {
			ignored = !p.negated
		}
	}
	return ignored
}

func addIgnoreRules(ig *GitIgnoreMatcher, dir, rootDir string) {
	rel, err := filepath.Rel(rootDir, dir)
	prefix := ""
	if err == nil && rel != "." {
		prefix = filepath.ToSlash(rel)
	}

	ignoreFileNames := []string{".gitignore", ".ignore", ".fdignore"}
	for _, filename := range ignoreFileNames {
		p := filepath.Join(dir, filename)
		if data, err := os.ReadFile(p); err == nil {
			parsed := parseGitIgnore(string(data), prefix)
			ig.patterns = append(ig.patterns, parsed.patterns...)
		}
	}
}

func isExcludedSkillPath(filePath string) bool {
	parts := strings.FieldsFunc(filePath, func(r rune) bool {
		return r == '/' || r == '\\'
	})
	for _, part := range parts {
		if ExcludedSkillDirs[part] {
			return true
		}
	}
	return false
}

func IterSkillIndexFiles(skillsDir, filename string) []string {
	if _, err := os.Stat(skillsDir); os.IsNotExist(err) {
		return nil
	}

	var matches []string
	var walk func(string)
	walk = func(dir string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, entry := range entries {
			if entry.IsDir() && ExcludedSkillDirs[entry.Name()] {
				continue
			}
			if entry.Name() == "skills-cursor" {
				continue
			}
			fullPath := filepath.Join(dir, entry.Name())
			if entry.IsDir() {
				walk(fullPath)
				continue
			}
			if entry.Name() == filename {
				matches = append(matches, fullPath)
			}
		}
	}
	walk(skillsDir)

	resolvedRoot, err := filepath.Abs(skillsDir)
	if err != nil {
		resolvedRoot = skillsDir
	}
	sort.Slice(matches, func(i, j int) bool {
		relA, _ := filepath.Rel(resolvedRoot, matches[i])
		relB, _ := filepath.Rel(resolvedRoot, matches[j])
		return filepath.ToSlash(relA) < filepath.ToSlash(relB)
	})

	return matches
}

func validateName(name string) []string {
	var errs []string
	if len(name) > MaxSkillNameLength {
		errs = append(errs, fmt.Sprintf("name exceeds %d characters (%d)", MaxSkillNameLength, len(name)))
	}
	if !SkillNameValidRe.MatchString(name) {
		errs = append(errs, "name contains invalid characters (must be lowercase a-z, 0-9, hyphens only)")
	}
	if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
		errs = append(errs, "name must not start or end with a hyphen")
	}
	if strings.Contains(name, "--") {
		errs = append(errs, "name must not contain consecutive hyphens")
	}
	return errs
}

func validateDescription(desc string) []string {
	var errs []string
	if strings.TrimSpace(desc) == "" {
		errs = append(errs, "description is required")
	} else if len(desc) > MaxSkillDescriptionLength {
		errs = append(errs, fmt.Sprintf("description exceeds %d characters (%d)", MaxSkillDescriptionLength, len(desc)))
	}
	return errs
}

func loadSkillFromFile(filePath, source, skillsRoot string) (*Skill, []string) {
	var warnings []string
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, []string{err.Error()}
	}

	fm, _ := ParseFrontmatter(string(data))
	if fm == nil {
		return nil, []string{"missing frontmatter"}
	}

	desc := extractSkillDescription(fm)
	descErrs := validateDescription(desc)
	warnings = append(warnings, descErrs...)

	skillDir := filepath.Dir(filePath)
	parentDirName := filepath.Base(skillDir)

	name, _ := fm["name"].(string)
	if name == "" {
		name = parentDirName
	}

	nameErrs := validateName(name)
	warnings = append(warnings, nameErrs...)

	descFull, _ := fm["description"].(string)
	if descFull == "" {
		return nil, warnings
	}

	if !skillMatchesPlatform(fm) {
		return nil, warnings
	}

	if !SkillMatchesEnvironment(fm) {
		return nil, warnings
	}

	disableModelInvocation, _ := fm["disable-model-invocation"].(bool)

	category := computeSkillCategory(filePath, skillsRoot)
	conditions := extractSkillConditions(fm)
	tags := extractSkillTags(fm)
	related := extractRelatedSkills(fm)
	platforms := normalizePlatforms(fm)

	scope := "project"
	if strings.Contains(skillsRoot, ".enough/skills") || strings.Contains(skillsRoot, ".enough/agent/skills") {
		scope = "user"
	}

	envs := toStringList(fm["environments"])
	if envs == nil {
		envs = []string{}
	}

	return &Skill{
		Name:            name,
		Description:     desc,
		FilePath:        filePath,
		BaseDir:         skillDir,
		DescriptionFull: descFull,
		SourceInfo: SourceInfo{
			Source:  source,
			Scope:   scope,
			BaseDir: skillsRoot,
		},
		DisableModelInvocation: disableModelInvocation,
		Category:               category,
		Platforms:              platforms,
		Tags:                   tags,
		RelatedSkills:          related,
		Conditions:             conditions,
		Environments:           envs,
	}, warnings
}

func loadSkillsFromDirInternal(dir, source string, includeRootFiles bool, ig *GitIgnoreMatcher, rootDir, skillsRoot string) ([]Skill, []string) {
	var skills []Skill
	var diagnostics []string

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil, nil
	}

	if ig == nil {
		ig = &GitIgnoreMatcher{}
	}
	addIgnoreRules(ig, dir, rootDir)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}

	// 1. Check if SKILL.md is present
	for _, entry := range entries {
		if entry.Name() == "SKILL.md" {
			fullPath := filepath.Join(dir, entry.Name())
			rel, relErr := filepath.Rel(rootDir, fullPath)
			if relErr == nil && ig.Matches(rel, false) {
				continue
			}
			sk, warns := loadSkillFromFile(fullPath, source, skillsRoot)
			diagnostics = append(diagnostics, warns...)
			if sk != nil {
				skills = append(skills, *sk)
			}
			return skills, diagnostics
		}
	}

	// 2. Otherwise recurse into subdirectories and load root .md files if allowed
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if entry.Name() == "node_modules" || entry.Name() == "skills-cursor" || ExcludedSkillDirs[entry.Name()] {
			continue
		}

		fullPath := filepath.Join(dir, entry.Name())
		rel, relErr := filepath.Rel(rootDir, fullPath)
		if relErr != nil {
			continue
		}

		isDir := entry.IsDir()
		if (entry.Type() & os.ModeSymlink) != 0 {
			if fi, err := os.Stat(fullPath); err == nil {
				isDir = fi.IsDir()
			}
		}

		if ig.Matches(rel, isDir) {
			continue
		}

		if isDir {
			subIg := ig.Clone()
			subSkills, subDiag := loadSkillsFromDirInternal(fullPath, source, false, subIg, rootDir, skillsRoot)
			skills = append(skills, subSkills...)
			diagnostics = append(diagnostics, subDiag...)
		} else if includeRootFiles && strings.HasSuffix(entry.Name(), ".md") {
			sk, warns := loadSkillFromFile(fullPath, source, skillsRoot)
			diagnostics = append(diagnostics, warns...)
			if sk != nil {
				skills = append(skills, *sk)
			}
		}
	}

	return skills, diagnostics
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

func IsSkillDisabled(name string, cfg config.Runtime) bool {
	platform := resolvePlatform()
	if cfg.Skills.PlatformDisabled != nil {
		if list, ok := cfg.Skills.PlatformDisabled[platform]; ok {
			for _, d := range list {
				if d == name {
					return true
				}
			}
		}
	}
	for _, d := range cfg.Skills.Disabled {
		if d == name {
			return true
		}
	}
	return false
}

func DiscoverAllSkills(cwd string, cfg config.Runtime) ([]Skill, []string) {
	dirs := SearchLocations(cwd, cfg, "")
	return LoadSkillsFromDirs(cwd, dirs, cfg)
}

func LoadSkillsFromDirs(cwd string, dirs []SearchDir, cfg config.Runtime) ([]Skill, []string) {
	var allSkills []Skill
	var allDiagnostics []string
	skillMap := make(map[string]Skill)
	canonicalSet := make(map[string]bool)

	var exclusionRegexes []*regexp.Regexp
	for _, p := range cfg.Skills.Paths {
		if strings.HasPrefix(p, "!") {
			pat := strings.TrimPrefix(p, "!")
			rx := gitIgnoreToRegex(pat)
			if rx != nil {
				exclusionRegexes = append(exclusionRegexes, rx)
			}
		}
	}

	addSkills := func(skills []Skill, diags []string) {
		allDiagnostics = append(allDiagnostics, diags...)
		for _, sk := range skills {
			if IsSkillDisabled(sk.Name, cfg) {
				continue
			}

			excluded := false
			for _, rx := range exclusionRegexes {
				if rx.MatchString(filepath.ToSlash(sk.FilePath)) {
					excluded = true
					break
				}
			}
			if excluded {
				continue
			}

			canonical, err := filepath.EvalSymlinks(sk.FilePath)
			if err != nil {
				canonical = sk.FilePath
			}

			if canonicalSet[canonical] {
				continue
			}

			existing, ok := skillMap[sk.Name]
			if ok {
				allDiagnostics = append(allDiagnostics, fmt.Sprintf("name %q collision: winner=%s loser=%s", sk.Name, sk.FilePath, existing.FilePath))
			}

			skillMap[sk.Name] = sk
			canonicalSet[canonical] = true
		}
	}

	// Loop in reverse order (lowest precedence to highest) so later scans overwrite earlier
	for i := len(dirs) - 1; i >= 0; i-- {
		dir := dirs[i]
		if _, err := os.Stat(dir.Path); err == nil {
			s, d := loadSkillsFromDirInternal(dir.Path, dir.Source, dir.IncludeRootMD, nil, dir.Path, dir.Path)
			addSkills(s, d)
		}
	}

	for _, sk := range skillMap {
		allSkills = append(allSkills, sk)
	}

	// Sort skills by category, then by name for stable list output
	sort.Slice(allSkills, func(i, j int) bool {
		if allSkills[i].Category != allSkills[j].Category {
			return allSkills[i].Category < allSkills[j].Category
		}
		return allSkills[i].Name < allSkills[j].Name
	})

	return allSkills, allDiagnostics
}
