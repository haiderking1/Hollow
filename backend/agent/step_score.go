package agent

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/enough/enough/backend/agent/obligations"
)

var failurePathRE = regexp.MustCompile(`(?:(?:^|\s|\()([\w./-]+\.(?:go|py|rs|js|ts|tsx|jsx|java|c|h|cpp|hpp|rb|php|cs|swift|kt|scala|sql|yaml|yml|toml|json|md|sh))(?::\d+(?::\d+)?)?)|File "([^"]+)"`)

type stepTracker struct {
	lastBashCommand  string
	lastBashFailed   bool
	lastVerifyOutput string
	lastVerifyFailed bool
	failurePaths     []string
}

func (a *Agent) scoreToolStep(name, argsJSON string, result toolResult) *toolResult {
	if a.swarmDepth > 0 || !a.evidenceEnabled() || !a.cfg.Evidence.StepScorerEnabled() {
		a.recordStepOutcome(name, argsJSON, result)
		return nil
	}

	a.mu.Lock()
	tracker := a.step
	a.mu.Unlock()

	switch name {
	case "bash":
		cmd := bashCommandArg(argsJSON)
		if result.isErr && cmd != "" && tracker.lastBashFailed && cmd == tracker.lastBashCommand {
			return &toolResult{
				output: "REJECTED: repeated failing command — change approach or fix the failure site before re-running",
				isErr:  true,
			}
		}
	case "write_file", "edit_file":
		if !tracker.lastVerifyFailed || len(tracker.failurePaths) == 0 {
			break
		}
		path, ok := toolPathArg(argsJSON)
		if !ok {
			break
		}
		abs, err := a.resolvePath(path)
		if err != nil {
			break
		}
		if !editTouchesFailureSite(abs, tracker.failurePaths, a.evidenceLedger().MutatedPaths()) {
			return &toolResult{
				output: "REJECTED: edit does not touch the failure site — last verify failed on: " +
					strings.Join(tracker.failurePaths, ", "),
				isErr: true,
			}
		}
	}

	a.recordStepOutcome(name, argsJSON, result)
	return nil
}

func (a *Agent) recordStepOutcome(name, argsJSON string, result toolResult) {
	if name != "bash" {
		return
	}
	cmd := bashCommandArg(argsJSON)
	if cmd == "" {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	a.step.lastBashCommand = cmd
	a.step.lastBashFailed = result.isErr

	reg := a.obligations
	isVerify := reg != nil && obligations.IsVerifyCommand(cmd, reg.VerifyCommand(), reg.ExtraVerifyCommands())
	if !isVerify {
		return
	}

	if result.isErr {
		a.step.lastVerifyFailed = true
		a.step.lastVerifyOutput = truncateStepOutput(result.output)
		a.step.failurePaths = extractFailurePaths(result.output, a.evidenceLedger().MutatedPaths())
	} else {
		a.step.lastVerifyFailed = false
		a.step.lastVerifyOutput = ""
		a.step.failurePaths = nil
	}
}

func bashCommandArg(argsJSON string) string {
	var args struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return ""
	}
	return strings.TrimSpace(args.Command)
}

func extractFailurePaths(output string, mutated []string) []string {
	seen := map[string]bool{}
	var paths []string
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		paths = append(paths, p)
	}
	for _, m := range failurePathRE.FindAllStringSubmatch(output, -1) {
		add(firstNonEmpty(m[1], m[2]))
	}
	for _, p := range mutated {
		add(filepath.ToSlash(p))
		add(filepath.Base(p))
	}
	return paths
}

func editTouchesFailureSite(editPath string, failurePaths, mutated []string) bool {
	editPath = filepath.Clean(editPath)
	for _, fp := range failurePaths {
		fp = filepath.Clean(fp)
		if fp == editPath || strings.HasSuffix(editPath, fp) || strings.HasSuffix(fp, filepath.Base(editPath)) {
			return true
		}
		if base := filepath.Base(fp); base != "" && strings.Contains(editPath, base) {
			return true
		}
	}
	for _, mp := range mutated {
		if filepath.Clean(mp) == editPath {
			return true
		}
	}
	return false
}

func truncateStepOutput(s string) string {
	const max = 4000
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n... truncated ..."
}
