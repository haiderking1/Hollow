package tui

import (
	"strings"
	"testing"

	"github.com/enough/enough/frontend/tui/term"
)

func TestFormatFooterTokens(t *testing.T) {
	if got := formatFooterTokens(500); got != "500" {
		t.Fatalf("got %q", got)
	}
	if got := formatFooterTokens(1500); got != "1.5k" {
		t.Fatalf("got %q", got)
	}
	if got := formatFooterTokens(1000000); got != "1.0M" {
		t.Fatalf("got %q", got)
	}
}

func TestFooterJoin(t *testing.T) {
	left := "0.0%/1.0M (auto)"
	right := "(opencode-go) deepseek-v4-flash • xhigh"
	line := footerJoin(80, left, right)
	if term.VisibleWidth(line) != 80 {
		t.Fatalf("expected width 80, got %d", term.VisibleWidth(line))
	}
	if !strings.HasSuffix(strings.TrimRight(line, " "), "xhigh") {
		t.Fatalf("expected right side aligned: %q", line)
	}
}
