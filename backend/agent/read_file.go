package agent

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/enough/enough/backend/imageutil"
	"github.com/enough/enough/backend/opencode"
)

func readFileTool() opencode.Tool {
	return opencode.Tool{
		Type: "function",
		Function: opencode.ToolFunction{
			Name:        "read_file",
			Description: "Read the contents of a file. Supports text files and images (jpg, png, gif, webp). Images are sent as attachments. For text files, output is truncated.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"path": {"type": "string", "description": "Relative or absolute path"}
				},
				"required": ["path"]
			}`),
		},
	}
}

func (a *Agent) toolReadFile(argsJSON string) toolResult {
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return toolResult{output: err.Error(), isErr: true}
	}

	path, err := a.resolvePath(args.Path)
	if err != nil {
		return toolResult{output: err.Error(), isErr: true}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return toolResult{output: err.Error(), isErr: true}
	}

	mimeType := imageutil.DetectSupportedImageMimeType(data)
	if mimeType != "" {
		supportsVision := opencode.SupportsImages(a.cfg.Model)
		resizedData, w, h, origW, origH, wasResized, resizeErr := imageutil.ResizeImage(data, mimeType)

		if resizeErr != nil {
			textNote := fmt.Sprintf("Read image file [%s]\n[Image omitted: could not be resized below the inline image size limit.]", mimeType)
			if !supportsVision {
				textNote += "\n[Current model does not support images. The image will be omitted from this request.]"
			}
			return toolResult{
				output: textNote,
				content: []ToolContentBlock{
					{Type: "text", Text: textNote},
				},
			}
		}

		textNote := fmt.Sprintf("Read image file [%s]\n%d\u00d7%d", mimeType, w, h)
		if wasResized {
			scale := float64(origW) / float64(w)
			textNote += fmt.Sprintf("\n[Image: original %dx%d, displayed at %dx%d. Multiply coordinates by %.2f to map to original image.]", origW, origH, w, h, scale)
		}
		if !supportsVision {
			textNote += "\n[Current model does not support images. The image will be omitted from this request.]"
		}

		contentBlocks := []ToolContentBlock{
			{Type: "text", Text: textNote},
		}
		if supportsVision {
			contentBlocks = append(contentBlocks, ToolContentBlock{
				Type:     "image",
				Data:     base64.StdEncoding.EncodeToString(resizedData),
				MIMEType: mimeType,
			})
		}

		return toolResult{
			output:  textNote,
			content: contentBlocks,
		}
	}

	const max = 64_000
	out := string(data)
	truncated := false
	if len(out) > max {
		out = out[:max]
		truncated = true
	}

	// Header carries the line count so callers never need an external `wc -l`.
	// Counting on the full data, not the truncated view, keeps the total accurate.
	lines := strings.Count(string(data), "\n")
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		lines++ // final line without a trailing newline still counts
	}
	header := fmt.Sprintf("Read %d lines from %s\n", lines, path)
	if truncated {
		out += "\n... truncated ..."
	}
	return toolResult{output: header + out}
}
