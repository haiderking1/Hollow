package memory

import (
	"encoding/json"
)

// ToolName is the native tool's registered name.
const ToolName = "memory"

// ToolDescription is the behavioral guidance shipped in the tool schema.
// Ported from Hermes' memory tool (tools/memory_tool.py), adapted to Enough
// (skills instead of session_search for procedures; no session search tool).
const ToolDescription = "Save durable information to persistent memory that survives across sessions. " +
	"Memory is injected into future turns, so keep it compact and focused on facts " +
	"that will still matter later.\n\n" +
	"WHEN TO SAVE (do this proactively, don't wait to be asked):\n" +
	"- User corrects you or says 'remember this' / 'don't do that again'\n" +
	"- User shares a preference, habit, or personal detail (name, role, timezone, coding style)\n" +
	"- You discover something about the environment (OS, installed tools, project structure)\n" +
	"- You learn a convention, API quirk, or workflow specific to this user's setup\n" +
	"- You identify a stable fact that will be useful again in future sessions\n\n" +
	"PRIORITY: User preferences and corrections > environment facts > procedural knowledge. " +
	"The most valuable memory prevents the user from having to repeat themselves.\n\n" +
	"Do NOT save task progress, session outcomes, completed-work logs, or temporary TODO " +
	"state to memory.\n" +
	"If you've discovered a new way to do something, solved a problem that could be " +
	"necessary later, save it as a skill with skill_manage.\n\n" +
	"TWO TARGETS:\n" +
	"- 'user': who the user is — name, role, preferences, communication style, pet peeves\n" +
	"- 'memory': your notes — environment facts, project conventions, tool quirks, lessons learned\n\n" +
	"ACTIONS: add (new entry), replace (update existing — match identifies it), " +
	"remove (delete — match identifies it), read (show live state).\n\n" +
	"SKIP: trivial/obvious info, things easily re-discovered, raw data dumps, and temporary task state."

// ToolParameters is the JSONSchema for the memory tool.
const ToolParameters = `{
	"type": "object",
	"properties": {
		"action": {
			"type": "string",
			"enum": ["add", "replace", "remove", "read"],
			"description": "The action to perform."
		},
		"target": {
			"type": "string",
			"enum": ["memory", "user"],
			"description": "Which memory store: 'memory' for personal notes, 'user' for user profile."
		},
		"content": {
			"type": "string",
			"description": "The entry content. Required for 'add'."
		},
		"match": {
			"type": "string",
			"description": "Short unique substring identifying the entry to replace or remove."
		},
		"replacement": {
			"type": "string",
			"description": "The new entry content. Required for 'replace'."
		}
	},
	"required": ["action", "target"]
}`

type toolArgs struct {
	Action      string `json:"action"`
	Target      string `json:"target"`
	Content     string `json:"content"`
	Match       string `json:"match"`
	Replacement string `json:"replacement"`
}

// ExecuteMemoryTool dispatches a memory tool call against store. Returns the
// JSON tool result and whether it is an error.
func ExecuteMemoryTool(argsJSON string, store *Store) (string, bool) {
	if store == nil {
		return marshal(Result{Success: false, Error: "Memory is not available. It may be disabled in config or this environment."})
	}

	var args toolArgs
	_ = json.Unmarshal([]byte(argsJSON), &args)

	if args.Target == "" {
		args.Target = TargetMemory
	}
	if args.Target != TargetMemory && args.Target != TargetUser {
		return marshal(Result{Success: false, Error: "Invalid target '" + args.Target + "'. Use 'memory' or 'user'."})
	}

	switch args.Action {
	case "add":
		if args.Content == "" {
			return marshal(Result{Success: false, Error: "content is required for 'add' action."})
		}
		return marshal(store.Add(args.Target, args.Content))
	case "replace":
		if args.Match == "" {
			return marshal(Result{Success: false, Error: "match is required for 'replace' action."})
		}
		if args.Replacement == "" {
			return marshal(Result{Success: false, Error: "replacement is required for 'replace' action."})
		}
		return marshal(store.Replace(args.Target, args.Match, args.Replacement))
	case "remove":
		if args.Match == "" {
			return marshal(Result{Success: false, Error: "match is required for 'remove' action."})
		}
		return marshal(store.Remove(args.Target, args.Match))
	case "read":
		return marshal(store.Read(args.Target))
	default:
		return marshal(Result{Success: false, Error: "Unknown action '" + args.Action + "'. Use: add, replace, remove, read"})
	}
}

func ApplyMemoryPending(payload map[string]interface{}, store *Store) Result {
	action := getStringField(payload, "action")
	target := getStringField(payload, "target")
	if target == "" {
		target = TargetMemory
	}
	content := getStringField(payload, "content")
	match := getStringField(payload, "match")
	if match == "" {
		match = getStringField(payload, "old_text")
	}
	replacement := getStringField(payload, "replacement")

	switch action {
	case "add":
		return store.Add(target, content)
	case "replace":
		return store.Replace(target, match, replacement)
	case "remove":
		return store.Remove(target, match)
	default:
		return Result{Success: false, Error: "Unknown staged action '" + action + "'"}
	}
}

func getStringField(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	if val, ok := m[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

// IsMutatingAction reports whether the given tool-call args describe a write.
// Used by the agent to reset the memory-review nudge counter on direct
// foreground memory writes.
func IsMutatingAction(argsJSON string) bool {
	var args toolArgs
	_ = json.Unmarshal([]byte(argsJSON), &args)
	switch args.Action {
	case "add", "replace", "remove":
		return true
	}
	return false
}

func marshal(r Result) (string, bool) {
	out, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return `{"success": false, "error": "json marshal error"}`, true
	}
	return string(out), !r.Success
}
