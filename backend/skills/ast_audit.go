package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type ASTFinding struct {
	File        string `json:"file"`
	Line        int    `json:"line"`
	PatternID   string `json:"pattern_id"`
	Description string `json:"description"`
}

var (
	rxDynamicImport   = regexp.MustCompile(`import_module\s*\(`)
	rxComputedImport  = regexp.MustCompile(`__import__\s*\(\s*[^'"]`)
	rxDynamicGetattr  = regexp.MustCompile(`getattr\s*\(\s*[^,]+,\s*[^'"]`)
	rxDictAccess      = regexp.MustCompile(`__dict__\s*\[\s*[^'"]`)
	rxImportlibImport = regexp.MustCompile(`(?:^|\n)\s*(?:import\s+importlib|from\s+importlib\s+import)`)
)

func scanSource(content, relPath string) []ASTFinding {
	var findings []ASTFinding
	lines := strings.Split(content, "\n")

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if rxDynamicImport.MatchString(line) {
			findings = append(findings, ASTFinding{
				File:        relPath,
				Line:        lineNum,
				PatternID:   "dynamic_import",
				Description: "importlib.import_module() — loads arbitrary modules at runtime",
			})
		}
		if rxComputedImport.MatchString(line) {
			findings = append(findings, ASTFinding{
				File:        relPath,
				Line:        lineNum,
				PatternID:   "dynamic_import_computed",
				Description: "__import__ with non-literal module name",
			})
		}
		if rxDynamicGetattr.MatchString(line) {
			findings = append(findings, ASTFinding{
				File:        relPath,
				Line:        lineNum,
				PatternID:   "dynamic_getattr",
				Description: "getattr with non-literal attribute name",
			})
		}
		if rxDictAccess.MatchString(line) {
			findings = append(findings, ASTFinding{
				File:        relPath,
				Line:        lineNum,
				PatternID:   "dict_access",
				Description: "__dict__[<computed>] — dynamic attribute access",
			})
		}
		if rxImportlibImport.MatchString(line) {
			findings = append(findings, ASTFinding{
				File:        relPath,
				Line:        lineNum,
				PatternID:   "importlib_import",
				Description: "import importlib — enables dynamic module loading",
			})
		}
	}

	return findings
}

func ASTScanPath(path string) ([]ASTFinding, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	if !fi.IsDir() {
		if strings.ToLower(filepath.Ext(path)) != ".py" {
			return nil, nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		return scanSource(string(data), filepath.Base(path)), nil
	}

	var findings []ASTFinding
	ignoredDirs := map[string]bool{
		"__pycache__":   true,
		".venv":         true,
		"venv":          true,
		"node_modules":  true,
		".git":          true,
	}

	err = filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if ignoredDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.ToLower(filepath.Ext(p)) != ".py" {
			return nil
		}
		rel, err := filepath.Rel(path, p)
		if err != nil {
			rel = filepath.Base(p)
		}
		data, err := os.ReadFile(p)
		if err == nil {
			findings = append(findings, scanSource(string(data), filepath.ToSlash(rel))...)
		}
		return nil
	})

	return findings, err
}

func FormatASTReport(findings []ASTFinding, skillName string) string {
	header := "AST deep scan"
	if skillName != "" {
		header = fmt.Sprintf("AST deep scan: %s", skillName)
	}

	if len(findings) == 0 {
		return fmt.Sprintf("%s\n  No dynamic import/access patterns detected.", header)
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})

	var lines []string
	lines = append(lines, header, fmt.Sprintf("  %d finding(s):", len(findings)))

	currentFile := ""
	for _, f := range findings {
		if f.File != currentFile {
			currentFile = f.File
			lines = append(lines, fmt.Sprintf("  %s", f.File))
		}
		lines = append(lines, fmt.Sprintf("    L%d  %s  — %s", f.Line, f.PatternID, f.Description))
	}
	lines = append(lines, "", "  Note: diagnostic hints for human review, not security verdicts.")

	return strings.Join(lines, "\n")
}
