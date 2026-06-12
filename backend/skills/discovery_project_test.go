package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/enough/enough/backend/config"
)

func TestDiscoveryProjectGitRoot(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("ENOUGH_HOME", tempHome)

	// Create repository root with .git and nested cwd
	repoRoot := filepath.Join(tempHome, "repo")
	nestedCwd := filepath.Join(repoRoot, "packages", "feature")
	if err := os.MkdirAll(nestedCwd, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	// 1. Skill in above-repo (should NOT be found since we stop at git root)
	aboveRepoSkillDir := filepath.Join(tempHome, ".agents", "skills", "above-repo")
	if err := os.MkdirAll(aboveRepoSkillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(aboveRepoSkillDir, "SKILL.md"), []byte("---\nname: above-repo\ndescription: Above Repo\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 2. Skill in repo-root (should be found)
	repoRootSkillDir := filepath.Join(repoRoot, ".agents", "skills", "repo-root")
	if err := os.MkdirAll(repoRootSkillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoRootSkillDir, "SKILL.md"), []byte("---\nname: repo-root\ndescription: Repo Root\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 3. Skill in nested package (should be found)
	nestedSkillDir := filepath.Join(repoRoot, "packages", ".agents", "skills", "nested")
	if err := os.MkdirAll(nestedSkillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nestedSkillDir, "SKILL.md"), []byte("---\nname: nested\ndescription: Nested\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Runtime{
		Skills: config.SkillsSettings{
			Enabled:             true,
			EnableSkillCommands: true,
		},
	}

	dirs := SearchLocations(nestedCwd, cfg, "")
	skills, _ := LoadSkillsFromDirs(nestedCwd, dirs, config.Runtime{})

	foundRepoRoot := false
	foundNested := false
	foundAboveRepo := false

	for _, sk := range skills {
		if sk.Name == "repo-root" {
			foundRepoRoot = true
		}
		if sk.Name == "nested" {
			foundNested = true
		}
		if sk.Name == "above-repo" {
			foundAboveRepo = true
		}
	}

	if !foundRepoRoot {
		t.Error("expected to find repo-root skill")
	}
	if !foundNested {
		t.Error("expected to find nested skill")
	}
	if foundAboveRepo {
		t.Error("should NOT find above-repo skill since it is outside git root")
	}
}

func TestDiscoveryProjectNoGitFSWalk(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("ENOUGH_HOME", tempHome)

	// Create non-repo structure without .git
	nonRepoRoot := filepath.Join(tempHome, "non-repo")
	nestedCwd := filepath.Join(nonRepoRoot, "a", "b")
	if err := os.MkdirAll(nestedCwd, 0o755); err != nil {
		t.Fatal(err)
	}

	// 1. Skill in root
	rootSkillDir := filepath.Join(nonRepoRoot, ".agents", "skills", "root")
	if err := os.MkdirAll(rootSkillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootSkillDir, "SKILL.md"), []byte("---\nname: root\ndescription: Root Skill\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 2. Skill in middle
	middleSkillDir := filepath.Join(nonRepoRoot, "a", ".agents", "skills", "middle")
	if err := os.MkdirAll(middleSkillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(middleSkillDir, "SKILL.md"), []byte("---\nname: middle\ndescription: Middle Skill\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Runtime{
		Skills: config.SkillsSettings{
			Enabled:             true,
			EnableSkillCommands: true,
		},
	}

	dirs := SearchLocations(nestedCwd, cfg, "")
	skills, _ := LoadSkillsFromDirs(nestedCwd, dirs, config.Runtime{})

	foundRoot := false
	foundMiddle := false

	for _, sk := range skills {
		if sk.Name == "root" {
			foundRoot = true
		}
		if sk.Name == "middle" {
			foundMiddle = true
		}
	}

	if !foundRoot {
		t.Error("expected to find root skill in non-git FS walk")
	}
	if !foundMiddle {
		t.Error("expected to find middle skill in non-git FS walk")
	}
}

func TestDiscoveryIgnoreRootMarkdownInAgentsSkills(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("ENOUGH_HOME", tempHome)

	agentsSkillsDir := filepath.Join(tempHome, ".agents", "skills")
	if err := os.MkdirAll(filepath.Join(agentsSkillsDir, "nested-skill"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Root MD file (should be ignored for .agents/skills)
	rootSkillFile := filepath.Join(agentsSkillsDir, "root-file.md")
	if err := os.WriteFile(rootSkillFile, []byte("---\nname: root-file\ndescription: Root markdown file\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Nested SKILL.md (should be found)
	nestedSkillFile := filepath.Join(agentsSkillsDir, "nested-skill", "SKILL.md")
	if err := os.WriteFile(nestedSkillFile, []byte("---\nname: nested-skill\ndescription: Nested skill\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Setup locations to scan agentsSkillsDir explicitly
	dirs := []SearchDir{
		{Path: agentsSkillsDir, Source: "project", IncludeRootMD: false},
	}

	skills, _ := LoadSkillsFromDirs(tempHome, dirs, config.Runtime{})

	foundRootFile := false
	foundNested := false

	for _, sk := range skills {
		if sk.Name == "root-file" {
			foundRootFile = true
		}
		if sk.Name == "nested-skill" {
			foundNested = true
		}
	}

	if foundRootFile {
		t.Error("should NOT find root-file.md under .agents/skills")
	}
	if !foundNested {
		t.Error("expected to find nested-skill under .agents/skills")
	}
}

func TestDiscoverySymlinkDeduplication(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("ENOUGH_HOME", tempHome)

	agentsSkillsDir := filepath.Join(tempHome, ".agents", "skills")
	if err := os.MkdirAll(filepath.Join(agentsSkillsDir, "foo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentsSkillsDir, "foo", "SKILL.md"), []byte("---\nname: foo\ndescription: foo\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a symlink under legacy path ~/.enough/agent/skills pointing to ~/.agents/skills
	legacyDir := filepath.Join(tempHome, "agent")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}

	legacySkillsDir := filepath.Join(legacyDir, "skills")
	if err := os.Symlink(agentsSkillsDir, legacySkillsDir); err != nil {
		// On some Windows/Docker setups, symlinks might fail due to privileges.
		// Skip if OS error.
		t.Skipf("skipping symlink test due to symlink creation error: %v", err)
	}

	cfg := config.Runtime{
		Skills: config.SkillsSettings{
			Enabled:             true,
			EnableSkillCommands: true,
		},
	}

	dirs := SearchLocations(tempHome, cfg, "")
	skills, _ := LoadSkillsFromDirs(tempHome, dirs, config.Runtime{})

	fooCount := 0
	for _, sk := range skills {
		if sk.Name == "foo" {
			fooCount++
		}
	}

	if fooCount != 1 {
		t.Fatalf("expected foo to be found exactly once, got count=%d", fooCount)
	}
}

func TestDiscoveryGitignoreDeduplicationAndIgnoring(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("ENOUGH_HOME", tempHome)

	skillsDir := filepath.Join(tempHome, "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Write .gitignore excluding "venv"
	if err := os.WriteFile(filepath.Join(skillsDir, ".gitignore"), []byte("venv\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 1. Good skill
	goodSkillDir := filepath.Join(skillsDir, "good-skill")
	if err := os.MkdirAll(goodSkillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(goodSkillDir, "SKILL.md"), []byte("---\nname: good-skill\ndescription: Good\n---\nContent"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 2. Ignored venv skill
	ignoredSkillDir := filepath.Join(skillsDir, "venv", "bad-skill")
	if err := os.MkdirAll(ignoredSkillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ignoredSkillDir, "SKILL.md"), []byte("---\nname: bad-skill\ndescription: Bad\n---\nContent"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Runtime{
		Skills: config.SkillsSettings{
			Enabled:             true,
			EnableSkillCommands: true,
		},
	}

	dirs := SearchLocations(tempHome, cfg, "")
	skills, _ := LoadSkillsFromDirs(tempHome, dirs, config.Runtime{})

	foundGood := false
	foundBad := false

	for _, sk := range skills {
		if sk.Name == "good-skill" {
			foundGood = true
		}
		if sk.Name == "bad-skill" {
			foundBad = true
		}
	}

	if !foundGood {
		t.Error("expected to find good-skill")
	}
	if foundBad {
		t.Error("should NOT find bad-skill because it resides in ignored venv/")
	}
}
