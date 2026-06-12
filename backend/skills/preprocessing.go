package skills

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"
)

var inlineShellRe = regexp.MustCompile("!`([^`\n]+)`")

type PreprocessingConfig struct {
	TemplateVars       bool
	InlineShell        bool
	InlineShellTimeout int
}

func DefaultInlineShellEnabled() bool {
	if runtime.GOOS == "windows" {
		return os.Getenv("WSL_DISTRO_NAME") != ""
	}
	return runtime.GOOS == "linux"
}

func DefaultPreprocessingConfig() PreprocessingConfig {
	return PreprocessingConfig{
		TemplateVars:       true,
		InlineShell:        DefaultInlineShellEnabled(),
		InlineShellTimeout: 10,
	}
}

func substituteTemplateVars(content, skillDir, sessionId string) string {
	if skillDir != "" {
		content = strings.ReplaceAll(content, "${ENOUGH_SKILL_DIR}", skillDir)
		content = strings.ReplaceAll(content, "${HERMES_SKILL_DIR}", skillDir)
		content = strings.ReplaceAll(content, "${FLAME_SKILL_DIR}", skillDir)
	}
	if sessionId != "" {
		content = strings.ReplaceAll(content, "${ENOUGH_SESSION_ID}", sessionId)
		content = strings.ReplaceAll(content, "${HERMES_SESSION_ID}", sessionId)
		content = strings.ReplaceAll(content, "${FLAME_SESSION_ID}", sessionId)
	}
	return content
}

func runInlineShell(command, cwd string, timeoutSec int) string {
	isWSLWindows := runtime.GOOS == "windows" && os.Getenv("WSL_DISTRO_NAME") != ""
	if runtime.GOOS != "linux" && !isWSLWindows {
		return "[inline-shell error: inline shell execution is supported on Linux only]"
	}

	if timeoutSec <= 0 {
		timeoutSec = 10
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "wsl", "bash", "-c", command)
	} else {
		cmd = exec.CommandContext(ctx, "bash", "-c", command)
	}
	cmd.Dir = cwd

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Sprintf("[inline-shell timeout after %ds: %s]", timeoutSec, command)
		}
		stderrStr := strings.TrimRight(stderr.String(), "\n")
		if stderrStr != "" {
			if len(stderrStr) > InlineShellMaxOutput {
				stderrStr = stderrStr[:InlineShellMaxOutput] + "...[truncated]"
			}
			return stderrStr
		}
		return fmt.Sprintf("[inline-shell error: %v]", err)
	}

	out := strings.TrimRight(stdout.String(), "\n")
	if out == "" && stderr.Len() > 0 {
		out = strings.TrimRight(stderr.String(), "\n")
	}

	if len(out) > InlineShellMaxOutput {
		return out[:InlineShellMaxOutput] + "...[truncated]"
	}

	return out
}

func expandInlineShell(content, skillDir string, timeoutSec int) string {
	if !strings.Contains(content, "!`") {
		return content
	}

	return inlineShellRe.ReplaceAllStringFunc(content, func(match string) string {
		cmd := match[2 : len(match)-1]
		trimmed := strings.TrimSpace(cmd)
		if trimmed == "" {
			return ""
		}
		return runInlineShell(trimmed, skillDir, timeoutSec)
	})
}

func PreprocessSkillContent(content, skillDir, sessionId string, inlineShellEnabled bool, timeoutSec int) string {
	if content == "" {
		return content
	}

	result := substituteTemplateVars(content, skillDir, sessionId)
	if inlineShellEnabled {
		if timeoutSec <= 0 {
			timeoutSec = 10
		}
		result = expandInlineShell(result, skillDir, timeoutSec)
	}
	return result
}
