package skills

import (
	"os"
	"strings"
)

var knownEnvironments = map[string]bool{
	"kanban": true,
	"docker": true,
	"s6":     true,
}

var envDetectCache = make(map[string]bool)

func detectEnvironment(env string) bool {
	if val, ok := envDetectCache[env]; ok {
		return val
	}

	result := true
	switch env {
	case "kanban":
		if os.Getenv("ENOUGH_KANBAN_TASK") != "" || os.Getenv("ENOUGH_KANBAN_BOARD") != "" ||
			os.Getenv("HERMES_KANBAN_TASK") != "" || os.Getenv("HERMES_KANBAN_BOARD") != "" {
			result = true
		} else {
			result = false
		}
	case "docker":
		result = isContainer()
	case "s6":
		if fi, err := os.Stat("/run/s6"); err == nil && fi.IsDir() {
			result = true
		} else if fi, err := os.Stat("/package/admin/s6-overlay"); err == nil && fi.IsDir() {
			result = true
		} else {
			result = false
		}
	}

	envDetectCache[env] = result
	return result
}

func isContainer() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	if _, err := os.Stat("/run/.containerenv"); err == nil {
		return true
	}
	data, err := os.ReadFile("/proc/1/cgroup")
	if err == nil {
		content := string(data)
		if strings.Contains(content, "docker") || strings.Contains(content, "podman") || strings.Contains(content, "/lxc/") {
			return true
		}
	}
	return false
}

func SkillMatchesEnvironment(fm map[string]interface{}) bool {
	envVal, ok := fm["environments"]
	if !ok || envVal == nil {
		return true
	}

	var list []string
	switch v := envVal.(type) {
	case string:
		if v != "" {
			list = []string{v}
		}
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

	for _, env := range list {
		normalized := strings.ToLower(strings.TrimSpace(env))
		if normalized == "" {
			continue
		}
		if !knownEnvironments[normalized] {
			return true
		}
		if detectEnvironment(normalized) {
			return true
		}
	}

	return false
}
