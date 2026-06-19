package agent

import "testing"

func TestReadonlyWorkflowRoleRejectsMutatingBash(t *testing.T) {
	a := &Agent{allowedTools: map[string]bool{"bash": true}, readonlyRole: true}
	if got := a.guardTool("bash", `{"command":"git checkout other"}`); got == nil || !got.isErr {
		t.Fatal("read-only role allowed git checkout")
	}
	if got := a.guardTool("bash", `{"command":"git diff --stat"}`); got != nil {
		t.Fatalf("read-only command rejected: %+v", got)
	}
}
