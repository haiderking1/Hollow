package skills

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/enough/enough/backend/config"
)

func TestReadinessStatus(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("ENOUGH_HOME", tempHome)

	skillDir := filepath.Join(tempHome, "skills", "test-readiness")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// 1. Skill requiring an environment variable (not set)
	content := `---
name: test-readiness
description: Test readiness status
required_environment_variables:
  - name: TEST_READINESS_VAR
    prompt: Please enter TEST_READINESS_VAR
    help: This is a test var
---
Body text`
	skillMd := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillMd, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Runtime{
		Skills: config.SkillsSettings{
			Enabled:             true,
			EnableSkillCommands: true,
		},
	}

	// Execute skill view, should return setup_needed
	res := executeSkillViewInternal("test-readiness", "", tempHome, cfg, "sess-1", true)
	if !res.Success {
		t.Fatalf("expected view success, got: %s", res.Error)
	}
	if res.ReadinessStatus != ReadinessSetupNeeded {
		t.Fatalf("expected ReadinessSetupNeeded, got %q", res.ReadinessStatus)
	}
	if len(res.MissingRequiredEnvironmentVariables) != 1 || res.MissingRequiredEnvironmentVariables[0] != "TEST_READINESS_VAR" {
		t.Fatalf("expected missing TEST_READINESS_VAR, got %v", res.MissingRequiredEnvironmentVariables)
	}
	if res.Setup == nil || len(res.Setup.CollectSecrets) != 0 {
		// collect_secrets is empty because we parsed required_environment_variables
		// but wait, we normalized setup from fm
	}

	// 2. Set the env var
	t.Setenv("TEST_READINESS_VAR", "my-value")
	resSet := executeSkillViewInternal("test-readiness", "", tempHome, cfg, "sess-1", true)
	if !resSet.Success {
		t.Fatalf("expected view success, got: %s", resSet.Error)
	}
	if resSet.ReadinessStatus != ReadinessAvailable {
		t.Fatalf("expected ReadinessAvailable, got %q", resSet.ReadinessStatus)
	}
	if len(resSet.MissingRequiredEnvironmentVariables) != 0 {
		t.Fatalf("expected no missing vars, got %v", resSet.MissingRequiredEnvironmentVariables)
	}

	// 3. Platform unsupported
	contentUnsupported := `---
name: test-readiness
description: Test readiness status
platforms: [non-existent-os]
---
Body text`
	if err := os.WriteFile(skillMd, []byte(contentUnsupported), 0o644); err != nil {
		t.Fatal(err)
	}
	resUnsupp := executeSkillViewInternal("test-readiness", "", tempHome, cfg, "sess-1", true)
	if resUnsupp.Success {
		t.Fatalf("expected view error for unsupported platform")
	}
	if resUnsupp.ReadinessStatus != ReadinessUnsupported {
		t.Fatalf("expected ReadinessUnsupported status, got %q", resUnsupp.ReadinessStatus)
	}
}

func TestReadinessSetupMetadata(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("ENOUGH_HOME", tempHome)

	skillDir := filepath.Join(tempHome, "skills", "test-setup")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}

	content := `---
name: test-setup
description: Test setup metadata
setup:
  help: Follow standard credentials guide.
  collect_secrets:
    - env_var: TEST_SECRET_VAR
      prompt: Secret prompt
      provider_url: https://example.com/token
      secret: true
---
Body text`
	skillMd := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillMd, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Runtime{
		Skills: config.SkillsSettings{
			Enabled:             true,
			EnableSkillCommands: true,
		},
	}

	res := executeSkillViewInternal("test-setup", "", tempHome, cfg, "sess-1", true)
	if !res.Success {
		t.Fatalf("expected view success, got: %s", res.Error)
	}
	if res.ReadinessStatus != ReadinessSetupNeeded {
		t.Fatalf("expected ReadinessSetupNeeded, got %q", res.ReadinessStatus)
	}
	if res.Setup == nil {
		t.Fatal("expected Setup block in view results")
	}
	if res.Setup.Help == nil || *res.Setup.Help != "Follow standard credentials guide." {
		t.Fatalf("expected setup help text, got %v", res.Setup.Help)
	}
	if len(res.Setup.CollectSecrets) != 1 || res.Setup.CollectSecrets[0].EnvVar != "TEST_SECRET_VAR" {
		t.Fatalf("expected collect_secrets TEST_SECRET_VAR, got %v", res.Setup.CollectSecrets)
	}
}
