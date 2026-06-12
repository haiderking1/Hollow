package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncSkills(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("ENOUGH_HOME", tempHome)

	// Verify initial sync copies bundled skills
	res, err := SyncSkills(true)
	if err != nil {
		t.Fatalf("SyncSkills failed: %v", err)
	}

	if res.TotalBundled == 0 {
		t.Fatalf("expected embedded bundled skills, got 0")
	}

	if len(res.Copied) == 0 {
		t.Fatalf("expected copied skills, got 0")
	}

	// Verify manifest exists and is v2 format
	manifestPath := filepath.Join(tempHome, "skills", ".bundled_manifest")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("expected .bundled_manifest to exist: %v", err)
	}
	manifestStr := string(data)
	if !strings.Contains(manifestStr, ":") {
		t.Fatalf("expected v2 manifest format name:hash, got: %s", manifestStr)
	}

	// Try running sync again - should skip all
	res2, err := SyncSkills(true)
	if err != nil {
		t.Fatalf("SyncSkills resync failed: %v", err)
	}
	if len(res2.Copied) != 0 || len(res2.Updated) != 0 {
		t.Fatalf("expected no changes on resync, got copied=%v updated=%v", res2.Copied, res2.Updated)
	}

	// Test user modification: modify a skill on disk
	firstSkill := res.Copied[0]
	// Locate it
	skillDir := FindSkillDirectory(firstSkill)
	if skillDir == "" {
		t.Fatalf("could not find skill directory for %s", firstSkill)
	}
	skillMd := filepath.Join(skillDir, "SKILL.md")
	originalContent, err := os.ReadFile(skillMd)
	if err != nil {
		t.Fatal(err)
	}

	// Append user modification
	if err := os.WriteFile(skillMd, append(originalContent, []byte("\n# Modified by User\n")...), 0o644); err != nil {
		t.Fatal(err)
	}

	// Sync again, should report it as user_modified and skip
	res3, err := SyncSkills(true)
	if err != nil {
		t.Fatal(err)
	}
	foundModified := false
	for _, m := range res3.UserModified {
		if m == firstSkill {
			foundModified = true
			break
		}
	}
	if !foundModified {
		t.Fatalf("expected %s to be skipped as user-modified", firstSkill)
	}

	// Test ResetBundledSkill (without restore)
	ok, msg, synced, err := ResetBundledSkill(firstSkill, false)
	if err != nil || !ok {
		t.Fatalf("ResetBundledSkill failed: ok=%v err=%v msg=%s", ok, err, msg)
	}
	if synced == nil {
		t.Fatal("expected sync result")
	}

	// Test ResetBundledSkill (with restore)
	ok, msg, synced, err = ResetBundledSkill(firstSkill, true)
	if err != nil || !ok {
		t.Fatalf("ResetBundledSkill with restore failed: ok=%v err=%v msg=%s", ok, err, msg)
	}
	// Verify it was restored (content matches original)
	restoredContent, err := os.ReadFile(skillMd)
	if err != nil {
		t.Fatal(err)
	}
	if string(restoredContent) != string(originalContent) {
		t.Fatalf("expected content to be restored, got differences")
	}

	// Test Deleted by User: delete the skill on disk
	if err := os.RemoveAll(skillDir); err != nil {
		t.Fatal(err)
	}
	// Sync again, should NOT re-copy since it's in the manifest (respect deletion)
	res4, err := SyncSkills(true)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range res4.Copied {
		if c == firstSkill {
			t.Fatalf("expected deleted skill %s to be respected, but it was re-copied", firstSkill)
		}
	}

	// Test Curator Suppression
	suppressedSkill := firstSkill
	MarkSuppressed(suppressedSkill)
	if !IsSuppressed(suppressedSkill) {
		t.Fatalf("expected %s to be suppressed", suppressedSkill)
	}
	// Let's reset the manifest for suppressedSkill
	manifestVal := ReadManifest()
	delete(manifestVal, suppressedSkill)
	_ = writeManifest(manifestVal)

	// Sync again, suppressed skill should NOT be copied even if not in manifest
	res5, err := SyncSkills(true)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range res5.Copied {
		if c == suppressedSkill {
			t.Fatalf("suppressed skill %s was re-copied", suppressedSkill)
		}
	}

	// Test Opt-out Marker
	if err := os.WriteFile(filepath.Join(tempHome, ".no-bundled-skills"), []byte("opt-out"), 0o644); err != nil {
		t.Fatal(err)
	}
	res6, err := SyncSkills(true)
	if err != nil {
		t.Fatal(err)
	}
	if !res6.SkippedOptOut {
		t.Fatalf("expected SkippedOptOut to be true when marker file exists")
	}
}
