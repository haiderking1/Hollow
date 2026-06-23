// PORT: backend/skills/frontmatter.go

import path from "node:path";
import { PlatformMap, PromptIndexDescriptionMax } from "./constants";
import { type SkillConditions, type SkillSnapshotEntry } from "./types";

function parseFrontmatterYAML(yamlStr: string): Record<string, any> {
  const result: Record<string, any> = {};
  const lines = yamlStr.split("\n");
  
  // A stack of { indent, obj, key } to track nesting
  const stack: { indent: number; obj: any; key?: string }[] = [{ indent: -1, obj: result }];

  for (const line of lines) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith("#")) {
      continue;
    }

    const indent = line.length - line.trimStart().length;

    // Pop from stack until the top element's indent is strictly less than the current line's indent
    while (stack.length > 1 && stack[stack.length - 1].indent >= indent) {
      stack.pop();
    }

    const current = stack[stack.length - 1];
    const parent = current.obj;

    // Check if the line is a list item starting with "- "
    if (trimmed.startsWith("- ")) {
      const valStr = trimmed.substring(2).trim();
      let value: any = valStr;
      
      // Parse value
      if ((valStr.startsWith('"') && valStr.endsWith('"')) || (valStr.startsWith("'") && valStr.endsWith("'"))) {
        value = valStr.slice(1, -1);
      } else if (valStr === "true") {
        value = true;
      } else if (valStr === "false") {
        value = false;
      } else if (!isNaN(Number(valStr)) && valStr !== "") {
        value = Number(valStr);
      }

      // If the parent is an array, push to it. If it's an object, we need to know what key it belongs to.
      if (Array.isArray(parent)) {
        parent.push(value);
      } else if (current.key) {
        if (!Array.isArray(parent[current.key])) {
          parent[current.key] = [];
        }
        parent[current.key].push(value);
      }
      continue;
    }

    const colonIdx = line.indexOf(":");
    if (colonIdx === -1) {
      continue;
    }

    const key = line.substring(0, colonIdx).trim();
    const valStr = line.substring(colonIdx + 1).trim();

    current.key = key;

    if (valStr === "") {
      // Nested object or list
      // Look at the next non-empty line to see if it starts with "- " to decide if it's an array
      let isArray = false;
      const currentIdx = lines.indexOf(line);
      for (let j = currentIdx + 1; j < lines.length; j++) {
        const nextTrimmed = lines[j].trim();
        if (nextTrimmed && !nextTrimmed.startsWith("#")) {
          if (nextTrimmed.startsWith("- ")) {
            isArray = true;
          }
          break;
        }
      }

      if (isArray) {
        const child: any[] = [];
        parent[key] = child;
        stack.push({ indent, obj: child });
      } else {
        const child = {};
        parent[key] = child;
        stack.push({ indent, obj: child });
      }
    } else {
      let value: any = valStr;
      if (valStr.startsWith("[") && valStr.endsWith("]")) {
        const listContent = valStr.slice(1, -1).trim();
        if (listContent === "") {
          value = [];
        } else {
          value = listContent.split(",").map((item) => {
            let s = item.trim();
            if ((s.startsWith('"') && s.endsWith('"')) || (s.startsWith("'") && s.endsWith("'"))) {
              s = s.slice(1, -1);
            }
            return s;
          });
        }
      } else {
        if (valStr === "true") value = true;
        else if (valStr === "false") value = false;
        else if (!isNaN(Number(valStr)) && valStr !== "") value = Number(valStr);
        else {
          if ((valStr.startsWith('"') && valStr.endsWith('"')) || (valStr.startsWith("'") && valStr.endsWith("'"))) {
            value = valStr.slice(1, -1);
          }
        }
      }
      parent[key] = value;
    }
  }

  return result;
}

export function ParseFrontmatter(content: string): [Record<string, any> | null, string] {
  const normalized = content.replaceAll("\r\n", "\n");
  if (!normalized.startsWith("---")) {
    return [null, content];
  }

  const parts = normalized.split("---");
  if (parts.length < 3) {
    return [null, content];
  }

  const fm = parseFrontmatterYAML(parts[1]);
  // The content body is everything after the second "---"
  // parts[0] is empty, parts[1] is frontmatter, parts[2...] is the body
  const body = parts.slice(2).join("---");
  return [fm, body];
}

export function SkillMatchesPlatform(fm: Record<string, any>): boolean {
  return skillMatchesPlatform(fm);
}

export function skillMatchesPlatform(fm: Record<string, any>): boolean {
  const platformsVal = fm["platforms"];
  if (platformsVal === undefined || platformsVal === null) {
    return true;
  }

  const list = toStringList(platformsVal);
  if (list.length === 0) {
    return true;
  }

  const current = process.platform === "win32" ? "windows" : process.platform;
  for (const platform of list) {
    const normalized = platform.trim().toLowerCase();
    let mapped = PlatformMap[normalized];
    if (!mapped) {
      mapped = normalized;
    }
    if (mapped === "win32") {
      mapped = "windows";
    }
    if (current.startsWith(mapped)) {
      return true;
    }
  }
  return false;
}

export function toStringList(val: any): string[] {
  if (val === undefined || val === null) {
    return [];
  }
  if (typeof val === "string") {
    if (val === "") {
      return [];
    }
    return [val];
  }
  if (Array.isArray(val)) {
    const out: string[] = [];
    for (const item of val) {
      if (typeof item === "string") {
        out.push(item);
      } else if (item !== undefined && item !== null) {
        out.push(String(item));
      }
    }
    return out;
  }
  return [];
}

export function extractSkillConditions(fm: Record<string, any>): SkillConditions {
  const cond: SkillConditions = {
    fallbackForToolsets: [],
    requiresToolsets: [],
    fallbackForTools: [],
    requiresTools: [],
  };

  const metaVal = fm["metadata"];
  if (!metaVal || typeof metaVal !== "object") {
    return cond;
  }

  const hermesVal = metaVal["hermes"];
  if (!hermesVal || typeof hermesVal !== "object") {
    return cond;
  }

  cond.fallbackForToolsets = toStringList(hermesVal["fallback_for_toolsets"]);
  cond.requiresToolsets = toStringList(hermesVal["requires_toolsets"]);
  cond.fallbackForTools = toStringList(hermesVal["fallback_for_tools"]);
  cond.requiresTools = toStringList(hermesVal["requires_tools"]);
  return cond;
}

export function extractSkillDescription(fm: Record<string, any>): string {
  const rawDesc = fm["description"];
  if (rawDesc === undefined || rawDesc === null) {
    return "";
  }
  const desc = String(rawDesc).trim().replace(/^['"]|['"]$/g, "").trim();
  if (desc.length > PromptIndexDescriptionMax) {
    return desc.substring(0, PromptIndexDescriptionMax - 3) + "...";
  }
  return desc;
}

export function extractSkillTags(fm: Record<string, any>): string[] {
  const metaVal = fm["metadata"];
  if (!metaVal || typeof metaVal !== "object") {
    return [];
  }
  const hermesVal = metaVal["hermes"];
  if (!hermesVal || typeof hermesVal !== "object") {
    return [];
  }
  return toStringList(hermesVal["tags"]);
}

export function extractRelatedSkills(fm: Record<string, any>): string[] {
  const metaVal = fm["metadata"];
  if (!metaVal || typeof metaVal !== "object") {
    return [];
  }
  const hermesVal = metaVal["hermes"];
  if (!hermesVal || typeof hermesVal !== "object") {
    return [];
  }
  return toStringList(hermesVal["related_skills"]);
}

export function normalizePlatforms(fm: Record<string, any>): string[] {
  const platformsVal = fm["platforms"];
  if (platformsVal === undefined || platformsVal === null) {
    return [];
  }
  const list = toStringList(platformsVal);
  const out: string[] = [];
  for (const p of list) {
    const trimmed = p.trim();
    if (trimmed !== "") {
      out.push(trimmed);
    }
  }
  return out;
}

export function skillShouldShow(
  conditions: SkillConditions,
  availableTools: Record<string, boolean> | null,
  availableToolsets: Record<string, boolean> | null
): boolean {
  if (availableTools === null && availableToolsets === null) {
    return true;
  }

  for (const ts of conditions.fallbackForToolsets) {
    if (availableToolsets !== null && availableToolsets[ts]) {
      return false;
    }
  }
  for (const t of conditions.fallbackForTools) {
    if (availableTools !== null && availableTools[t]) {
      return false;
    }
  }
  for (const ts of conditions.requiresToolsets) {
    if (availableToolsets === null || !availableToolsets[ts]) {
      return false;
    }
  }
  for (const t of conditions.requiresTools) {
    if (availableTools === null || !availableTools[t]) {
      return false;
    }
  }
  return true;
}

export function computeSkillCategory(skillFilePath: string, skillsRoot: string): string {
  try {
    let rel = path.relative(skillsRoot, skillFilePath);
    rel = rel.split(path.sep).join("/");
    const parts = rel.split("/");
    const cleaned: string[] = [];
    for (const p of parts) {
      if (p !== "") {
        cleaned.push(p);
      }
    }
    if (cleaned.length >= 2) {
      if (cleaned.length > 2) {
        return cleaned.slice(0, cleaned.length - 2).join("/");
      }
      return cleaned[0];
    }
    return "general";
  } catch {
    return "general";
  }
}

export function buildSnapshotEntry(
  skillFile: string,
  skillsDir: string,
  fm: Record<string, any>,
  description: string
): SkillSnapshotEntry {
  let skillName = "unknown";
  let category = "general";

  try {
    const rel = path.relative(skillsDir, skillFile);
    const relSlash = rel.split(path.sep).join("/");
    const parts = relSlash.split("/");
    const cleaned: string[] = [];
    for (const p of parts) {
      if (p !== "") {
        cleaned.push(p);
      }
    }
    if (cleaned.length >= 2) {
      skillName = cleaned[cleaned.length - 2];
      if (cleaned.length > 2) {
        category = cleaned.slice(0, cleaned.length - 2).join("/");
      } else {
        category = cleaned[0];
      }
    } else {
      category = "general";
      if (cleaned.length > 0) {
        skillName = cleaned[cleaned.length - 1].replace(/\.md$/, "");
      } else {
        skillName = "unknown";
      }
    }
  } catch {
    category = "general";
    skillName = "unknown";
  }

  const fmNameVal = fm["name"];
  let frontmatterName = typeof fmNameVal === "string" ? fmNameVal : "";
  if (frontmatterName === "") {
    frontmatterName = skillName;
  }

  const platforms = normalizePlatforms(fm);
  const envs = toStringList(fm["environments"]);

  return {
    skill_name: skillName,
    category: category,
    frontmatter_name: frontmatterName,
    description: description,
    platforms: platforms,
    conditions: extractSkillConditions(fm),
    disable_model_invocation: false,
    environments: envs,
  };
}

/*
PORT STATUS
source path: backend/skills/frontmatter.go
source lines: 304
draft lines: 367
confidence: high
status: phase_b_compile
*/
