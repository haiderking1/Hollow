package obligations

import (
	"regexp"
	"strings"
)

var curlCmdRE = regexp.MustCompile(`(?i)\bcurl(?:\s+-[\w-]+)*\s+https?://[^\s"'` + "`" + `]+`)

func ExtractTaskVerifyCommands(prompt string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(cmd string) {
		cmd = strings.TrimSpace(cmd)
		if cmd == "" || seen[cmd] {
			return
		}
		seen[cmd] = true
		out = append(out, cmd)
	}

	for _, m := range curlCmdRE.FindAllString(prompt, -1) {
		add(m)
	}

	parts := strings.Split(prompt, "`")
	for i := 1; i < len(parts); i += 2 {
		part := strings.TrimSpace(parts[i])
		if LooksLikeVerifyCommand(part) {
			add(part)
		}
	}
	return out
}

func LooksLikeVerifyCommand(command string) bool {
	lower := strings.ToLower(strings.TrimSpace(command))
	if lower == "" {
		return false
	}
	if strings.HasPrefix(lower, "curl ") || strings.Contains(lower, " curl ") {
		return true
	}
	for _, prefix := range []string{
		"go test", "pytest", "npm test", "pnpm test", "yarn test",
		"cargo test", "make test", "vitest", "jest",
	} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func IsVerifyCommand(command, verifyCmd string, extra []string) bool {
	if commandMatchesAny(command, verifyCmd, extra) {
		return true
	}
	return LooksLikeVerifyCommand(command)
}

func commandMatchesAny(command, verifyCmd string, extra []string) bool {
	if verifyCmd != "" && commandMatchesPattern(command, verifyCmd) {
		return true
	}
	for _, pattern := range extra {
		if commandMatchesPattern(command, pattern) {
			return true
		}
	}
	return false
}

func commandMatchesPattern(command, pattern string) bool {
	command = strings.TrimSpace(command)
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false
	}
	if strings.Contains(command, pattern) {
		return true
	}
	if url := extractCurlURL(pattern); url != "" && strings.Contains(command, url) {
		return true
	}
	return false
}

func extractCurlURL(command string) string {
	fields := strings.Fields(command)
	for _, f := range fields {
		if strings.HasPrefix(f, "http://") || strings.HasPrefix(f, "https://") {
			return strings.Trim(f, `"'`)
		}
	}
	return ""
}
