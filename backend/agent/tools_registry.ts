import { type tool } from "../opencode/types";
import { type runtime } from "../config/config";
import { readFileTool } from "./read_file";
import { writeFileTool } from "./write_file";
import { editFileTool } from "./edit_file";
import { listDirTool } from "./list_dir";
import { globTool } from "./glob";
import { grepTool } from "./grep";
import { bashTool } from "./bash";
import { webSearchTool } from "./web_search";
import { webFetchTool } from "./web_fetch";
import { browserTool } from "./browser";
import { maxSwarmDepth } from "./agent";

export function nativeTools(cfg: runtime): tool[] {
  const tools: tool[] = [
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
  ];
  if (cfg.skills.enabled) {
    tools.push(skillsListTool());
    tools.push(skillViewTool());
    tools.push(skillManageTool());
  }
  return tools;
}

export function workerTools(depth: number): tool[] {
  const tools: tool[] = [
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
  ];
  if (depth < maxSwarmDepth) {
    tools.push(agentSwarmTool());
  }
  return tools;
}

export function verifierTools(): tool[] {
  return [
    readFileTool(),
    listDirTool(),
    globTool(),
    grepTool(),
    bashTool(),
  ];
}

export function plannerTools(): tool[] {
  return [
    readFileTool(),
    listDirTool(),
    globTool(),
    grepTool(),
  ];
}

export function skillsListTool(): tool {
  const schema = {
    type: "object",
    properties: {
      category: {
        type: "string",
        description: "Optional category filter (e.g., 'github')"
      }
    }
  };
  return {
    type: "function",
    function: {
      name: "skills_list",
      description: "List available skills (name + description). Use skill_view(name) to load full content.",
      parameters: new TextEncoder().encode(JSON.stringify(schema)),
    }
  };
}

export function skillViewTool(): tool {
  const schema = {
    type: "object",
    properties: {
      name: {
        type: "string",
        description: "The skill name (use skills_list to see available skills). For categorized skills, use 'category/skill-name'."
      },
      file_path: {
        type: "string",
        description: "OPTIONAL: Path to a linked file within the skill (e.g., 'references/api.md'). Omit to get SKILL.md content."
      }
    },
    required: ["name"]
  };
  return {
    type: "function",
    function: {
      name: "skill_view",
      description: "Skills allow for loading information about specific tasks and workflows, as well as scripts and templates. Load a skill's full content or access its linked files (references, templates, scripts). First call returns SKILL.md content plus a 'linked_files' dict showing available references/templates/scripts. To access those, call again with file_path parameter.",
      parameters: new TextEncoder().encode(JSON.stringify(schema)),
    }
  };
}

export function skillManageTool(): tool {
  const schema = {
    type: "object",
    properties: {
      action: {
        type: "string",
        enum: ["create", "patch", "edit", "delete", "write_file", "remove_file"],
        description: "The action to perform."
      },
      name: {
        type: "string",
        description: "Skill name (lowercase, hyphens/underscores, max 64 chars). Must match an existing skill for patch/edit/delete/write_file/remove_file."
      },
      content: {
        type: "string",
        description: "Full SKILL.md content (YAML frontmatter + markdown body). Required for 'create' and 'edit'."
      },
      old_string: {
        type: "string",
        description: "Text to find in the file (required for 'patch'). Must be unique unless replace_all=true."
      },
      new_string: {
        type: "string",
        description: "Replacement text (required for 'patch'). Can be empty string to delete the matched text."
      },
      replace_all: {
        type: "boolean",
        description: "For 'patch': replace all occurrences instead of requiring a unique match (default: false)."
      },
      category: {
        type: "string",
        description: "Optional category/domain for organizing the skill (e.g., 'devops'). Creates a subdirectory grouping. Only used with 'create'."
      },
      file_path: {
        type: "string",
        description: "Path to a supporting file within the skill directory. For write_file/remove_file: required. For patch: optional, defaults to SKILL.md."
      },
      file_content: {
        type: "string",
        description: "Content for the file. Required for 'write_file'."
      },
      absorbed_into: {
        type: "string",
        description: "For 'delete' only — umbrella skill name when merging content, or empty string when pruning with no target."
      }
    },
    required: ["action", "name"]
  };
  return {
    type: "function",
    function: {
      name: "skill_manage",
      description: "Manage skills (create, update, delete). Skills are your procedural memory — reusable approaches for recurring task types. New skills go to ~/.hollow/skills/; existing skills can be modified wherever they live.\n\nActions: create (full SKILL.md + optional category), patch (old_string/new_string — preferred for fixes), edit (full SKILL.md rewrite — major overhauls only), delete, write_file, remove_file.\n\nOn delete, pass absorbed_into=<umbrella> when merging into another skill, or absorbed_into=\"\" when pruning with no target.\n\nCreate when: complex task succeeded (5+ calls), errors overcome, user-corrected approach worked, non-trivial workflow discovered, or user asks you to remember a procedure.\nUpdate when: instructions stale/wrong, missing steps or pitfalls found during use. If you used a skill and hit issues not covered by it, patch it immediately.\n\nAfter difficult/iterative tasks, offer to save as a skill. Skip for simple one-offs. Confirm with user before creating/deleting.\n\nGood skills: trigger conditions, numbered steps with exact commands, pitfalls section, verification steps. Use skill_view() to see format examples.",
      parameters: new TextEncoder().encode(JSON.stringify(schema)),
    }
  };
}

export function agentSwarmTool(): tool {
  const schema = {
    type: "object",
    properties: {
      goal: {
        type: "string",
        description: "High-level goal to auto-decompose into independent parallel subtasks via a planner agent. Provide this OR tasks; if both are given, tasks win."
      },
      tasks: {
        type: "array",
        items: {
          type: "object",
          properties: {
            id: {
              type: "string",
              description: "Optional short label for this agent's task. Defaults to agent-<n>."
            },
            prompt: {
              type: "string",
              description: "The full, self-contained instruction for this agent. It runs in isolation with no memory of the other agents."
            },
            depends_on: {
              type: "array",
              items: { type: "string" },
              description: "Ids of other tasks in THIS call that must finish before this agent starts. The dependent receives those agents' outputs in its prompt."
            }
          },
          required: ["prompt"]
        },
        description: "One entry per agent to spawn. Each agent runs in parallel in its own fresh context with the standard coding tools."
      },
      shared_context: {
        type: "string",
        description: "Optional briefing prepended to every agent's prompt."
      },
      max_concurrency: {
        type: "number",
        description: "Maximum number of agents running at the same time. Defaults to 16."
      },
      retry: {
        type: "number",
        description: "How many times to retry an agent that errors or returns nothing. Default 1."
      },
      max_turns_per_agent: {
        type: "number",
        description: "Do not use unless the user explicitly asks for a cap. Each agent runs to completion with no turn limit by default."
      },
      isolate: {
        type: "string",
        enum: ["worktree"],
        description: "Set to \"worktree\" to run each agent in its own git worktree/branch so parallel edits never collide. Dirty worktrees are left for review; clean ones are removed."
      }
    }
  };
  return {
    type: "function",
    function: {
      name: "agent_swarm",
      description: "Run many sub-agents in parallel. Pass \"tasks\" (one self-contained prompt each) or a single \"goal\" to have a planner split it into parallel subtasks. Each agent gets a fresh, isolated context with the standard coding tools scoped to the current directory. Up to 16 agents per call; max_concurrency (default 16) run at once. Sub-agents use the same Hollow agent capabilities. Agents run with no turn limit by default (like you). Do not set max_turns_per_agent unless the user explicitly requests a cap. Agents can nest agent_swarm up to 3 nested swarm calls; from a top-level call this supports a four-worker chain (level1 -> level2 -> level3 -> level4). Keep tasks disjoint to avoid conflicting edits.",
      parameters: new TextEncoder().encode(JSON.stringify(schema)),
    }
  };
}

