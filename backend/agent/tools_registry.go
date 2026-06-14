package agent

import (
	"encoding/json"

	"github.com/enough/enough/backend/config"
	"github.com/enough/enough/backend/opencode"
)

// nativeTools is the full tool set available to the main (coder) agent,
// including agent_swarm for parallel sub-agents.
func nativeTools(cfg config.Runtime) []opencode.Tool {
	tools := []opencode.Tool{
		readFileTool(),
		writeFileTool(),
		editFileTool(),
		listDirTool(),
		globTool(),
		grepTool(),
		bashTool(),
		webSearchTool(),
		webFetchTool(),
		browserTool(),
		agentSwarmTool(),
	}
	if cfg.Skills.Enabled {
		tools = append(tools, skillsListTool())
		tools = append(tools, skillViewTool())
		tools = append(tools, skillManageTool())
	}
	return tools
}

// workerTools returns the coding toolset for a swarm worker. Workers at depth
// below maxSwarmDepth also get a nested agent_swarm tool.
func workerTools(depth int) []opencode.Tool {
	tools := []opencode.Tool{
		readFileTool(),
		writeFileTool(),
		editFileTool(),
		listDirTool(),
		globTool(),
		grepTool(),
		bashTool(),
		webSearchTool(),
		webFetchTool(),
		browserTool(),
	}
	if depth < maxSwarmDepth {
		tools = append(tools, agentSwarmTool())
	}
	return tools
}

// verifierTools is the verifier role's set: read-only discovery plus bash
// for running verification commands. Must stay in sync with
// verifierAllowedTools — the allowlist is the enforcement, this is the menu.
func verifierTools() []opencode.Tool {
	return []opencode.Tool{
		readFileTool(),
		listDirTool(),
		globTool(),
		grepTool(),
		bashTool(),
	}
}

// plannerTools is the read-only subset for the swarm goal planner.
func plannerTools() []opencode.Tool {
	return []opencode.Tool{
		readFileTool(),
		listDirTool(),
		globTool(),
		grepTool(),
	}
}

func skillsListTool() opencode.Tool {
	return opencode.Tool{
		Type: "function",
		Function: opencode.ToolFunction{
			Name:        "skills_list",
			Description: "List available skills (name + description). Use skill_view(name) to load full content.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"category": {
						"type": "string",
						"description": "Optional category filter (e.g., 'github')"
					}
				}
			}`),
		},
	}
}

func skillViewTool() opencode.Tool {
	return opencode.Tool{
		Type: "function",
		Function: opencode.ToolFunction{
			Name:        "skill_view",
			Description: "Skills allow for loading information about specific tasks and workflows, as well as scripts and templates. Load a skill's full content or access its linked files (references, templates, scripts). First call returns SKILL.md content plus a 'linked_files' dict showing available references/templates/scripts. To access those, call again with file_path parameter.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"name": {
						"type": "string",
						"description": "The skill name (use skills_list to see available skills). For categorized skills, use 'category/skill-name'."
					},
					"file_path": {
						"type": "string",
						"description": "OPTIONAL: Path to a linked file within the skill (e.g., 'references/api.md'). Omit to get SKILL.md content."
					}
				},
				"required": ["name"]
			}`),
		},
	}
}

func skillManageTool() opencode.Tool {
	return opencode.Tool{
		Type: "function",
		Function: opencode.ToolFunction{
			Name:        "skill_manage",
			Description: "Manage skills (create, update, delete). Skills are your procedural memory — reusable approaches for recurring task types. New skills go to ~/.enough/skills/; existing skills can be modified wherever they live.\n\nActions: create (full SKILL.md + optional category), patch (old_string/new_string — preferred for fixes), edit (full SKILL.md rewrite — major overhauls only), delete, write_file, remove_file.\n\nOn delete, pass absorbed_into=<umbrella> when merging into another skill, or absorbed_into=\"\" when pruning with no target.\n\nCreate when: complex task succeeded (5+ calls), errors overcome, user-corrected approach worked, non-trivial workflow discovered, or user asks you to remember a procedure.\nUpdate when: instructions stale/wrong, missing steps or pitfalls found during use. If you used a skill and hit issues not covered by it, patch it immediately.\n\nAfter difficult/iterative tasks, offer to save as a skill. Skip for simple one-offs. Confirm with user before creating/deleting.\n\nGood skills: trigger conditions, numbered steps with exact commands, pitfalls section, verification steps. Use skill_view() to see format examples.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"action": {
						"type": "string",
						"enum": ["create", "patch", "edit", "delete", "write_file", "remove_file"],
						"description": "The action to perform."
					},
					"name": {
						"type": "string",
						"description": "Skill name (lowercase, hyphens/underscores, max 64 chars). Must match an existing skill for patch/edit/delete/write_file/remove_file."
					},
					"content": {
						"type": "string",
						"description": "Full SKILL.md content (YAML frontmatter + markdown body). Required for 'create' and 'edit'."
					},
					"old_string": {
						"type": "string",
						"description": "Text to find in the file (required for 'patch'). Must be unique unless replace_all=true."
					},
					"new_string": {
						"type": "string",
						"description": "Replacement text (required for 'patch'). Can be empty string to delete the matched text."
					},
					"replace_all": {
						"type": "boolean",
						"description": "For 'patch': replace all occurrences instead of requiring a unique match (default: false)."
					},
					"category": {
						"type": "string",
						"description": "Optional category/domain for organizing the skill (e.g., 'devops'). Creates a subdirectory grouping. Only used with 'create'."
					},
					"file_path": {
						"type": "string",
						"description": "Path to a supporting file within the skill directory. For write_file/remove_file: required. For patch: optional, defaults to SKILL.md."
					},
					"file_content": {
						"type": "string",
						"description": "Content for the file. Required for 'write_file'."
					},
					"absorbed_into": {
						"type": "string",
						"description": "For 'delete' only — umbrella skill name when merging content, or empty string when pruning with no target."
					}
				},
				"required": ["action", "name"]
			}`),
		},
	}
}
