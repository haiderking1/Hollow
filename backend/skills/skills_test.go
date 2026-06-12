package skills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/enough/enough/backend/config"
)

func TestSkillsIntegration(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("ENOUGH_HOME", tmpHome)

	// Verify paths resolver matches convention
	if !strings.HasPrefix(SkillsDir(), tmpHome) {
		t.Fatalf("expected SkillsDir() to start with temp ENOUGH_HOME, got %s", SkillsDir())
	}

	// 1. Create a global skill using skill_manage tool API
	createArgs := `{
		"action": "create",
		"name": "integration-skill",
		"category": "devops",
		"content": "---\nname: integration-skill\ndescription: Test integration skill description\n---\nBody of integration skill targeting ${ENOUGH_SKILL_DIR}"
	}`
	res, isErr := ExecuteSkillManage(createArgs, SkillManageOptions{GuardEnabled: true})
	if isErr {
		t.Fatalf("ExecuteSkillManage create failed: %s", res)
	}

	var createRes struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Path    string `json:"path"`
	}
	if err := json.Unmarshal([]byte(res), &createRes); err != nil {
		t.Fatalf("failed to unmarshal create result: %v", err)
	}
	if !createRes.Success {
		t.Fatalf("skill creation success was false")
	}

	skillDir := filepath.Join(SkillsDir(), createRes.Path)
	if !strings.Contains(skillDir, "integration-skill") {
		t.Fatalf("expected path to contain skill name, got %s", skillDir)
	}

	// Verify SKILL.md file exists on disk
	skillFile := filepath.Join(skillDir, "SKILL.md")
	if _, err := os.Stat(skillFile); err != nil {
		t.Fatalf("expected SKILL.md file to exist: %v", err)
	}

	// 2. Add a supporting file using write_file manage action
	writeFileArgs := `{
		"action": "write_file",
		"name": "integration-skill",
		"file_path": "references/standards.md",
		"file_content": "These are coding standards"
	}`
	res, isErr = ExecuteSkillManage(writeFileArgs, SkillManageOptions{GuardEnabled: true})
	if isErr {
		t.Fatalf("ExecuteSkillManage write_file failed: %s", res)
	}

	// 3. Test list tool
	listArgs := `{"category": "devops"}`
	listResStr, isErr := ExecuteSkillsList(listArgs, tmpHome, config.Runtime{
		Skills: config.SkillsSettings{Enabled: true},
	})
	if isErr {
		t.Fatalf("ExecuteSkillsList failed: %s", listResStr)
	}
	if !strings.Contains(listResStr, "integration-skill") {
		t.Fatalf("expected skills list to contain integration-skill: %s", listResStr)
	}

	// 4. Test view tool & Preprocessing substitution
	viewArgs := `{"name": "integration-skill"}`
	viewResStr, isErr := ExecuteSkillView(viewArgs, tmpHome, config.Runtime{
		Skills: config.SkillsSettings{Enabled: true},
	}, "session-123")
	if isErr {
		t.Fatalf("ExecuteSkillView failed: %s", viewResStr)
	}
	if !strings.Contains(viewResStr, "Test integration skill description") {
		t.Fatalf("expected view content to contain description: %s", viewResStr)
	}
	if !strings.Contains(viewResStr, skillDir) {
		t.Fatalf("expected view content to resolve ${ENOUGH_SKILL_DIR} to %s: %s", skillDir, viewResStr)
	}

	// 5. Test view tool with supporting file path
	viewFileArgs := `{"name": "integration-skill", "file_path": "references/standards.md"}`
	viewFileResStr, isErr := ExecuteSkillView(viewFileArgs, tmpHome, config.Runtime{
		Skills: config.SkillsSettings{Enabled: true},
	}, "session-123")
	if isErr {
		t.Fatalf("ExecuteSkillView on supporting file failed: %s", viewFileResStr)
	}
	if !strings.Contains(viewFileResStr, "These are coding standards") {
		t.Fatalf("expected supporting file content, got: %s", viewFileResStr)
	}

	// 6. Test system prompt index building
	cfg := config.Runtime{
		Skills: config.SkillsSettings{
			Enabled: true,
		},
	}
	prompt := BuildIndexPrompt(tmpHome, cfg, []string{"skills_list", "skill_view"})
	if !strings.Contains(prompt, "integration-skill") {
		t.Fatalf("expected BuildIndexPrompt to list integration-skill: %s", prompt)
	}

	// 7. Test slash command expansion
	expanded, _, err := ExpandSkillSlashCommand("integration-skill", "test-arguments", tmpHome, cfg, "session-123")
	if err != nil {
		t.Fatalf("ExpandSkillSlashCommand failed: %v", err)
	}
	if !strings.Contains(expanded, "test-arguments") {
		t.Fatalf("expected expanded slash prompt to contain arguments: %s", expanded)
	}
	if !strings.Contains(expanded, "integration-skill") {
		t.Fatalf("expected expanded slash prompt to contain skill name: %s", expanded)
	}
}

func TestSkillsCollisionPrecedence(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("ENOUGH_HOME", tmpHome)

	// Set up global user skill
	globalDir := filepath.Join(tmpHome, "skills", "general", "duplicate-skill")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatalf("failed to create global skill dir: %v", err)
	}
	globalSkillContent := "---\nname: duplicate-skill\ndescription: Global version description\n---\nGlobal body"
	if err := os.WriteFile(filepath.Join(globalDir, "SKILL.md"), []byte(globalSkillContent), 0o644); err != nil {
		t.Fatalf("failed to write global skill: %v", err)
	}

	// Set up project skill (cwd)
	projectDir := filepath.Join(tmpHome, "project")
	projectSkillDir := filepath.Join(projectDir, ".enough", "skills", "duplicate-skill")
	if err := os.MkdirAll(projectSkillDir, 0o755); err != nil {
		t.Fatalf("failed to create project skill dir: %v", err)
	}
	projectSkillContent := "---\nname: duplicate-skill\ndescription: Project version description\n---\nProject body"
	if err := os.WriteFile(filepath.Join(projectSkillDir, "SKILL.md"), []byte(projectSkillContent), 0o644); err != nil {
		t.Fatalf("failed to write project skill: %v", err)
	}

	// Discover skills
	skillsList, _ := DiscoverAllSkills(projectDir, config.Runtime{})

	// Check if only 1 is discovered and it's the project version (precedence: project beats global)
	found := false
	for _, sk := range skillsList {
		if sk.Name == "duplicate-skill" {
			found = true
			if sk.Description != "Project version description" {
				t.Fatalf("expected project version to win collision override, got description: %s", sk.Description)
			}
		}
	}
	if !found {
		t.Fatalf("expected duplicate-skill to be found")
	}
}

func TestInlineBashPreprocessing(t *testing.T) {
	tmpDir := t.TempDir()
	rawContent := "Inline result is: !`echo 'preprocessed'`"
	processed := PreprocessSkillContent(rawContent, tmpDir, "sess-1", true, 10)
	expected := "Inline result is: preprocessed"
	if !strings.Contains(processed, expected) {
		t.Fatalf("expected preprocessing to execute bash cmd and produce: %q, got: %q", expected, processed)
	}
}
