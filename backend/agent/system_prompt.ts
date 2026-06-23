// PORT: backend/agent/system_prompt.go

import fs from "node:fs";
import path from "node:path";
import { type runtime } from "../config/config";
import { LoadSoul } from "../memory/soul";
import { ToolName } from "../memory/tool";
import { TargetMemory, TargetUser } from "../memory/store";
import { ContextThreatIDs } from "../memory/scan";
import { GuidanceBlock } from "../skills/prompt_strings";
import { BuildIndexPrompt } from "../skills/prompt_index";
import { DiscoverAllSkills } from "../skills/discovery";
import { FormatSkillsForPrompt } from "../skills/format";
import { Effect } from "effect";
import { disclosurePolicy, enoughHelpGuidance, soulCustomization, agentRules, systemPrompt, hasSkillManage, hasSkillTools } from "./prompt";

export const MemoryGuidance =
  "You have persistent memory across sessions. Save durable facts using the memory tool: user preferences, environment details, tool quirks, and stable conventions. " +
  "Memory is injected into every turn, so keep it compact and focused on facts that will still matter later.\n" +
  "Prioritize what reduces future user steering — the most valuable memory is one that prevents the user from having to correct or remind you again. " +
  "User preferences and recurring corrections matter more than procedural task details.\n" +
  "Do NOT save task progress, session outcomes, completed-work logs, or temporary TODO state to memory; past session transcripts hold those. " +
  "Specifically: do not record PR numbers, issue numbers, commit SHAs, 'fixed bug X', 'submitted PR Y', 'Phase N done', file counts, or any artifact that will be stale in 7 days. If a fact will be stale in a week, it does not belong in memory. " +
  "If you've discovered a new way to do something, solved a problem that could be necessary later, save it as a skill with the skill tool.\n" +
  "Write memories as declarative facts, not instructions to yourself. " +
  "'User prefers concise responses' ✓ — 'Always respond concisely' ✗. " +
  "'Project uses pytest with xdist' ✓ — 'Run tests with pytest -n 4' ✗. " +
  "Imperative phrasing gets re-read as a directive in later sessions and can cause repeated work or override the user's current request. Procedures and workflows belong in skills, not memory.\n" +
  "CRITICAL: To save anything, you MUST call the memory tool in the same turn. " +
  "Never tell the user you will remember something, or that you have remembered it, unless the memory tool returned success in this turn (or a staged pending write).\n" +
  "PROFILE CORRECTIONS (mandatory): USER PROFILE in your prompt is a frozen session snapshot — it can be wrong or ambiguous. When the user corrects how you addressed them, how you interpreted profile text, or any fact you stated from memory/profile, call memory(action=replace, target=user, ...) in the same turn before finishing your reply. Apologizing alone leaves the wrong entry on disk and the mistake repeats next session. Spelling notes like 'lowercase h' mean write the full name that way (e.g. 'haider') — not a nickname from the initial letter.";

export const mcpFilterGuidance = `## MCP result hygiene

Never return an unbounded MCP query directly into the conversation. If a query may return more than 50 rows or 8KB, first write and run a short filtering script. The script must call the MCP server through bash with:

  enough mcp call <server.tool> '<json>'

Filter, aggregate, sample, or paginate in code and print only compact summary JSON. Read that summary into context, not the raw rows.

Bad: call an MCP database/search tool and let thousands of rows enter model context.
Good: bash runs a Python, shell, or Go script which calls enough mcp call, keeps only relevant rows/counts/samples, then prints a small JSON object.

For dynamic workflows, pre-fetch data in the orchestration script with sdk.runBash or sdk.fetchJSON, derive structured maps/clusters there, and pass only the relevant slice to each subagent.`;

const contextFileMaxChars = 24000;

export interface SystemPromptInputs {
  WorkDir: string;
  Cfg: runtime;
  ToolNames: string[];
  Store: any | null; // memory.Store | null
  SessionID: string;
  Now?: Date;
  PreloadedSkillsPrompt?: string;
}

export function BuildSessionSystemPrompt(inVal: SystemPromptInputs): string {
  const stable = buildStableTier(inVal);
  const context = buildContextTier(inVal);
  const volatile = buildVolatileTier(inVal);

  const parts: string[] = [];
  for (const p of [stable, context, volatile]) {
    const trimmed = p.trim();
    if (trimmed !== "") {
      parts.push(trimmed);
    }
  }
  return parts.join("\n\n");
}

function buildStableTier(inVal: SystemPromptInputs): string {
  const parts: string[] = [];

  const soul = LoadSoul();
  if (soul !== "") {
    parts.push(soul, disclosurePolicy, enoughHelpGuidance, soulCustomization, agentRules);
  } else {
    parts.push(systemPrompt);
  }

  let memoryToolEnabled = false;
  for (const t of inVal.ToolNames) {
    if (t === ToolName) {
      memoryToolEnabled = true;
      break;
    }
  }
  if (memoryToolEnabled) {
    parts.push(MemoryGuidance);
  }

  if (inVal.Cfg.skills.enabled) {
    if (hasSkillManage(inVal.ToolNames)) {
      parts.push(GuidanceBlock);
    }
    if (hasSkillTools(inVal.ToolNames)) {
      const idx = BuildIndexPrompt(inVal.WorkDir, inVal.Cfg, inVal.ToolNames);
      if (idx.trim() !== "") {
        parts.push(idx);
      }
    } else {
      try {
        const [sks] = Effect.runSync(DiscoverAllSkills(inVal.WorkDir, inVal.Cfg));
        if (sks.length > 0) {
          parts.push(FormatSkillsForPrompt(sks).trim());
        }
      } catch {}
    }
  }

  if (inVal.PreloadedSkillsPrompt && inVal.PreloadedSkillsPrompt !== "") {
    parts.push(inVal.PreloadedSkillsPrompt);
  }
  parts.push(mcpFilterGuidance);

  return parts.join("\n\n");
}

function buildContextTier(inVal: SystemPromptInputs): string {
  if (inVal.WorkDir === "") {
    return "";
  }
  for (const name of ["AGENTS.md", "agents.md"]) {
    const filePath = path.join(inVal.WorkDir, name);
    let data = "";
    try {
      if (fs.existsSync(filePath)) {
        data = fs.readFileSync(filePath, "utf8");
      } else {
        continue;
      }
    } catch {
      continue;
    }
    const content = data.trim();
    if (content === "") {
      continue;
    }
    const ids = ContextThreatIDs(content);
    if (ids.length > 0) {
      return `## ${name}\n\n[BLOCKED: ${name} contained threat pattern(s): ${ids.join(", ")}. Its content was removed from the system prompt.]`;
    }
    return truncateContextFile(`## ${name}\n\n${content}`, "AGENTS.md");
  }
  return "";
}

function buildVolatileTier(inVal: SystemPromptInputs): string {
  const parts: string[] = [];

  if (inVal.Store !== null) {
    if (inVal.Cfg.memory.memory_enabled) {
      const block = inVal.Store.formatForSystemPrompt(TargetMemory);
      if (block !== "") {
        parts.push(block);
      }
    }
    if (inVal.Cfg.memory.user_profile_enabled) {
      const block = inVal.Store.formatForSystemPrompt(TargetUser);
      if (block !== "") {
        parts.push(block);
      }
    }
  }

  const now = inVal.Now || new Date();
  // Day granularity only - using long date format
  const options: Intl.DateTimeFormatOptions = { weekday: "long", year: "numeric", month: "long", day: "numeric" };
  const formatter = new Intl.DateTimeFormat("en-US", options);
  let line = "Conversation started: " + formatter.format(now);
  if (inVal.WorkDir !== "") {
    line += "\nWorking directory: " + inVal.WorkDir;
  }
  if (inVal.SessionID !== "") {
    line += "\nSession ID: " + inVal.SessionID;
  }
  parts.push(line);

  return parts.join("\n\n");
}

function truncateContextFile(content: string, filename: string): string {
  if (content.length <= contextFileMaxChars) {
    return content;
  }
  const headChars = Math.floor((contextFileMaxChars * 7) / 10);
  const tailChars = Math.floor((contextFileMaxChars * 2) / 10);
  const marker = `\n\n[...truncated ${filename}: kept ${headChars}+${tailChars} of ${content.length} chars. Use file tools to read the full file.]\n\n`;
  return content.slice(0, headChars) + marker + content.slice(content.length - tailChars);
}

/*
PORT STATUS
source path: backend/agent/system_prompt.go
source lines: 233
draft lines: 200
confidence: high
status: phase_b_compile
*/
