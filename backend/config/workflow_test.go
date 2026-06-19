package config

import "testing"

func TestWorkflowSettingsPersist(t *testing.T) {
	t.Setenv("ENOUGH_HOME", t.TempDir())
	cfg := Default()
	cfg.Workflows.Ultracode = true
	cfg.Workflows.AltScreen = true
	cfg.Workflows.AlwaysApprove = []string{"/repo::audit"}
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Workflows == nil || !loaded.Workflows.Ultracode || !loaded.Workflows.AltScreen ||
		len(loaded.Workflows.AlwaysApprove) != 1 {
		t.Fatalf("workflow settings = %#v", loaded.Workflows)
	}
}
