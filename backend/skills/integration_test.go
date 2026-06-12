package skills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/enough/enough/backend/config"
)

func TestAgentSessionSkillsIntegration(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("ENOUGH_HOME", tempHome)
	t.Setenv("HOME", tempHome)
	ClearSkillsPromptCache()

	cfg := config.Runtime{
		Skills: config.SkillsSettings{
			Enabled:             true,
			EnableSkillCommands: true,
		},
	}

	// 1. Create skill using ExecuteSkillManage
	skillContent := `---
name: integration-skill
description: Integration test skill
---
Use ${ENOUGH_SKILL_DIR}/scripts/foo.sh
`
	createArgs := map[string]interface{}{
		"action":  "create",
		"name":    "integration-skill",
		"content": skillContent,
	}
	argsJSON, err := json.Marshal(createArgs)
	if err != nil {
		t.Fatal(err)
	}

	resStr, isErr := ExecuteSkillManage(string(argsJSON), SkillManageOptions{GuardEnabled: true})
	if isErr {
		t.Fatalf("ExecuteSkillManage failed: %s", resStr)
	}

	// Verify skill created on disk
	skillMdPath := filepath.Join(tempHome, "skills", "integration-skill", "SKILL.md")
	if _, err := os.Stat(skillMdPath); err != nil {
		t.Fatalf("expected SKILL.md to exist: %v", err)
	}

	// 2. Index in prompt (with skill tools enabled: BuildIndexPrompt)
	toolSet := []string{"skills_list", "skill_view", "skill_manage"}
	indexPrompt := BuildIndexPrompt(tempHome, cfg, toolSet)
	if !strings.Contains(indexPrompt, "integration-skill") {
		t.Fatalf("expected index prompt to contain 'integration-skill', got %q", indexPrompt)
	}

	// 3. XML Fallback (without skill tools enabled: FormatSkillsForPrompt)
	sks, diags := DiscoverAllSkills(tempHome, cfg)
	if len(diags) > 0 {
		t.Logf("discovery diagnostics: %v", diags)
	}
	xmlPrompt := FormatSkillsForPrompt(sks)
	if !strings.Contains(xmlPrompt, "<available_skills>") {
		t.Fatalf("expected xml prompt to contain '<available_skills>', got %q", xmlPrompt)
	}
	if !strings.Contains(xmlPrompt, "<name>integration-skill</name>") {
		t.Fatalf("expected xml prompt to contain '<name>integration-skill</name>', got %q", xmlPrompt)
	}

	// 4. View replaces template variables
	viewArgs := map[string]interface{}{
		"name": "integration-skill",
	}
	viewArgsJSON, err := json.Marshal(viewArgs)
	if err != nil {
		t.Fatal(err)
	}

	viewResStr, isErr := ExecuteSkillView(string(viewArgsJSON), tempHome, cfg, "sess-123")
	if isErr {
		t.Fatalf("ExecuteSkillView failed: %s", viewResStr)
	}

	var viewRes struct {
		Success  bool   `json:"success"`
		Content  string `json:"content"`
		SkillDir string `json:"skill_dir"`
	}
	if err := json.Unmarshal([]byte(viewResStr), &viewRes); err != nil {
		t.Fatal(err)
	}

	if !viewRes.Success {
		t.Fatalf("expected viewRes.Success to be true")
	}

	expectedDir := filepath.Join(tempHome, "skills", "integration-skill")
	if viewRes.SkillDir != expectedDir {
		t.Fatalf("expected skillDir %q, got %q", expectedDir, viewRes.SkillDir)
	}

	if !strings.Contains(viewRes.Content, expectedDir) {
		t.Fatalf("expected view content to contain preprocessed path %q, got %q", expectedDir, viewRes.Content)
	}

	if strings.Contains(viewRes.Content, "${ENOUGH_SKILL_DIR}") {
		t.Fatalf("expected view content to not contain '${ENOUGH_SKILL_DIR}', got %q", viewRes.Content)
	}
}
