package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/enough/enough/backend/config"
)

func TestPluginSkillResolution(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("ENOUGH_HOME", tempHome)

	pluginSkillDir := filepath.Join(tempHome, "plugins", "my-plugin", "skills", "hello-world")
	if err := os.MkdirAll(pluginSkillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	skillContent := `---
name: hello-world
description: A plugin skill
---
Hello from plugin!`

	if err := os.WriteFile(filepath.Join(pluginSkillDir, "SKILL.md"), []byte(skillContent), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Runtime{
		Skills: config.SkillsSettings{Enabled: true},
		Plugins: config.PluginsSettings{
			Disabled: []string{},
		},
	}

	// 1. Successful view of plugin skill
	res := executeSkillViewInternal("my-plugin:hello-world", "", tempHome, cfg, "sess-1", true)
	if !res.Success {
		t.Fatalf("expected success, got error: %s", res.Error)
	}
	if !strings.Contains(res.Content, "Hello from plugin!") {
		t.Fatalf("expected content 'Hello from plugin!', got %q", res.Content)
	}
	if !strings.Contains(res.Content, "my-plugin") {
		t.Fatalf("expected content to contain plugin sibling/context banner, got %q", res.Content)
	}

	// 2. Disabled plugin error
	cfgDisabled := cfg
	cfgDisabled.Plugins.Disabled = []string{"my-plugin"}
	resDisabled := executeSkillViewInternal("my-plugin:hello-world", "", tempHome, cfgDisabled, "sess-1", true)
	if resDisabled.Success {
		t.Fatal("expected disabled plugin view to fail")
	}
	if !strings.Contains(resDisabled.Error, "is disabled") {
		t.Fatalf("expected disabled error message, got: %s", resDisabled.Error)
	}

	// 3. Skill not found in plugin namespace (and lists other skills)
	resNotFound := executeSkillViewInternal("my-plugin:missing", "", tempHome, cfg, "sess-1", true)
	if resNotFound.Success {
		t.Fatal("expected view of missing skill to fail")
	}
	if !strings.Contains(resNotFound.Error, "not found in plugin") {
		t.Fatalf("expected not found error message, got: %s", resNotFound.Error)
	}
	if len(resNotFound.Matches) == 0 || resNotFound.Matches[0] != "my-plugin:hello-world" {
		t.Fatalf("expected available skills listed in matches, got: %v", resNotFound.Matches)
	}
}
