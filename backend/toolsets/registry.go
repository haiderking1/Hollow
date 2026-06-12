package toolsets

import (
	"sort"
)

var ToolToToolset = map[string]string{
	"read_file":     "file",
	"write_file":    "file",
	"edit_file":     "file",
	"list_dir":      "file",
	"glob":          "file",
	"grep":          "file",
	"bash":          "terminal",
	"web_search":    "web",
	"agent_swarm":   "delegate",
	"skills_list":   "skills",
	"skill_view":    "skills",
	"skill_manage":  "skills",
	"memory":        "memory",
}

// GetAvailableToolsets takes a slice of active tool names and returns a sorted slice of toolset names.
func GetAvailableToolsets(activeTools []string) []string {
	seen := make(map[string]bool)
	for _, tool := range activeTools {
		if ts, ok := ToolToToolset[tool]; ok {
			seen[ts] = true
		}
	}
	var out []string
	for ts := range seen {
		out = append(out, ts)
	}
	sort.Strings(out)
	return out
}
