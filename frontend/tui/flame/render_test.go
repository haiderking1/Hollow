package flame

import (
	"strings"
	"testing"

	"github.com/enough/enough/frontend/tui/markdown"
)

func TestPrefixEqual(t *testing.T) {
	a := []string{"one", "two", "three"}
	b := []string{"one", "two", "changed"}
	if !prefixEqual(a, a, 2) {
		t.Fatal("expected prefix match for identical slices")
	}
	if prefixEqual(a, b, 3) {
		t.Fatal("expected prefix mismatch at index 2")
	}
}

func TestIsImageSpacerRow(t *testing.T) {
	undo := markdown.CapabilitiesForTest(markdown.Capabilities{Images: markdown.ImageSixel, TrueColor: true})
	defer undo()

	rendered := markdown.RenderAttachmentImage(
		"data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==",
		40,
		markdown.Theme{},
		markdown.RenderOptions{},
	)
	if rendered == "" {
		t.Fatal("expected sixel output")
	}

	imgLines := strings.Split(strings.TrimSpace(rendered), "\n")
	if len(imgLines) < 1 {
		t.Fatal("expected at least one image line")
	}
	imageLine := imgLines[len(imgLines)-1]
	if !markdown.IsImageLine(imageLine) {
		t.Fatalf("expected image on last line, got %q", imageLine)
	}

	lines := []string{"hello"}
	for _, l := range imgLines {
		lines = append(lines, l)
	}
	lines = append(lines, "next")

	for i := 1; i < len(imgLines); i++ {
		idx := i // reserved row index in lines
		if !isImageSpacerRow(lines, idx) {
			t.Fatalf("expected reserved row %d to be image spacer", idx)
		}
	}
	if isImageSpacerRow(lines, len(lines)-1) {
		t.Fatal("text line should not be image spacer")
	}
	if isImageSpacerRow(lines, 0) {
		t.Fatal("text line should not be image spacer")
	}
}

func TestRenderStablePrefixSkipsScan(t *testing.T) {
	r := &Renderer{
		previousLines: []string{"chat-1", "chat-2", "composer", "footer"},
		previousWidth: 80,
		previousHeight: 24,
	}

	// Simulate composer-only change: thousands of chat lines unchanged.
	prev := make([]string, 0, 1004)
	for i := 0; i < 1000; i++ {
		prev = append(prev, "chat")
	}
	prev = append(prev, "composer v1", "footer")
	r.previousLines = prev

	next := append([]string(nil), prev...)
	next[len(next)-2] = "composer v2"

	diffStart := 0
	if prefixEqual(r.previousLines, next, 1000) {
		diffStart = 1000
	}

	firstChanged := -1
	maxLines := max(len(next), len(r.previousLines))
	for i := diffStart; i < maxLines; i++ {
		if r.previousLines[i] != next[i] {
			firstChanged = i
			break
		}
	}
	if firstChanged != 1000 {
		t.Fatalf("expected first change at 1000, got %d", firstChanged)
	}
}
