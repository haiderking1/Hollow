package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestRenderAgentSwarmBlock(t *testing.T) {
	styles := NewStyles()
	args := `{
		"shared_context": "Current system already supports spawned subagents with roles.",
		"tasks": [
			{"id": "Professor Farnsworth", "prompt": "Explore Structured Interagent Mailboxes."},
			{"id": "Wernstrom", "prompt": "Explore Agent Tree Control Plane."},
			{"id": "Zoidberg", "prompt": "Explore Shared Blackboard for Agent Coordination."}
		]
	}`
	lines := renderAgentSwarmBlock(styles, toolRow{
		Kind:    toolKindSwarm,
		Args:    args,
		Pending: true,
	}, 100, false, 3)
	plain := ansi.Strip(strings.Join(lines, "\n"))

	for _, want := range []string{
		"Spawned",
		"Professor Farnsworth",
		"[worker]",
		"Structured Interagent Mailboxes",
		"Wernstrom",
		"Zoidberg",
		"Context:",
		"Current system already supports spawned subagents",
	} {
		if !strings.Contains(plain, want) {
			t.Fatalf("missing %q in:\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "Updated") {
		t.Fatalf("swarm block should not include group header: %q", plain)
	}
}

func TestRenderWebSearchBlock(t *testing.T) {
	styles := NewStyles()
	lines := renderWebSearchBlock(styles, toolRow{
		Kind:    toolKindWeb,
		Target:  "interesting random fact",
		Pending: true,
	}, 80, false)
	plain := ansi.Strip(strings.Join(lines, "\n"))

	if !strings.Contains(plain, "Search") || !strings.Contains(plain, "interesting random fact") {
		t.Fatalf("unexpected web search render: %q", plain)
	}
	if !strings.Contains(plain, "└") {
		t.Fatalf("expected tree branch: %q", plain)
	}
}

func TestSpawnBulletRendersInSwarmBlock(t *testing.T) {
	styles := NewStyles()
	lines := renderAgentSwarmBlock(styles, toolRow{
		Kind:    toolKindSwarm,
		Args:    `{"tasks":[{"id":"a","prompt":"ping"}]}`,
		Pending: true,
	}, 100, false, 2)
	plain := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(plain, "Spawned") {
		t.Fatalf("expected spawn header: %q", plain)
	}
	if strings.Contains(plain, "*") {
		t.Fatalf("swarm beacon should not use asterisk bullet: %q", plain)
	}
}

func TestRenderAgentSwarmBlockShowsStatusWhenDone(t *testing.T) {
	styles := NewStyles()
	lines := renderAgentSwarmBlock(styles, toolRow{
		Kind:   toolKindSwarm,
		Args:   `{"tasks":[{"id":"a","prompt":"do thing"}]}`,
		Output: "## a [ok] (2 turns)\ndone",
	}, 100, false, 0)
	plain := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(plain, "(ok)") {
		t.Fatalf("expected done status: %q", plain)
	}
	if strings.Contains(plain, "running") {
		t.Fatalf("should not show running when done: %q", plain)
	}
}

func TestRenderAgentSwarmBlockShowsAbortedStatus(t *testing.T) {
	styles := NewStyles()
	lines := renderAgentSwarmBlock(styles, toolRow{
		Kind:   toolKindSwarm,
		Args:   `{"tasks":[{"id":"a","prompt":"do thing"}]}`,
		Output: "## a [aborted] (1 turn)\nError: user aborted",
	}, 100, false, 0)
	plain := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(plain, "(aborted)") {
		t.Fatalf("expected aborted status: %q", plain)
	}
}

func TestRenderAgentSwarmBlockShowsRetryCount(t *testing.T) {
	styles := NewStyles()
	lines := renderAgentSwarmBlock(styles, toolRow{
		Kind:   toolKindSwarm,
		Args:   `{"tasks":[{"id":"a","prompt":"do thing"}]}`,
		Output: "## a [ok] (2 turns ×3)\ndone",
	}, 100, false, 0)
	plain := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(plain, "×3") {
		t.Fatalf("expected retry count: %q", plain)
	}
}

func TestRenderAgentSwarmBlockShowsErrorWhenExpanded(t *testing.T) {
	styles := NewStyles()
	lines := renderAgentSwarmBlock(styles, toolRow{
		Kind:   toolKindSwarm,
		Args:   `{"tasks":[{"id":"a","prompt":"do thing"}]}`,
		Output: "## a [error] (1 turn)\nError: boom",
	}, 100, true, 0)
	plain := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(plain, "Error: boom") {
		t.Fatalf("expected expanded error detail: %q", plain)
	}
}

func TestRenderAgentSwarmBlockShowsJSONParseError(t *testing.T) {
	styles := NewStyles()
	errText := "agent_swarm: invalid JSON in tool arguments (invalid character '\\n' in string literal)."
	lines := renderAgentSwarmBlock(styles, toolRow{
		Kind:   toolKindSwarm,
		Error:  true,
		Output: errText,
	}, 100, false, 0)
	plain := ansi.Strip(strings.Join(lines, "\n"))
	if strings.Contains(plain, "failed") && !strings.Contains(plain, "invalid JSON") {
		t.Fatalf("expected JSON parse error text, got: %q", plain)
	}
	if !strings.Contains(plain, "invalid JSON") {
		t.Fatalf("expected invalid JSON in output: %q", plain)
	}
}

func TestSingleSwarmNoGroupHeader(t *testing.T) {
	styles := NewStyles()
	out := renderToolGroup(styles, []chatMsg{{
		toolName: "agent_swarm",
		toolArgs: `{"tasks":[{"id":"a","prompt":"do thing"}]}`,
	}}, 100, false, 0)
	if strings.Contains(out, "Updated") {
		t.Fatalf("single swarm should not show group header: %q", out)
	}
	if !strings.Contains(ansi.Strip(out), "Spawned") {
		t.Fatalf("expected spawn header: %q", out)
	}
}
