package skills

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/enough/enough/backend/config"
)

func withFileLock(path string, f func() (SkillManageResult, error)) (SkillManageResult, error) {
	lockPath := path + ".lock"
	_ = os.MkdirAll(filepath.Dir(lockPath), 0o700)

	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return SkillManageResult{Success: false, Error: "failed to create lock file: " + err.Error()}, nil
	}
	defer func() {
		_ = file.Close()
		_ = os.Remove(lockPath)
	}()

	err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX)
	if err != nil {
		return SkillManageResult{Success: false, Error: "failed to acquire file lock: " + err.Error()}, nil
	}
	defer func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	}()

	return f()
}

func validateManageName(name string) string {
	if name == "" {
		return "Skill name is required."
	}
	if len(name) > MaxSkillNameLength {
		return fmt.Sprintf("Skill name exceeds %d characters.", MaxSkillNameLength)
	}
	if !SkillManageNameRe.MatchString(name) {
		return fmt.Sprintf("Invalid skill name '%s'. Use lowercase letters, numbers, hyphens, dots, and underscores. Must start with a letter or digit.", name)
	}
	return ""
}

func validateCategory(category string) string {
	trimmed := strings.TrimSpace(category)
	if trimmed == "" {
		return ""
	}
	if strings.Contains(trimmed, "/") || strings.Contains(trimmed, "\\") {
		return fmt.Sprintf("Invalid category '%s'. Use lowercase letters, numbers, hyphens, dots, and underscores. Categories must be a single directory name.", category)
	}
	if len(trimmed) > MaxSkillNameLength {
		return fmt.Sprintf("Category exceeds %d characters.", MaxSkillNameLength)
	}
	if !SkillManageNameRe.MatchString(trimmed) {
		return fmt.Sprintf("Invalid category '%s'. Use lowercase letters, numbers, hyphens, dots, and underscores. Categories must be a single directory name.", category)
	}
	return ""
}

func validateFrontmatter(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return "Content cannot be empty."
	}
	if !strings.HasPrefix(trimmed, "---") {
		return "SKILL.md must start with YAML frontmatter (---). See existing skills for format."
	}

	fm, body := ParseFrontmatter(content)
	if fm == nil {
		return "SKILL.md frontmatter is not closed. Ensure you have a closing '---' line."
	}

	name, _ := fm["name"].(string)
	if name == "" {
		return "Frontmatter must include 'name' field."
	}
	desc, _ := fm["description"].(string)
	if desc == "" {
		return "Frontmatter must include 'description' field."
	}
	if len(desc) > MaxSkillDescriptionLength {
		return fmt.Sprintf("Description exceeds %d characters.", MaxSkillDescriptionLength)
	}

	if strings.TrimSpace(body) == "" {
		return "SKILL.md must have content after the frontmatter (instructions, procedures, etc.)."
	}

	return ""
}

func validateContentSize(content, label string) string {
	if len(content) > MaxSkillContentChars {
		return fmt.Sprintf("%s content is %s characters (limit: %s). Consider splitting into a smaller SKILL.md with supporting files in references/ or templates/.",
			label, formatNumber(len(content)), formatNumber(MaxSkillContentChars))
	}
	return ""
}

func formatNumber(n int) string {
	// A simple comma formatter
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	if len(s) > 0 {
		parts = append([]string{s}, parts...)
	}
	return strings.Join(parts, ",")
}

func FindSkillDirectory(name string) string {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = ""
	}
	cfg, err := config.LoadRuntime()
	if err != nil {
		cfg = config.Runtime{}
	}
	dirs := SearchLocations(cwd, cfg, "")
	for _, dir := range dirs {
		for _, skillFile := range IterSkillIndexFiles(dir.Path, "SKILL.md") {
			if isExcludedSkillPath(skillFile) {
				continue
			}
			if filepath.Base(filepath.Dir(skillFile)) == name {
				return filepath.Dir(skillFile)
			}
			// Check frontmatter name
			data, err := os.ReadFile(skillFile)
			if err == nil {
				fm, _ := ParseFrontmatter(string(data))
				if fm != nil {
					if nameVal, ok := fm["name"].(string); ok && nameVal == name {
						return filepath.Dir(skillFile)
					}
				}
			}
		}
	}
	return ""
}

func resolveSkillDir(name, category string) string {
	skillsDir := SkillsDir()
	trimmedCat := strings.TrimSpace(category)
	if trimmedCat != "" {
		return filepath.Join(skillsDir, trimmedCat, name)
	}
	return filepath.Join(skillsDir, name)
}

func skillNotFoundError(name string, suffix string) string {
	base := fmt.Sprintf("Skill '%s' not found. Use skills_list() to see available skills.", name)
	if suffix != "" {
		base += suffix
	}
	return base
}

func validateFilePath(filePath string) string {
	if filePath == "" {
		return "file_path is required."
	}
	if hasTraversalComponent(filePath) {
		return "Path traversal ('..') is not allowed."
	}
	// Split path
	parts := strings.FieldsFunc(filePath, func(r rune) bool {
		return r == '/' || r == '\\'
	})
	if len(parts) == 0 || !AllowedSkillSubdirs[parts[0]] {
		var allowed []string
		for s := range AllowedSkillSubdirs {
			allowed = append(allowed, s)
		}
		sort.Strings(allowed)
		return fmt.Sprintf("File must be under one of: %s. Got: '%s'", strings.Join(allowed, ", "), filePath)
	}
	if len(parts) < 2 {
		return fmt.Sprintf("Provide a file path, not just a directory. Example: '%s/myfile.md'", parts[0])
	}
	return ""
}

func resolveSkillTarget(skillDir, filePath string) (string, string) {
	target := filepath.Join(skillDir, filePath)
	err := validateWithinDir(target, skillDir)
	if err != "" {
		return "", err
	}
	return target, ""
}

func normalizeForFuzzyMatch(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t\r")
	}
	text = strings.Join(lines, "\n")
	text = strings.NewReplacer(
		"\u2018", "'", "\u2019", "'", "\u201A", "'", "\u201B", "'",
		"\u201C", "\"", "\u201D", "\"", "\u201E", "\"", "\u201F", "\"",
		"\u2010", "-", "\u2011", "-", "\u2012", "-", "\u2013", "-", "\u2014", "-", "\u2015", "-", "\u2212", "-",
		"\u00A0", " ", "\u202F", " ", "\u205F", " ", "\u3000", " ",
	).Replace(text)

	var sb strings.Builder
	for _, r := range text {
		if r >= 8194 && r <= 8202 {
			sb.WriteRune(' ')
		} else {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

type fuzzyMatchResult struct {
	found                 bool
	index                 int
	matchLength           int
	usedFuzzy             bool
	contentForReplacement string
}

func fuzzyFindText(content, oldText string) fuzzyMatchResult {
	idx := strings.Index(content, oldText)
	if idx != -1 {
		return fuzzyMatchResult{
			found:                 true,
			index:                 idx,
			matchLength:           len(oldText),
			usedFuzzy:             false,
			contentForReplacement: content,
		}
	}

	fuzzyContent := normalizeForFuzzyMatch(content)
	fuzzyOldText := normalizeForFuzzyMatch(oldText)
	fuzzyIdx := strings.Index(fuzzyContent, fuzzyOldText)

	if fuzzyIdx == -1 {
		return fuzzyMatchResult{
			found:                 false,
			index:                 -1,
			matchLength:           0,
			usedFuzzy:             false,
			contentForReplacement: content,
		}
	}

	return fuzzyMatchResult{
		found:                 true,
		index:                 fuzzyIdx,
		matchLength:           len(fuzzyOldText),
		usedFuzzy:             true,
		contentForReplacement: fuzzyContent,
	}
}

func fuzzyFindAndReplace(content, oldString, newString string, replaceAll bool) (string, int, string, error) {
	fuzzyContent := normalizeForFuzzyMatch(content)
	fuzzyOld := normalizeForFuzzyMatch(oldString)

	occurrenceCount := strings.Count(fuzzyContent, fuzzyOld)
	if occurrenceCount == 0 {
		preview := content
		if len(preview) > 500 {
			preview = preview[:500] + "..."
		}
		return content, 0, preview, errors.New("Could not find the text to replace. The old_string must match (fuzzy matching is applied).")
	}

	if !replaceAll && occurrenceCount > 1 {
		return content, occurrenceCount, "", fmt.Errorf("Found %d occurrences of the text. The text must be unique unless replace_all=true.", occurrenceCount)
	}

	match := fuzzyFindText(content, oldString)
	if !match.found {
		return content, 0, "", errors.New("Could not find the text to replace.")
	}

	base := match.contentForReplacement
	matchCount := 0

	if replaceAll {
		parts := strings.Split(base, fuzzyOld)
		matchCount = len(parts) - 1
		base = strings.Join(parts, newString)
	} else {
		base = base[:match.index] + newString + base[match.index+match.matchLength:]
		matchCount = 1
	}

	if base == match.contentForReplacement && newString == oldString {
		return content, 0, "", errors.New("No changes made. The replacement produced identical content.")
	}

	return base, matchCount, "", nil
}

func writeTextAtomic(filePath, content string, guardEnabled bool, skillDir string) string {
	err := atomicWrite(filePath, []byte(content))
	if err != nil {
		return "Failed to write file atomically: " + err.Error()
	}
	return SecurityScanSkillDir(skillDir, guardEnabled)
}

func pruneEmptyCategoryDir(skillDir string) {
	skillsRoot := filepath.Clean(SkillsDir())
	parent := filepath.Dir(skillDir)
	if filepath.Clean(parent) == skillsRoot {
		return
	}
	if _, err := os.Stat(parent); os.IsNotExist(err) {
		return
	}
	// Read directory
	files, err := os.ReadDir(parent)
	if err == nil && len(files) == 0 {
		_ = os.Remove(parent)
	}
}

func invalidateCache() {
	ClearSkillsPromptCache()
}

func createSkill(name, content, category string, guardEnabled bool) (SkillManageResult, error) {
	nameErr := validateManageName(name)
	if nameErr != "" {
		return SkillManageResult{Success: false, Error: nameErr}, nil
	}
	catErr := validateCategory(category)
	if catErr != "" {
		return SkillManageResult{Success: false, Error: catErr}, nil
	}
	if content == "" {
		return SkillManageResult{Success: false, Error: "content is required for 'create'. Provide the full SKILL.md text (frontmatter + body)."}, nil
	}
	fmErr := validateFrontmatter(content)
	if fmErr != "" {
		return SkillManageResult{Success: false, Error: fmErr}, nil
	}
	sizeErr := validateContentSize(content, "SKILL.md")
	if sizeErr != "" {
		return SkillManageResult{Success: false, Error: sizeErr}, nil
	}

	existing := FindSkillDirectory(name)
	if existing != "" {
		return SkillManageResult{Success: false, Error: fmt.Sprintf("A skill named '%s' already exists at %s.", name, existing)}, nil
	}

	skillDir := resolveSkillDir(name, category)
	skillMd := filepath.Join(skillDir, "SKILL.md")

	return withFileLock(skillMd, func() (SkillManageResult, error) {
		_ = os.MkdirAll(skillDir, 0o700)
		scanErr := writeTextAtomic(skillMd, content, guardEnabled, skillDir)
		if scanErr != "" {
			_ = os.RemoveAll(skillDir)
			return SkillManageResult{Success: false, Error: scanErr}, nil
		}
		invalidateCache()
		relPath, err := filepath.Rel(SkillsDir(), skillDir)
		if err != nil {
			relPath = skillDir
		}
		relPath = filepath.ToSlash(relPath)
		result := SkillManageResult{
			Success: true,
			Message: fmt.Sprintf("Skill '%s' created.", name),
			Path:    relPath,
			SkillMd: skillMd,
			Hint:    fmt.Sprintf("To add reference files, templates, or scripts, use skill_manage(action='write_file', name='%s', file_path='references/example.md', file_content='...')", name),
		}
		if strings.TrimSpace(category) != "" {
			result.Category = strings.TrimSpace(category)
		}
		return result, nil
	})
}

func editSkill(name, content string, guardEnabled bool) (SkillManageResult, error) {
	if content == "" {
		return SkillManageResult{Success: false, Error: "content is required for 'edit'. Provide the full updated SKILL.md text."}, nil
	}
	fmErr := validateFrontmatter(content)
	if fmErr != "" {
		return SkillManageResult{Success: false, Error: fmErr}, nil
	}
	sizeErr := validateContentSize(content, "SKILL.md")
	if sizeErr != "" {
		return SkillManageResult{Success: false, Error: sizeErr}, nil
	}

	skillDir := FindSkillDirectory(name)
	if skillDir == "" {
		return SkillManageResult{Success: false, Error: skillNotFoundError(name, "")}, nil
	}

	skillMd := filepath.Join(skillDir, "SKILL.md")
	return withFileLock(skillMd, func() (SkillManageResult, error) {
		var originalContent []byte
		if fi, err := os.Stat(skillMd); err == nil && !fi.IsDir() {
			originalContent, _ = os.ReadFile(skillMd)
		}
		scanErr := writeTextAtomic(skillMd, content, guardEnabled, skillDir)
		if scanErr != "" {
			if originalContent != nil {
				_ = atomicWrite(skillMd, originalContent)
			}
			return SkillManageResult{Success: false, Error: scanErr}, nil
		}
		invalidateCache()
		return SkillManageResult{
			Success: true,
			Message: fmt.Sprintf("Skill '%s' updated.", name),
			Path:    skillDir,
		}, nil
	})
}

func patchSkill(name, oldString, newString, filePath string, replaceAll bool, guardEnabled bool) (SkillManageResult, error) {
	if oldString == "" {
		return SkillManageResult{Success: false, Error: "old_string is required for 'patch'."}, nil
	}
	if newString == "" && newString != "" { // just validation that newString is specified, in Go it is a string so it's always non-nil, but we check empty vs non-empty? The TS check was: newString === undefined || newString === null.
		return SkillManageResult{Success: false, Error: "new_string is required for 'patch'."}, nil
	}

	skillDir := FindSkillDirectory(name)
	if skillDir == "" {
		return SkillManageResult{Success: false, Error: skillNotFoundError(name, "")}, nil
	}

	var target string
	if filePath != "" {
		pathErr := validateFilePath(filePath)
		if pathErr != "" {
			return SkillManageResult{Success: false, Error: pathErr}, nil
		}
		var err string
		target, err = resolveSkillTarget(skillDir, filePath)
		if err != "" {
			return SkillManageResult{Success: false, Error: err}, nil
		}
	} else {
		target = filepath.Join(skillDir, "SKILL.md")
	}

	if _, err := os.Stat(target); os.IsNotExist(err) {
		rel, _ := filepath.Rel(skillDir, target)
		return SkillManageResult{Success: false, Error: fmt.Sprintf("File not found: %s", filepath.ToSlash(rel))}, nil
	}

	return withFileLock(target, func() (SkillManageResult, error) {
		data, err := os.ReadFile(target)
		if err != nil {
			return SkillManageResult{Success: false, Error: "Failed to read target file: " + err.Error()}, nil
		}
		content := string(data)

		newContent, matchCount, filePreview, replaceErr := fuzzyFindAndReplace(content, oldString, newString, replaceAll)
		if replaceErr != nil {
			res := SkillManageResult{Success: false, Error: replaceErr.Error()}
			if filePreview != "" {
				res.FilePreview = filePreview
			}
			return res, nil
		}

		targetLabel := filePath
		if targetLabel == "" {
			targetLabel = "SKILL.md"
		}
		sizeErr := validateContentSize(newContent, targetLabel)
		if sizeErr != "" {
			return SkillManageResult{Success: false, Error: sizeErr}, nil
		}

		if filePath == "" {
			fmErr := validateFrontmatter(newContent)
			if fmErr != "" {
				return SkillManageResult{Success: false, Error: fmt.Sprintf("Patch would break SKILL.md structure: %s", fmErr)}, nil
			}
		}

		scanErr := writeTextAtomic(target, newContent, guardEnabled, skillDir)
		if scanErr != "" {
			return SkillManageResult{Success: false, Error: scanErr}, nil
		}
		invalidateCache()

		plural := ""
		if matchCount > 1 {
			plural = "s"
		}
		return SkillManageResult{
			Success: true,
			Message: fmt.Sprintf("Patched %s in skill '%s' (%d replacement%s).", targetLabel, name, matchCount, plural),
		}, nil
	})
}

func pinnedGuard(name string) string {
	um := LoadUsage()
	if rec, ok := um[name]; ok && rec.Pinned {
		return fmt.Sprintf("Skill '%s' is pinned and cannot be deleted by skill_manage. Ask the user to run `enough curator unpin %s` if they want to delete it. Patches and edits are allowed on pinned skills; only deletion is blocked.", name, name)
	}
	return ""
}

// archiveDeleteSkill is the autonomous-pass variant of delete: the skill
// directory is moved to .archive/ (recoverable) instead of removed, and the
// curator-protected builtins are refused outright.
func archiveDeleteSkill(name, absorbedInto string) (SkillManageResult, error) {
	if IsProtectedBuiltin(name) {
		return SkillManageResult{Success: false, Error: fmt.Sprintf("Skill '%s' is a protected built-in and cannot be archived or deleted by an autonomous pass.", name)}, nil
	}

	if pinErr := pinnedGuard(name); pinErr != "" {
		return SkillManageResult{Success: false, Error: pinErr}, nil
	}

	trimmedAbsorbed := strings.TrimSpace(absorbedInto)
	if trimmedAbsorbed != "" {
		if trimmedAbsorbed == name {
			return SkillManageResult{Success: false, Error: fmt.Sprintf("absorbed_into='%s' cannot equal the skill being deleted.", trimmedAbsorbed)}, nil
		}
		if FindSkillDirectory(trimmedAbsorbed) == "" {
			return SkillManageResult{Success: false, Error: fmt.Sprintf("absorbed_into='%s' does not exist. Create or patch the umbrella skill first, then retry the delete.", trimmedAbsorbed)}, nil
		}
	}

	ok, msg := ArchiveSkill(name)
	if !ok {
		return SkillManageResult{Success: false, Error: msg}, nil
	}
	if IsBundledSkillName(name) {
		MarkSuppressed(name)
	}
	invalidateCache()

	out := fmt.Sprintf("Skill '%s' archived (%s).", name, msg)
	if trimmedAbsorbed != "" {
		out += fmt.Sprintf(" Content absorbed into '%s'.", trimmedAbsorbed)
	}
	return SkillManageResult{Success: true, Message: out}, nil
}

func deleteSkill(name, absorbedInto string, _guardEnabled bool) (SkillManageResult, error) {
	skillDir := FindSkillDirectory(name)
	if skillDir == "" {
		return SkillManageResult{Success: false, Error: skillNotFoundError(name, "")}, nil
	}

	if pinErr := pinnedGuard(name); pinErr != "" {
		return SkillManageResult{Success: false, Error: pinErr}, nil
	}

	trimmedAbsorbed := strings.TrimSpace(absorbedInto)
	if trimmedAbsorbed != "" {
		if trimmedAbsorbed == name {
			return SkillManageResult{Success: false, Error: fmt.Sprintf("absorbed_into='%s' cannot equal the skill being deleted.", trimmedAbsorbed)}, nil
		}
		target := FindSkillDirectory(trimmedAbsorbed)
		if target == "" {
			return SkillManageResult{Success: false, Error: fmt.Sprintf("absorbed_into='%s' does not exist. Create or patch the umbrella skill first, then retry the delete.", trimmedAbsorbed)}, nil
		}
	}

	skillMd := filepath.Join(skillDir, "SKILL.md")
	return withFileLock(skillMd, func() (SkillManageResult, error) {
		_ = os.RemoveAll(skillDir)
		pruneEmptyCategoryDir(skillDir)
		invalidateCache()

		msg := fmt.Sprintf("Skill '%s' deleted.", name)
		if trimmedAbsorbed != "" {
			msg += fmt.Sprintf(" Content absorbed into '%s'.", trimmedAbsorbed)
		}
		return SkillManageResult{Success: true, Message: msg}, nil
	})
}

func writeSkillFile(name, filePath, fileContent string, guardEnabled bool) (SkillManageResult, error) {
	pathErr := validateFilePath(filePath)
	if pathErr != "" {
		return SkillManageResult{Success: false, Error: pathErr}, nil
	}

	contentBytes := len([]byte(fileContent))
	if contentBytes > MaxSkillFileBytes {
		return SkillManageResult{Success: false, Error: fmt.Sprintf("File content is %s bytes (limit: %s bytes / 1 MiB). Consider splitting into smaller files.", formatNumber(contentBytes), formatNumber(MaxSkillFileBytes))}, nil
	}

	sizeErr := validateContentSize(fileContent, filePath)
	if sizeErr != "" {
		return SkillManageResult{Success: false, Error: sizeErr}, nil
	}

	skillDir := FindSkillDirectory(name)
	if skillDir == "" {
		return SkillManageResult{Success: false, Error: skillNotFoundError(name, " Create it first with action='create'.")}, nil
	}

	target, err := resolveSkillTarget(skillDir, filePath)
	if err != "" {
		return SkillManageResult{Success: false, Error: err}, nil
	}

	return withFileLock(target, func() (SkillManageResult, error) {
		_ = os.MkdirAll(filepath.Dir(target), 0o700)
		var originalContent []byte
		if fi, err := os.Stat(target); err == nil && !fi.IsDir() {
			originalContent, _ = os.ReadFile(target)
		}

		scanErr := writeTextAtomic(target, fileContent, guardEnabled, skillDir)
		if scanErr != "" {
			if originalContent != nil {
				_ = atomicWrite(target, originalContent)
			} else {
				_ = os.Remove(target)
			}
			return SkillManageResult{Success: false, Error: scanErr}, nil
		}
		invalidateCache()
		return SkillManageResult{
			Success: true,
			Message: fmt.Sprintf("File '%s' written to skill '%s'.", filePath, name),
			Path:    target,
		}, nil
	})
}

func removeSkillFile(name, filePath string) (SkillManageResult, error) {
	pathErr := validateFilePath(filePath)
	if pathErr != "" {
		return SkillManageResult{Success: false, Error: pathErr}, nil
	}

	skillDir := FindSkillDirectory(name)
	if skillDir == "" {
		return SkillManageResult{Success: false, Error: skillNotFoundError(name, "")}, nil
	}

	target, err := resolveSkillTarget(skillDir, filePath)
	if err != "" {
		return SkillManageResult{Success: false, Error: err}, nil
	}

	if _, statErr := os.Stat(target); os.IsNotExist(statErr) {
		var available []string
		var collectFiles func(string, string)
		collectFiles = func(dir, base string) {
			entries, err := os.ReadDir(dir)
			if err != nil {
				return
			}
			for _, entry := range entries {
				full := filepath.Join(dir, entry.Name())
				if entry.IsDir() {
					collectFiles(full, base)
				} else {
					rel, relErr := filepath.Rel(base, full)
					if relErr == nil {
						available = append(available, filepath.ToSlash(rel))
					}
				}
			}
		}
		for subdir := range AllowedSkillSubdirs {
			d := filepath.Join(skillDir, subdir)
			if _, err := os.Stat(d); err == nil {
				collectFiles(d, skillDir)
			}
		}
		sort.Strings(available)

		return SkillManageResult{
			Success:        false,
			Error:          fmt.Sprintf("File '%s' not found in skill '%s'.", filePath, name),
			AvailableFiles: available,
		}, nil
	}

	return withFileLock(target, func() (SkillManageResult, error) {
		_ = os.Remove(target)
		parent := filepath.Dir(target)
		if filepath.Clean(parent) != filepath.Clean(skillDir) {
			files, err := os.ReadDir(parent)
			if err == nil && len(files) == 0 {
				_ = os.RemoveAll(parent)
			}
		}
		invalidateCache()
		return SkillManageResult{
			Success: true,
			Message: fmt.Sprintf("File '%s' removed from skill '%s'.", filePath, name),
		}, nil
	})
}
