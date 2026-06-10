package agent

import (
	"encoding/json"

	"github.com/enough/enough/backend/opencode"
)

func nativeTools() []opencode.Tool {
	return []opencode.Tool{
		{
			Type: "function",
			Function: opencode.ToolFunction{
				Name:        "read_file",
				Description: "Read a file from the project workspace",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"path": {"type": "string", "description": "Relative or absolute path"}
					},
					"required": ["path"]
				}`),
			},
		},
		{
			Type: "function",
			Function: opencode.ToolFunction{
				Name:        "write_file",
				Description: "Write content to a file in the workspace",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"path": {"type": "string"},
						"content": {"type": "string"}
					},
					"required": ["path", "content"]
				}`),
			},
		},
		{
			Type: "function",
			Function: opencode.ToolFunction{
				Name:        "list_dir",
				Description: "List entries in a directory",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"path": {"type": "string", "description": "Directory path, default ."}
					}
				}`),
			},
		},
		{
			Type: "function",
			Function: opencode.ToolFunction{
				Name:        "bash",
				Description: "Run a shell command in the project workspace",
				Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"command": {"type": "string"}
					},
					"required": ["command"]
				}`),
			},
		},
	}
}
