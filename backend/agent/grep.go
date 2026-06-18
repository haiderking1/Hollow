package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/enough/enough/backend/opencode"
)

func grepTool() opencode.Tool {
	return opencode.Tool{
		Type: "function",
		Function: opencode.ToolFunction{
			Name: "grep",
			Description: "Search file contents using a regular expression. Returns matching lines as " +
				"path:line:text, workspace-relative. Use the optional include glob to limit the file " +
				"types searched (e.g. \"*.go\"). Prefer this over bash grep for searching the codebase.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"pattern": {"type": "string", "description": "Regular expression to search for"},
					"path": {"type": "string", "description": "Directory to search in (default workspace root)"},
					"include": {"type": "string", "description": "Glob to filter files, e.g. *.go"}
				},
				"required": ["pattern"]
			}`),
		},
	}
}

const (
	maxGrepMatches  = 200
	maxGrepFileSize = 2_000_000
)

func (a *Agent) toolGrep(ctx context.Context, argsJSON string) toolResult {
	var args struct {
		Pattern string `json:"pattern"`
		Path    string `json:"path"`
		Include string `json:"include"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return toolResult{output: err.Error(), isErr: true}
	}
	if strings.TrimSpace(args.Pattern) == "" {
		return toolResult{output: "pattern is required", isErr: true}
	}

	re, err := regexp.Compile(args.Pattern)
	if err != nil {
		return toolResult{output: fmt.Sprintf("invalid pattern: %v", err), isErr: true}
	}

	root := args.Path
	if root == "" {
		root = "."
	}
	rootAbs, err := a.resolvePath(root)
	if err != nil {
		return toolResult{output: err.Error(), isErr: true}
	}

	var out []string
	truncated := false
	walkErr := filepath.WalkDir(rootAbs, func(p string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if shouldSkipDir(d.Name()) && p != rootAbs {
				return filepath.SkipDir
			}
			return nil
		}
		if args.Include != "" {
			if ok, _ := filepath.Match(args.Include, d.Name()); !ok {
				return nil
			}
		}
		info, infoErr := d.Info()
		if infoErr != nil || info.Size() > maxGrepFileSize {
			return nil
		}

		f, openErr := os.Open(p)
		if openErr != nil {
			return nil
		}
		defer f.Close()

		rel, relErr := filepath.Rel(a.workDir, p)
		if relErr != nil {
			rel = p
		}
		rel = filepath.ToSlash(rel)

		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		lineNo := 0
		for scanner.Scan() {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			lineNo++
			line := scanner.Text()
			if re.MatchString(line) {
				out = append(out, fmt.Sprintf("%s:%d:%s", rel, lineNo, strings.TrimSpace(line)))
				if len(out) >= maxGrepMatches {
					truncated = true
					return fs.SkipAll
				}
			}
		}
		return nil
	})
	if walkErr != nil {
		if ctx.Err() != nil {
			return toolResult{output: "[interrupted]", isErr: true}
		}
		return toolResult{output: walkErr.Error(), isErr: true}
	}

	if len(out) == 0 {
		return toolResult{output: "no matches"}
	}
	res := strings.Join(out, "\n")
	if truncated {
		res += fmt.Sprintf("\n... truncated at %d matches ...", maxGrepMatches)
	}
	return toolResult{output: res}
}
