package agent

import (
	"strings"
	"testing"
)

func TestSanitizeBashOutputStripsOrphanMouse(t *testing.T) {
	in := strings.Repeat("[MCX0[MCY0", 50)
	out, _ := SanitizeBashOutput(in)
	if stringsContains(out, "[MC") {
		t.Fatalf("mouse leak remained: %q", out[:min(80, len(out))])
	}
	if !stringsContains(out, "suppressed") {
		t.Fatalf("expected suppression message, got %q", out[:min(80, len(out))])
	}
}

func TestSanitizeBashOutputStripsSixel(t *testing.T) {
	in := "before\x1bPq\"1;1;1;2;3;4;5;6;7\x1b\\after"
	out, sup := SanitizeBashOutput(in)
	if stringsContains(out, "\x1b") || stringsContains(out, "[MC") {
		t.Fatalf("escape leaked: %q", out)
	}
	if !stringsContains(out, "before") || !stringsContains(out, "after") {
		t.Fatalf("text lost: %q", out)
	}
	if !sup {
		t.Fatal("expected suppressed flag")
	}
}

func TestSanitizeBashOutputPlainPassthrough(t *testing.T) {
	in := "hello\nworld\n"
	out, sup := SanitizeBashOutput(in)
	if out != in {
		t.Fatalf("plain changed: %q", out)
	}
	if sup {
		t.Fatal("unexpected suppress")
	}
}

func TestBashCommandBlockedMpv(t *testing.T) {
	if msg := bashCommandBlocked("mpv --vo=sixel video.mp4"); msg == "" {
		t.Fatal("expected mpv block")
	}
	if msg := bashCommandBlocked("go test ./..."); msg != "" {
		t.Fatalf("unexpected block: %s", msg)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func stringsContains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
