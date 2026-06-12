package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/enough/enough/backend/config"
)

func TestSkillsCollisionPrecedenceOrder(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("ENOUGH_HOME", filepath.Join(tempDir, "enough-home"))

	projectDir := filepath.Join(tempDir, "project")
	userDir := filepath.Join(tempDir, "enough-home")
	explicitDir := filepath.Join(tempDir, "explicit")
	flameDir := filepath.Join(tempDir, "flame-home")

	// 1. Create a Flame skill
	flameSkillDir := filepath.Join(flameDir, ".flame", "skills", "colliding-skill") // wait, flame is ~/.flame/skills
	flameHomePath := filepath.Join(tempDir, "flame-home-user")
	t.Setenv("HOME", flameHomePath) // override HOME for userHome detection of flame
	flameSkillDir = filepath.Join(flameHomePath, ".flame", "skills", "colliding-skill")

	if err := os.MkdirAll(flameSkillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(flameSkillDir, "SKILL.md"), []byte("---\nname: colliding-skill\ndescription: Flame version\n---\nBody"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 2. Create an Explicit skill
	explicitSkillDir := filepath.Join(explicitDir, "colliding-skill")
	if err := os.MkdirAll(explicitSkillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(explicitSkillDir, "SKILL.md"), []byte("---\nname: colliding-skill\ndescription: Explicit version\n---\nBody"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 3. Create a User skill
	userSkillDir := filepath.Join(userDir, "skills", "colliding-skill")
	if err := os.MkdirAll(userSkillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userSkillDir, "SKILL.md"), []byte("---\nname: colliding-skill\ndescription: User version\n---\nBody"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 4. Create a Project skill
	projectSkillDir := filepath.Join(projectDir, ".enough", "skills", "colliding-skill")
	if err := os.MkdirAll(projectSkillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectSkillDir, "SKILL.md"), []byte("---\nname: colliding-skill\ndescription: Project version\n---\nBody"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Runtime{
		Skills: config.SkillsSettings{
			Enabled:             true,
			EnableSkillCommands: true,
			Paths:               []string{explicitSkillDir},
		},
	}

	dirs := SearchLocations(projectDir, cfg, "")
	skills, diags := LoadSkillsFromDirs(projectDir, dirs, cfg)

	// We expect exactly 1 skill named colliding-skill, and its description should be "Project version"
	var colliding *Skill
	for _, sk := range skills {
		if sk.Name == "colliding-skill" {
			colliding = &sk
			break
		}
	}

	if colliding == nil {
		t.Fatal("expected colliding-skill to be found")
	}

	if colliding.Description != "Project version" {
		t.Fatalf("expected Project version to win collision, got: %s", colliding.Description)
	}

	// Verify diagnostics report the collision winner/loser
	foundDiag := false
	for _, d := range diags {
		if strings.Contains(d, `colliding-skill`) && strings.Contains(d, "winner=") && strings.Contains(d, "loser=") {
			foundDiag = true
			break
		}
	}
	if !foundDiag {
		t.Fatalf("expected collision diagnostics in: %v", diags)
	}
}
