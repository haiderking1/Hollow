// PORT: backend/memory/tool.go

import { Effect } from "effect";
import { Store, Result, TargetMemory, TargetUser } from "./store";

// ToolName is the native tool's registered name.
export const ToolName = "memory";

// ToolDescription is the behavioral guidance shipped in the tool schema.
// Ported from Hermes' memory tool (tools/memory_tool.py), adapted to Hollow
// (skills instead of session_search for procedures; no session search tool).
export const ToolDescription =
  "Save durable information to persistent memory that survives across sessions. " +
  "Memory is injected into future turns, so keep it compact and focused on facts " +
  "that will still matter later.\n\n" +
  "WHEN TO SAVE (do this proactively, don't wait to be asked):\n" +
  "- User corrects you or says 'remember this' / 'don't do that again'\n" +
  "- User clarifies or corrects how you interpreted USER PROFILE (name, nickname, spelling)\n" +
  "- User shares a preference, habit, or personal detail (name, role, timezone, coding style)\n" +
  "- You discover something about the environment (OS, installed tools, project structure)\n" +
  "- You learn a convention, API quirk, or workflow specific to this user's setup\n" +
  "- You identify a stable fact that will be useful again in future sessions\n\n" +
  "PRIORITY: User preferences and corrections > environment facts > procedural knowledge. " +
  "The most valuable memory prevents the user from having to repeat themselves.\n\n" +
  "When correcting USER PROFILE entries, use action=replace (not add) so ambiguous " +
  "wording does not linger.\n\n" +
  "Do NOT save task progress, session outcomes, completed-work logs, or temporary TODO " +
  "state to memory.\n" +
  "If you've discovered a new way to do something, solved a problem that could be " +
  "necessary later, save it as a skill with skill_manage.\n\n" +
  "TWO TARGETS:\n" +
  "- 'user': who the user is — name, role, preferences, communication style, pet peeves\n" +
  "- 'memory': your notes — environment facts, project conventions, tool quirks, lessons learned\n\n" +
  "ACTIONS: add (new entry), replace (update existing — match identifies it), " +
  "remove (delete — match identifies it), read (show live state).\n\n" +
  "SKIP: trivial/obvious info, things easily re-discovered, raw data dumps, and temporary task state.";

// ToolParameters is the JSONSchema for the memory tool.
export const ToolParameters = `{
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
}`;

export interface ToolArgs {
  action: string;
  target?: string;
  content?: string;
  match?: string;
  replacement?: string;
}

// ExecuteMemoryTool dispatches a memory tool call against store. Returns the
// JSON tool result and whether it is an error.
export function ExecuteMemoryTool(
  argsJSON: string,
  store: Store | null
): Effect.Effect<[string, boolean], Error> {
  if (store === null || store === undefined) {
    return Effect.succeed(
      marshal({
        success: false,
        error: "Memory is not available. It may be disabled in config or this environment.",
      })
    );
  }

  let args: ToolArgs;
  try {
    args = JSON.parse(argsJSON);
  } catch {
    args = {} as any;
  }

  let target = args.target;
  if (!target) {
    target = TargetMemory;
  }
  if (target !== TargetMemory && target !== TargetUser) {
    return Effect.succeed(
      marshal({
        success: false,
        error: `Invalid target '${target}'. Use 'memory' or 'user'.`,
      })
    );
  }

  switch (args.action) {
    case "add":
      if (!args.content) {
        return Effect.succeed(marshal({ success: false, error: "content is required for 'add' action." }));
      }
      return store.add(target, args.content).pipe(Effect.map((res) => marshal(res)));
    case "replace":
      if (!args.match) {
        return Effect.succeed(marshal({ success: false, error: "match is required for 'replace' action." }));
      }
      if (!args.replacement) {
        return Effect.succeed(marshal({ success: false, error: "replacement is required for 'replace' action." }));
      }
      return store.replace(target, args.match, args.replacement).pipe(Effect.map((res) => marshal(res)));
    case "remove":
      if (!args.match) {
        return Effect.succeed(marshal({ success: false, error: "match is required for 'remove' action." }));
      }
      return store.remove(target, args.match).pipe(Effect.map((res) => marshal(res)));
    case "read":
      return Effect.succeed(marshal(store.read(target)));
    default:
      return Effect.succeed(
        marshal({
          success: false,
          error: `Unknown action '${args.action}'. Use: add, replace, remove, read`,
        })
      );
  }
}

export function ApplyMemoryPending(
  payload: Record<string, any>,
  store: Store
): Effect.Effect<Result, Error> {
  const action = getStringField(payload, "action");
  let target = getStringField(payload, "target");
  if (target === "") {
    target = TargetMemory;
  }
  const content = getStringField(payload, "content");
  let match = getStringField(payload, "match");
  if (match === "") {
    match = getStringField(payload, "old_text");
  }
  const replacement = getStringField(payload, "replacement");

  switch (action) {
    case "add":
      return store.add(target, content);
    case "replace":
      return store.replace(target, match, replacement);
    case "remove":
      return store.remove(target, match);
    default:
      return Effect.succeed({ success: false, error: `Unknown staged action '${action}'` });
  }
}

function getStringField(m: Record<string, any> | null | undefined, key: string): string {
  if (!m) {
    return "";
  }
  const val = m[key];
  if (typeof val === "string") {
    return val;
  }
  return "";
}

// IsMutatingAction reports whether the given tool-call args describe a write.
// Used by the agent to reset the memory-review nudge counter on direct
// foreground memory writes.
export function IsMutatingAction(argsJSON: string): boolean {
  let args: ToolArgs;
  try {
    args = JSON.parse(argsJSON);
  } catch {
    return false;
  }
  switch (args.action) {
    case "add":
    case "replace":
    case "remove":
      return true;
  }
  return false;
}

function marshal(r: Result): [string, boolean] {
  try {
    const out = JSON.stringify(r, null, "  ");
    return [out, !r.success];
  } catch {
    return [JSON.stringify({ success: false, error: "json marshal error" }), true];
  }
}

/*
PORT STATUS
source path: backend/memory/tool.go
source lines: 176
draft lines: 189
confidence: high
status: phase_b_compile
*/
