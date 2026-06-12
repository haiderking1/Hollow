package plugins

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/enough/enough/backend/config"
	"github.com/enough/enough/backend/enoughhome"
)

var nsRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// IsValidNamespace reports whether the namespace is valid.
func IsValidNamespace(ns string) bool {
	return nsRegex.MatchString(ns)
}

// ParseQualifiedName splits a qualified name like "namespace:skill" into namespace and bare name.
func ParseQualifiedName(name string) (namespace, bare string) {
	idx := strings.Index(name, ":")
	if idx < 0 {
		return "", name
	}
	return name[:idx], name[idx+1:]
}

// IsPluginDisabled reports whether the plugin namespace is disabled.
func IsPluginDisabled(namespace string, cfg config.Runtime) bool {
	for _, d := range cfg.Plugins.Disabled {
		if d == namespace {
			return true
		}
	}
	return false
}

// PluginsDir returns the directory where user plugins are installed.
func PluginsDir() string {
	return filepath.Join(enoughhome.HomeDir(), "plugins")
}

// FindPluginSkill finds the path to the plugin skill's SKILL.md file.
func FindPluginSkill(qualifiedName string) (string, error) {
	ns, bare := ParseQualifiedName(qualifiedName)
	if ns == "" || bare == "" {
		return "", fmt.Errorf("invalid qualified name: %s", qualifiedName)
	}
	if !IsValidNamespace(ns) {
		return "", fmt.Errorf("invalid namespace: %s", ns)
	}
	path := filepath.Join(PluginsDir(), ns, "skills", bare, "SKILL.md")
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	return path, nil
}

// ListPluginSkills returns all skills provided by the plugin namespace.
func ListPluginSkills(namespace string) ([]string, error) {
	skillsDir := filepath.Join(PluginsDir(), namespace, "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return nil, nil
	}
	var skills []string
	for _, entry := range entries {
		if entry.IsDir() {
			skillMd := filepath.Join(skillsDir, entry.Name(), "SKILL.md")
			if _, err := os.Stat(skillMd); err == nil {
				skills = append(skills, entry.Name())
			}
		}
	}
	sort.Strings(skills)
	return skills, nil
}

// GetPluginSiblingBanner returns the siblings banner.
func GetPluginSiblingBanner(namespace, bare string) string {
	siblings, _ := ListPluginSkills(namespace)
	var clean []string
	for _, s := range siblings {
		if s != bare {
			clean = append(clean, s)
		}
	}
	if len(clean) > 0 {
		return fmt.Sprintf("[Bundle context: This skill is part of the '%s' plugin.\nSibling skills: %s.\nUse qualified form to invoke siblings (e.g. %s:%s).]\n\n",
			namespace, strings.Join(clean, ", "), namespace, clean[0])
	}
	return fmt.Sprintf("[Bundle context: This skill is part of the '%s' plugin.]\n\n", namespace)
}
