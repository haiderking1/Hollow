package agent

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/enough/enough/backend/config"
)

const png1x1Base64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGNgYGD4DwABBAEAX+XDSwAAAABJRU5ErkJggg=="

func TestToolReadFileReportsLineCount(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]struct {
		content   string
		wantLines int
	}{
		"trailing newline":    {"a\nb\nc\n", 3},
		"no trailing newline": {"a\nb\nc", 3},
		"single line":         {"only", 1},
		"empty":               {"", 0},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name+".txt")
			mustWrite(t, path, tc.content)
			a := &Agent{workDir: dir}
			res := a.toolReadFile(`{"path":"` + path + `"}`)
			if res.isErr {
				t.Fatalf("read_file error: %s", res.output)
			}
			wantHeader := "Read " + strconv.Itoa(tc.wantLines) + " lines from "
			if !strings.HasPrefix(res.output, wantHeader) {
				t.Fatalf("expected header %q, got %q", wantHeader, res.output)
			}
		})
	}
}

func TestToolReadFileTruncatedOutputReportsFullLineCount(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.txt")
	content := strings.Repeat("line\n", 15000)
	mustWrite(t, path, content)

	a := &Agent{workDir: dir}
	res := a.toolReadFile(`{"path":"` + path + `"}`)
	if res.isErr {
		t.Fatalf("read_file error: %s", res.output)
	}
	if !strings.HasPrefix(res.output, "Read 15000 lines from ") {
		t.Fatalf("expected full line count in header, got %q", strings.SplitN(res.output, "\n", 2)[0])
	}
	if !strings.Contains(res.output, "... truncated ...") {
		t.Fatalf("expected truncation marker")
	}
}

func TestReadFileImageSupport(t *testing.T) {
	dir := t.TempDir()

	pngBytes, err := base64.StdEncoding.DecodeString(png1x1Base64)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("valid image read", func(t *testing.T) {
		path := filepath.Join(dir, "test.png")
		if err := os.WriteFile(path, pngBytes, 0600); err != nil {
			t.Fatal(err)
		}
		cfg := config.Runtime{}
		cfg.Model = "gpt-5" // vision-supporting model
		a := &Agent{workDir: dir, cfg: cfg}
		res := a.toolReadFile(`{"path":"` + path + `"}`)
		if res.isErr {
			t.Fatalf("toolReadFile error: %s", res.output)
		}
		if !strings.Contains(res.output, "Read image file [image/png]") {
			t.Errorf("expected header to mention image/png, got %q", res.output)
		}
		if len(res.content) != 2 {
			t.Fatalf("expected 2 content blocks, got %d", len(res.content))
		}
		if res.content[0].Type != "text" || !strings.Contains(res.content[0].Text, "Read image file [image/png]") {
			t.Errorf("first block is not the expected text block: %+v", res.content[0])
		}
		if res.content[1].Type != "image" || res.content[1].MIMEType != "image/png" || res.content[1].Data == "" {
			t.Errorf("second block is not the expected image block: %+v", res.content[1])
		}
	})

	t.Run("fake png text read", func(t *testing.T) {
		path := filepath.Join(dir, "fake.png")
		mustWrite(t, path, "This is not an image even though it ends with .png")
		cfg := config.Runtime{}
		cfg.Model = "gpt-5"
		a := &Agent{workDir: dir, cfg: cfg}
		res := a.toolReadFile(`{"path":"` + path + `"}`)
		if res.isErr {
			t.Fatalf("toolReadFile error: %s", res.output)
		}
		if !strings.HasPrefix(res.output, "Read 1 lines from ") {
			t.Errorf("expected text read header, got %q", res.output)
		}
		if len(res.content) != 0 {
			t.Errorf("expected no content blocks for text file, got %d", len(res.content))
		}
	})

	t.Run("non-vision model", func(t *testing.T) {
		path := filepath.Join(dir, "test_non_vision.png")
		if err := os.WriteFile(path, pngBytes, 0600); err != nil {
			t.Fatal(err)
		}
		cfg := config.Runtime{}
		cfg.Model = "deepseek-v4-flash" // non-vision model
		a := &Agent{workDir: dir, cfg: cfg}
		res := a.toolReadFile(`{"path":"` + path + `"}`)
		if res.isErr {
			t.Fatalf("toolReadFile error: %s", res.output)
		}
		if !strings.Contains(res.output, "Current model does not support images") {
			t.Errorf("expected warning in output, got %q", res.output)
		}
		if len(res.content) != 1 {
			t.Fatalf("expected 1 content block (text only), got %d", len(res.content))
		}
		if res.content[0].Type != "text" || !strings.Contains(res.content[0].Text, "Current model does not support images") {
			t.Errorf("expected warning in text block, got %+v", res.content[0])
		}
	})
}
