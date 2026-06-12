package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/enough/enough/backend/config"
	"github.com/enough/enough/backend/plugins"
)

type skillCandidate struct {
	skillDir string
	skillMd  string
}

type SkillViewResult struct {
	Success                             bool                 `json:"success"`
	Name                                string               `json:"name,omitempty"`
	Description                         string               `json:"description,omitempty"`
	Category                            string               `json:"category,omitempty"`
	Content                             string               `json:"content,omitempty"`
	RawContent                          string               `json:"raw_content,omitempty"`
	SkillDir                            string               `json:"skill_dir,omitempty"`
	LinkedFiles                         map[string][]string  `json:"linked_files,omitempty"`
	Tags                                []string             `json:"tags,omitempty"`
	RelatedSkills                       []string             `json:"related_skills,omitempty"`
	Warnings                            []string             `json:"warnings,omitempty"`
	UsageHint                           string               `json:"usage_hint,omitempty"`
	Error                               string               `json:"error,omitempty"`
	Matches                             []string             `json:"matches,omitempty"`
	Hint                                string               `json:"hint,omitempty"`
	File                                string               `json:"file,omitempty"`
	ReadinessStatus                     SkillReadinessStatus `json:"readiness_status,omitempty"`
	RequiredEnvironmentVariables        []RequiredEnvVar     `json:"required_environment_variables,omitempty"`
	MissingRequiredEnvironmentVariables []string             `json:"missing_required_environment_variables,omitempty"`
	SetupNeeded                         bool                 `json:"setup_needed"`
	Setup                               *SetupBlock          `json:"setup,omitempty"`
}

func findSkillCandidates(name, workDir string, cfg config.Runtime) []skillCandidate {
	var candidates []skillCandidate
	seen := make(map[string]bool)

	recordCandidate := func(skillDir, skillMd string) {
		resolved, err := filepath.Abs(skillMd)
		if err != nil {
			resolved = skillMd
		}
		if seen[resolved] {
			return
		}
		seen[resolved] = true
		candidates = append(candidates, skillCandidate{skillDir: skillDir, skillMd: skillMd})
	}

	dirs := SearchLocations(workDir, cfg, "")

	localCategoryName := ""
	if idx := strings.Index(name, ":"); idx >= 0 {
		ns := name[:idx]
		bare := name[idx+1:]
		if bare != "" {
			localCategoryName = ns + "/" + bare
		}
	}

	for _, dir := range dirs {
		searchDir := dir.Path
		if _, err := os.Stat(searchDir); os.IsNotExist(err) {
			continue
		}

		// 1. Direct path searchDir/name
		directPath := filepath.Join(searchDir, name)
		if fi, err := os.Stat(directPath); err == nil {
			if fi.IsDir() {
				skillMd := filepath.Join(directPath, "SKILL.md")
				if _, err := os.Stat(skillMd); err == nil {
					recordCandidate(directPath, skillMd)
				}
			} else if strings.HasSuffix(directPath, ".md") {
				recordCandidate("", directPath)
			} else {
				if _, err := os.Stat(directPath + ".md"); err == nil {
					recordCandidate("", directPath+".md")
				}
			}
		} else {
			if _, err := os.Stat(directPath + ".md"); err == nil {
				recordCandidate("", directPath+".md")
			}
		}

		// 2. category/name via : syntax
		if localCategoryName != "" {
			categorizedPath := filepath.Join(searchDir, localCategoryName)
			if fi, err := os.Stat(categorizedPath); err == nil {
				if fi.IsDir() {
					skillMd := filepath.Join(categorizedPath, "SKILL.md")
					if _, err := os.Stat(skillMd); err == nil {
						recordCandidate(categorizedPath, skillMd)
					}
				} else if strings.HasSuffix(categorizedPath, ".md") {
					recordCandidate("", categorizedPath)
				} else {
					if _, err := os.Stat(categorizedPath + ".md"); err == nil {
						recordCandidate("", categorizedPath+".md")
					}
				}
			} else {
				if _, err := os.Stat(categorizedPath + ".md"); err == nil {
					recordCandidate("", categorizedPath+".md")
				}
			}
		}

		// 3. Walk IterSkillIndexFiles matching basename
		for _, foundSkillMd := range IterSkillIndexFiles(searchDir, "SKILL.md") {
			if isExcludedSkillPath(foundSkillMd) {
				continue
			}
			if filepath.Base(filepath.Dir(foundSkillMd)) == name {
				recordCandidate(filepath.Dir(foundSkillMd), foundSkillMd)
			}
		}

		// 4. legacy flat name.md
		var walkForLegacyFlat func(string)
		walkForLegacyFlat = func(currentDir string) {
			entries, err := os.ReadDir(currentDir)
			if err != nil {
				return
			}
			for _, entry := range entries {
				if entry.IsDir() {
					if !ExcludedSkillDirs[entry.Name()] && entry.Name() != "skills-cursor" {
						walkForLegacyFlat(filepath.Join(currentDir, entry.Name()))
					}
					continue
				}
				if entry.Name() == name+".md" && entry.Name() != "SKILL.md" {
					recordCandidate("", filepath.Join(currentDir, entry.Name()))
				}
			}
		}
		walkForLegacyFlat(searchDir)
	}

	// Resolve collisions by precedence
	candidateName := func(c skillCandidate) string {
		data, err := os.ReadFile(c.skillMd)
		if err != nil {
			return ""
		}
		fm, _ := ParseFrontmatter(string(data))
		if fm != nil {
			if nameVal, ok := fm["name"].(string); ok && nameVal != "" {
				return nameVal
			}
		}
		if c.skillDir != "" {
			return filepath.Base(c.skillDir)
		}
		base := filepath.Base(c.skillMd)
		return strings.TrimSuffix(base, ".md")
	}

	groups := make(map[string][]skillCandidate)
	for _, c := range candidates {
		nameVal := candidateName(c)
		if nameVal != "" {
			groups[nameVal] = append(groups[nameVal], c)
		} else {
			groups[c.skillMd] = []skillCandidate{c}
		}
	}

	var filtered []skillCandidate
	for _, groupCands := range groups {
		if len(groupCands) <= 1 {
			filtered = append(filtered, groupCands...)
			continue
		}

		bestIdx := len(dirs)
		var bestCand skillCandidate
		hasBest := false

		for _, c := range groupCands {
			cAbs, err := filepath.Abs(c.skillMd)
			if err != nil {
				cAbs = c.skillMd
			}
			cIdx := len(dirs)
			for idx, dir := range dirs {
				if strings.HasPrefix(cAbs, dir.Path) {
					cIdx = idx
					break
				}
			}
			if cIdx < bestIdx {
				bestIdx = cIdx
				bestCand = c
				hasBest = true
			}
		}

		if hasBest {
			filtered = append(filtered, bestCand)
		}
	}

	return filtered
}

func scanLinkedFiles(skillDir string) map[string][]string {
	linked := map[string][]string{
		"references": {},
		"templates":  {},
		"assets":     {},
		"scripts":    {},
		"other":      {},
	}

	var walk func(string)
	walk = func(dir string) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, entry := range entries {
			full := filepath.Join(dir, entry.Name())
			if entry.IsDir() {
				walk(full)
				continue
			}
			if entry.Name() == "SKILL.md" {
				continue
			}
			rel, err := filepath.Rel(skillDir, full)
			if err != nil {
				continue
			}
			relPosix := filepath.ToSlash(rel)
			if strings.HasPrefix(relPosix, "references/") {
				linked["references"] = append(linked["references"], relPosix)
			} else if strings.HasPrefix(relPosix, "templates/") {
				linked["templates"] = append(linked["templates"], relPosix)
			} else if strings.HasPrefix(relPosix, "assets/") {
				linked["assets"] = append(linked["assets"], relPosix)
			} else if strings.HasPrefix(relPosix, "scripts/") {
				linked["scripts"] = append(linked["scripts"], relPosix)
			} else {
				ext := strings.ToLower(filepath.Ext(entry.Name()))
				if ext == ".md" || ext == ".py" || ext == ".yaml" || ext == ".yml" || ext == ".json" || ext == ".tex" || ext == ".sh" {
					linked["other"] = append(linked["other"], relPosix)
				}
			}
		}
	}
	walk(skillDir)

	filtered := make(map[string][]string)
	for k, v := range linked {
		if len(v) > 0 {
			sort.Strings(v)
			filtered[k] = v
		}
	}
	return filtered
}

func executeSkillViewInternal(name, filePath, workDir string, cfg config.Runtime, sessionId string, preprocess bool) SkillViewResult {
	name = strings.TrimSpace(name)
	filePath = strings.TrimSpace(filePath)

	if name == "" {
		return SkillViewResult{Success: false, Error: "Skill name is required."}
	}

	if strings.Contains(name, ":") {
		ns, bare := plugins.ParseQualifiedName(name)
		if plugins.IsValidNamespace(ns) {
			if plugins.IsPluginDisabled(ns, cfg) {
				return SkillViewResult{
					Success: false,
					Error:   fmt.Sprintf("Plugin '%s' is disabled. Re-enable with: enough plugins enable %s", ns, ns),
				}
			}

			available, _ := plugins.ListPluginSkills(ns)
			pluginSkillMd, err := plugins.FindPluginSkill(name)

			if err == nil || len(available) > 0 {
				if err != nil {
					var qualified []string
					for _, s := range available {
						qualified = append(qualified, ns+":"+s)
					}
					return SkillViewResult{
						Success: false,
						Error:   fmt.Sprintf("Skill '%s' not found in plugin '%s'.", bare, ns),
						Matches: qualified,
						Hint:    fmt.Sprintf("The '%s' plugin provides %d skill(s).", ns, len(available)),
					}
				}

				dataBytes, err := os.ReadFile(pluginSkillMd)
				if err != nil {
					return SkillViewResult{
						Success: false,
						Error:   fmt.Sprintf("Failed to read skill '%s': %v", name, err),
					}
				}
				content := string(dataBytes)

				var warnings []string
				contentLower := strings.ToLower(content)
				injectionDetected := false
				for _, p := range SkillGuardThreatPatterns {
					if p.Regex.MatchString(contentLower) {
						injectionDetected = true
						break
					}
				}
				if injectionDetected {
					warnings = append(warnings, "skill content contains patterns that may indicate prompt injection")
				}

				fm, body := ParseFrontmatter(content)
				if fm == nil {
					fm = make(map[string]interface{})
				}

				if !skillMatchesPlatform(fm) {
					return SkillViewResult{
						Success:         false,
						Error:           fmt.Sprintf("Skill '%s' is not supported on this platform.", name),
						ReadinessStatus: ReadinessUnsupported,
					}
				}

				desc, _ := fm["description"].(string)

				pluginSkillDir := filepath.Dir(pluginSkillMd)
				processedBody := body
				if preprocess {
					processedBody = PreprocessSkillContent(body, pluginSkillDir, sessionId, cfg.Skills.InlineShell, cfg.Skills.InlineShellTimeout)
				}

				banner := plugins.GetPluginSiblingBanner(ns, bare)

				requiredEnvVars := getRequiredEnvironmentVariables(fm)
				envMap := LoadEnoughEnv()

				var missingRequiredEnvVars []string
				for _, envVar := range requiredEnvVars {
					if !envVar.Optional && !isEnvVarSet(envVar.Name, envMap) {
						missingRequiredEnvVars = append(missingRequiredEnvVars, envVar.Name)
					}
				}

				setupNeeded := len(missingRequiredEnvVars) > 0
				readinessStatus := ReadinessAvailable
				if setupNeeded {
					readinessStatus = ReadinessSetupNeeded
				}

				setupObj := normalizeSetupMetadata(fm)

				tags := extractSkillTags(fm)
				related := extractRelatedSkills(fm)

				if filePath != "" {
					if hasTraversalComponent(filePath) {
						return SkillViewResult{Success: false, Error: "Path traversal ('..') is not allowed.", Hint: "Use a relative path within the skill directory"}
					}
					travErr := validateWithinDir(filePath, pluginSkillDir)
					if travErr != "" {
						return SkillViewResult{Success: false, Error: travErr, Hint: "Use a relative path within the skill directory"}
					}

					targetFile := filepath.Join(pluginSkillDir, filePath)
					if _, err := os.Stat(targetFile); os.IsNotExist(err) {
						return SkillViewResult{
							Success:     false,
							Error:       fmt.Sprintf("File '%s' not found in skill '%s'.", filePath, name),
							LinkedFiles: scanLinkedFiles(pluginSkillDir),
						}
					}

					fileData, err := os.ReadFile(targetFile)
					if err != nil {
						return SkillViewResult{Success: false, Error: fmt.Sprintf("Failed to read file: %v", err)}
					}

					return SkillViewResult{
						Success:  true,
						Name:     name,
						File:     filePath,
						Content:  string(fileData),
						SkillDir: pluginSkillDir,
						Warnings: warnings,
					}
				}

				return SkillViewResult{
					Success:                             true,
					Name:                                name,
					Description:                         desc,
					Category:                            ns,
					Content:                             banner + processedBody,
					RawContent:                          body,
					SkillDir:                            pluginSkillDir,
					LinkedFiles:                         scanLinkedFiles(pluginSkillDir),
					Tags:                                tags,
					RelatedSkills:                       related,
					Warnings:                            warnings,
					ReadinessStatus:                     readinessStatus,
					RequiredEnvironmentVariables:        requiredEnvVars,
					MissingRequiredEnvironmentVariables: missingRequiredEnvVars,
					SetupNeeded:                         setupNeeded,
					Setup:                               &setupObj,
				}
			}
		}
	}

	candidates := findSkillCandidates(name, workDir, cfg)

	if len(candidates) > 1 {
		var paths []string
		for _, c := range candidates {
			paths = append(paths, c.skillMd)
		}
		return SkillViewResult{
			Success: false,
			Error:   fmt.Sprintf("Ambiguous skill name '%s': %d skills match. Refusing to guess.", name, len(candidates)),
			Matches: paths,
			Hint:    "Pass the full relative path instead of the bare name (e.g., 'category/skill-name').",
		}
	}

	if len(candidates) == 0 {
		return SkillViewResult{
			Success: false,
			Error:   fmt.Sprintf("Skill '%s' not found.", name),
			Hint:    "Use skills_list to see all available skills",
		}
	}

	candidate := candidates[0]
	skillDir := candidate.skillDir
	skillMd := candidate.skillMd
	resolvedSkillDir := skillDir
	if resolvedSkillDir == "" {
		resolvedSkillDir = filepath.Dir(skillMd)
	}

	dataBytes, err := os.ReadFile(skillMd)
	if err != nil {
		return SkillViewResult{Success: false, Error: fmt.Sprintf("Failed to read skill '%s': %v", name, err)}
	}
	content := string(dataBytes)

	// Security checks: traversal outside trusted
	var warnings []string
	outsideTrusted := true
	dirs := SearchLocations(workDir, cfg, "")
	for _, dir := range dirs {
		if isPathWithinDir(skillMd, dir.Path) {
			outsideTrusted = false
			break
		}
	}

	if outsideTrusted {
		warnings = append(warnings, fmt.Sprintf("skill file is outside the trusted skills directory (%s): %s", SkillsDir(), skillMd))
	}

	// Check injection
	contentLower := strings.ToLower(content)
	injectionDetected := false
	for _, p := range SkillGuardThreatPatterns {
		if p.Regex.MatchString(contentLower) {
			injectionDetected = true
			break
		}
	}
	if injectionDetected {
		warnings = append(warnings, "skill content contains patterns that may indicate prompt injection")
	}

	fm, body := ParseFrontmatter(content)
	if fm == nil {
		fm = make(map[string]interface{})
	}

	if !skillMatchesPlatform(fm) {
		return SkillViewResult{
			Success:         false,
			Error:           fmt.Sprintf("Skill '%s' is not supported on this platform.", name),
			ReadinessStatus: ReadinessUnsupported,
		}
	}

	resolvedName, _ := fm["name"].(string)
	if resolvedName == "" {
		resolvedName = filepath.Base(resolvedSkillDir)
	}

	if IsSkillDisabled(resolvedName, cfg) {
		return SkillViewResult{Success: false, Error: fmt.Sprintf("Skill '%s' is disabled.", resolvedName)}
	}

	// Read supporting file if file_path is specified
	if filePath != "" && skillDir != "" {
		if hasTraversalComponent(filePath) {
			return SkillViewResult{Success: false, Error: "Path traversal ('..') is not allowed.", Hint: "Use a relative path within the skill directory"}
		}
		travErr := validateWithinDir(filePath, skillDir)
		if travErr != "" {
			return SkillViewResult{Success: false, Error: travErr, Hint: "Use a relative path within the skill directory"}
		}

		targetFile := filepath.Join(skillDir, filePath)
		if _, err := os.Stat(targetFile); os.IsNotExist(err) {
			return SkillViewResult{
				Success:     false,
				Error:       fmt.Sprintf("File '%s' not found in skill '%s'.", filePath, name),
				LinkedFiles: scanLinkedFiles(skillDir),
			}
		}

		fileData, err := os.ReadFile(targetFile)
		if err != nil {
			return SkillViewResult{Success: false, Error: fmt.Sprintf("Failed to read file: %v", err)}
		}

		return SkillViewResult{
			Success:  true,
			Name:     resolvedName,
			File:     filePath,
			Content:  string(fileData),
			SkillDir: skillDir,
			Warnings: warnings,
		}
	}

	processedBody := body
	if preprocess {
		processedBody = PreprocessSkillContent(body, resolvedSkillDir, sessionId, cfg.Skills.InlineShell, cfg.Skills.InlineShellTimeout)
	}

	linkedFiles := scanLinkedFiles(resolvedSkillDir)
	tags := extractSkillTags(fm)
	related := extractRelatedSkills(fm)

	desc, _ := fm["description"].(string)

	usageHint := ""
	if len(linkedFiles) > 0 {
		usageHint = "To view linked files, call skill_view(name, file_path) where file_path is e.g. 'references/api.md'"
	}

	requiredEnvVars := getRequiredEnvironmentVariables(fm)
	envMap := LoadEnoughEnv()

	var missingRequiredEnvVars []string
	for _, envVar := range requiredEnvVars {
		if !envVar.Optional && !isEnvVarSet(envVar.Name, envMap) {
			missingRequiredEnvVars = append(missingRequiredEnvVars, envVar.Name)
		}
	}

	setupNeeded := len(missingRequiredEnvVars) > 0
	readinessStatus := ReadinessAvailable
	if setupNeeded {
		readinessStatus = ReadinessSetupNeeded
	}

	setupObj := normalizeSetupMetadata(fm)

	return SkillViewResult{
		Success:                             true,
		Name:                                resolvedName,
		Description:                         desc,
		Category:                            computeSkillCategory(skillMd, SkillsDir()),
		Content:                             processedBody,
		RawContent:                          body,
		SkillDir:                            resolvedSkillDir,
		LinkedFiles:                         linkedFiles,
		Tags:                                tags,
		RelatedSkills:                       related,
		Warnings:                            warnings,
		UsageHint:                           usageHint,
		ReadinessStatus:                     readinessStatus,
		RequiredEnvironmentVariables:        requiredEnvVars,
		MissingRequiredEnvironmentVariables: missingRequiredEnvVars,
		SetupNeeded:                         setupNeeded,
		Setup:                               &setupObj,
	}
}

func ExecuteSkillView(argsJSON string, workDir string, cfg config.Runtime, sessionId string) (string, bool) {
	var args struct {
		Name     string `json:"name"`
		FilePath string `json:"file_path"`
	}
	_ = json.Unmarshal([]byte(argsJSON), &args)

	result := executeSkillViewInternal(args.Name, args.FilePath, workDir, cfg, sessionId, true)
	if result.Success {
		resolved := result.Name
		if resolved == "" {
			resolved = args.Name
		}
		BumpView(resolved)
		BumpUse(resolved)
	}

	outBytes, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return `{"success": false, "error": "json marshal error"}`, true
	}
	return string(outBytes), !result.Success
}
