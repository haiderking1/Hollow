package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/enough/enough/backend/config"
)

func TestSlashCommandsExpand(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("ENOUGH_HOME", tempHome)

	skillDir := filepath.Join(tempHome, "skills", "demo")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	content := "---\nname: demo\ndescription: Demo\n---\nRun ${ENOUGH_SKILL_DIR}/scripts/foo.sh\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Runtime{
		Skills: config.SkillsSettings{
			Enabled:             true,
			EnableSkillCommands: true,
		},
	}

	// 1. ExpandSkillSlashCommand returns activation banner
	msg, cleanBody, err := ExpandSkillSlashCommand("demo", "extra user note", tempHome, cfg, "sess-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(msg, "[IMPORTANT: The user has invoked") {
		t.Errorf("expected activation banner, got %q", msg)
	}
	if !strings.Contains(msg, "Run ") {
		t.Errorf("expected Run, got %q", msg)
	}
	if !strings.Contains(msg, skillDir) {
		t.Errorf("expected skillDir %q, got %q", skillDir, msg)
	}
	if strings.Contains(msg, "<skill ") {
		t.Errorf("expected no <skill tags, got %q", msg)
	}
	if !strings.Contains(msg, "extra user note") {
		t.Errorf("expected extra user note, got %q", msg)
	}
	if !strings.Contains(cleanBody, "Run ") {
		t.Errorf("expected cleanBody to contain Run, got %q", cleanBody)
	}

	// 2. buildSkillInvocationMessage includes skill directory hint
	loadedSkill := map[string]interface{}{
		"name":    "hinted",
		"content": "Body text",
	}
	msgHinted := BuildSkillInvocationMessage(loadedSkill, filepath.Join(tempHome, "skills", "hinted"), "", "", config.Runtime{})
	if !strings.Contains(msgHinted, "[Skill directory:") {
		t.Errorf("expected directory hint, got %q", msgHinted)
	}
	if !strings.Contains(msgHinted, "hinted") {
		t.Errorf("expected 'hinted' in directory hint, got %q", msgHinted)
	}

	// 3. returns error for unknown skill
	_, _, err = ExpandSkillSlashCommand("does-not-exist", "note", tempHome, cfg, "sess-123")
	if err == nil {
		t.Error("expected error for unknown skill, got nil")
	}
}
