package skills

import (
	"runtime"
	"strings"
	"testing"
)

func TestSubstituteTemplateVars(t *testing.T) {
	// 1. substitutes FLAME_SKILL_DIR, HERMES_SKILL_DIR and ENOUGH_SKILL_DIR when skill dir provided
	res1 := substituteTemplateVars("Path: ${FLAME_SKILL_DIR} / ${ENOUGH_SKILL_DIR} / ${HERMES_SKILL_DIR}", "/tmp/skill", "")
	if res1 != "Path: /tmp/skill / /tmp/skill / /tmp/skill" {
		t.Fatalf("expected Path: /tmp/skill / /tmp/skill / /tmp/skill, got %q", res1)
	}

	// 2. substitutes FLAME_SESSION_ID, HERMES_SESSION_ID and ENOUGH_SESSION_ID when session id provided
	res2 := substituteTemplateVars("Session: ${FLAME_SESSION_ID} / ${ENOUGH_SESSION_ID} / ${HERMES_SESSION_ID}", "", "sess-123")
	if res2 != "Session: sess-123 / sess-123 / sess-123" {
		t.Fatalf("expected Session: sess-123 / sess-123 / sess-123, got %q", res2)
	}

	// 3. leaves unresolved tokens in place
	res3 := substituteTemplateVars("Session: ${FLAME_SESSION_ID}", "", "")
	if res3 != "Session: ${FLAME_SESSION_ID}" {
		t.Fatalf("expected unresolved tokens to be left in place, got %q", res3)
	}
}

func TestExpandInlineShell(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("skipping inline shell tests on non-linux platforms")
	}

	// 1. expands inline shell when explicitly enabled
	content := "Value: !`echo test-value`"
	result := expandInlineShell(content, "", 5)
	if !strings.Contains(result, "test-value") {
		t.Fatalf("expected inline shell expansion to contain 'test-value', got %q", result)
	}
}

func TestPreprocessSkillContentFlags(t *testing.T) {
	// preprocess applies template vars before optional shell
	result := PreprocessSkillContent("Dir=${FLAME_SKILL_DIR} shell=!`echo test`", "/my/skill", "", false, 10)
	if !strings.Contains(result, "Dir=/my/skill") {
		t.Fatalf("expected template substitution, got %q", result)
	}
	if !strings.Contains(result, "!`echo test`") {
		t.Fatalf("expected shell execution to be bypassed when disabled, got %q", result)
	}
}
