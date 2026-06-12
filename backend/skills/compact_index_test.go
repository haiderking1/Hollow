package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/enough/enough/backend/config"
)

func TestCompactIndexFocusMode(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("ENOUGH_HOME", tempHome)

	// Create a coding skill (in 'coding' category)
	codingDir := filepath.Join(tempHome, "skills", "coding", "refactor")
	if err := os.MkdirAll(codingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codingDir, "SKILL.md"), []byte("---\nname: refactor\ndescription: Refactor code\ncategory: coding\n---\nRefactor steps.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a non-coding skill (in 'music' category, which is in NonCodingCategories)
	musicDir := filepath.Join(tempHome, "skills", "music", "spotify")
	if err := os.MkdirAll(musicDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(musicDir, "SKILL.md"), []byte("---\nname: spotify\ndescription: Control spotify\ncategory: music\n---\nSpotify steps.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Runtime{
		Skills: config.SkillsSettings{Enabled: true},
		Agent: config.AgentSettings{
			CodingContext: "focus",
		},
	}

	// Force isCoding to be true by creating a .git folder in tempHome
	if err := os.MkdirAll(filepath.Join(tempHome, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	prompt := BuildIndexPrompt(tempHome, cfg, []string{"terminal"})

	// coding category should have description
	if !strings.Contains(prompt, "coding:") || !strings.Contains(prompt, "- refactor: Refactor code") {
		t.Fatalf("expected coding category to have description, got prompt:\n%s", prompt)
	}

	// music category should be demoted to names only
	if !strings.Contains(prompt, "music [names only]: spotify") {
		t.Fatalf("expected music category to be demoted to names only, got prompt:\n%s", prompt)
	}
	if strings.Contains(prompt, "- spotify: Control spotify") {
		t.Fatalf("expected music skill description to be omitted in focus mode, got prompt:\n%s", prompt)
	}
	if !strings.Contains(prompt, "[names only] are outside the current coding context") {
		t.Fatalf("expected demotion footer note, got prompt:\n%s", prompt)
	}
}
