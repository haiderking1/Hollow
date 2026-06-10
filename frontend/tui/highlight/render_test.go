package highlight

import (
	"strings"
	"testing"
)

func TestParseBlocks(t *testing.T) {
	text := "hello\n```js\nconst x = 1;\n```\nworld"
	blocks := ParseBlocks(text)
	if len(blocks) != 3 {
		t.Fatalf("got %d blocks", len(blocks))
	}
	if blocks[0].Kind != BlockProse || blocks[0].Text != "hello\n" {
		t.Fatalf("prose 0: %+v", blocks[0])
	}
	if blocks[1].Kind != BlockCode || blocks[1].Lang != "javascript" || blocks[1].Text != "const x = 1;" {
		t.Fatalf("code: %+v", blocks[1])
	}
	if blocks[2].Kind != BlockProse || blocks[2].Text != "\nworld" {
		t.Fatalf("prose 2: %+v", blocks[2])
	}
}

func TestHighlightCodeJavaScript(t *testing.T) {
	p := GruvboxDark()
	out := HighlightCode("js", `const msg = "hi"; // greet`, p)
	if !strings.Contains(out, p.Keyword) || !strings.Contains(out, "const") {
		t.Fatalf("expected keyword color: %q", out)
	}
	if !strings.Contains(out, p.String) || !strings.Contains(out, `"hi"`) {
		t.Fatalf("expected string color: %q", out)
	}
	if !strings.Contains(out, p.Comment) || !strings.Contains(out, "// greet") {
		t.Fatalf("expected comment color: %q", out)
	}
	if strings.Contains(out, "\033[48") {
		t.Fatalf("unexpected background color: %q", out)
	}
}

func TestStylizeBold(t *testing.T) {
	out := stylizeProse("**YOOOOO THIS ENERGY IS IT.**", TextStyle{
		Plain:  func(s string) string { return "P:" + s },
		Bold:   func(s string) string { return "B:" + s },
		Italic: func(s string) string { return "I:" + s },
	}, GruvboxDark())
	if strings.Contains(out, "**") {
		t.Fatalf("expected asterisks removed: %q", out)
	}
	if !strings.Contains(out, "B:YOOOOO THIS ENERGY IS IT.") {
		t.Fatalf("expected bold segment: %q", out)
	}
}

func TestRenderInlineCode(t *testing.T) {
	p := GruvboxDark()
	out := Render("use `npm run build` here", 40, TextStyle{
		Plain: func(s string) string { return s },
	})
	if !strings.Contains(out, p.Inline) {
		t.Fatalf("expected inline code styling: %q", out)
	}
	if strings.Contains(out, "\033[48") {
		t.Fatalf("unexpected background color: %q", out)
	}
	if !strings.Contains(out, "npm run build") {
		t.Fatalf("missing inline code text: %q", out)
	}
}

func TestRenderCodeFence(t *testing.T) {
	text := "```go\nfunc main() {}\n```"
	out := Render(text, 80, TextStyle{
		Plain: func(s string) string { return s },
	})
	p := GruvboxDark()
	if !strings.Contains(out, p.Keyword) {
		t.Fatalf("expected keyword color: %q", out)
	}
	if strings.Contains(out, "\033[48") {
		t.Fatalf("unexpected background color: %q", out)
	}
	if !strings.Contains(out, "func") {
		t.Fatalf("missing code content: %q", out)
	}
	if !strings.Contains(out, "go") {
		t.Fatalf("missing lang label: %q", out)
	}
}
