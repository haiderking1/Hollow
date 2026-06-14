package opencode

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// ImagePart represents the mime type and raw bytes of an image to be attached.
type ImagePart struct {
	MIMEType string
	Data     []byte // raw bytes; encode to data URL at API boundary
}

// ToolContentBlock is shared across agent, verifier, and swarm.
type ToolContentBlock struct {
	Type     string // "text" | "image"
	Text     string
	Data     string // base64
	MIMEType string
}

// UserContent builds user content blocks from text and image parts.
func UserContent(text string, images []ImagePart) json.RawMessage {
	if len(images) == 0 {
		return StringContent(text)
	}
	var blocks []ContentBlock
	if text != "" {
		blocks = append(blocks, ContentBlock{
			Type: "text",
			Text: text,
		})
	}
	for _, img := range images {
		encoded := base64.StdEncoding.EncodeToString(img.Data)
		dataURL := fmt.Sprintf("data:%s;base64,%s", img.MIMEType, encoded)
		blocks = append(blocks, ContentBlock{
			Type: "image_url",
			ImageURL: &ContentImageURL{
				URL: dataURL,
			},
		})
	}
	return BlocksContent(blocks)
}

// ToolContentFromAgent maps internal agent-style ToolContentBlocks to opencode ContentBlocks.
func ToolContentFromAgent(blocks []ToolContentBlock) json.RawMessage {
	if len(blocks) == 0 {
		return nil
	}
	var out []ContentBlock
	for _, block := range blocks {
		if block.Type == "text" {
			out = append(out, ContentBlock{
				Type: "text",
				Text: block.Text,
			})
		} else if block.Type == "image" {
			out = append(out, ContentBlock{
				Type: "image_url",
				ImageURL: &ContentImageURL{
					URL: fmt.Sprintf("data:%s;base64,%s", block.MIMEType, block.Data),
				},
			})
		}
	}
	return BlocksContent(out)
}
