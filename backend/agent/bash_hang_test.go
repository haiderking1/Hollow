package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Flame closes inherited stdio handles after a short grace once the shell exits,
// so a detached grandchild cannot keep bash tool "running" forever.
func TestBashInheritedStdioDoesNotHang(t *testing.T) {
	a, _ := newBashTestAgent(t)
	cmd := `node -e "const {spawn}=require('child_process'); const c=spawn('sleep',['60'],{stdio:'inherit',detached:true}); c.unref(); console.log('child-exiting');"`
	args, err := json.Marshal(struct{ Command string }{Command: cmd})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	res := a.toolBash(ctx, "call_1", string(args))
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("toolBash hung for %s", elapsed)
	}
	if !strings.Contains(res.output, "child-exiting") {
		t.Fatalf("output = %q", res.output)
	}
}
