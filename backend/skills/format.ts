// PORT: backend/skills/format.go

import { type Skill } from "./types";

function escapeXml(str: string): string {
  return str
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&apos;");
}

export function FormatSkillsForPrompt(skills: Skill[]): string {
  const visibleSkills: Skill[] = [];
  for (const s of skills) {
    if (!s.DisableModelInvocation) {
      visibleSkills.push(s);
    }
  }

  if (visibleSkills.length === 0) {
    return "";
  }

  let sb = "";
  sb += "\n\nThe following skills provide specialized instructions for specific tasks.\n";
  sb += "Use the read tool to load a skill's file when the task matches its description.\n";
  sb += "When a skill file references a relative path, resolve it against the skill directory (parent of SKILL.md / dirname of the path) and use that absolute path in tool commands.\n\n";
  sb += "<available_skills>\n";

  for (const skill of visibleSkills) {
    sb += "  <skill>\n";
    sb += "    <name>" + escapeXml(skill.Name) + "</name>\n";
    sb += "    <description>" + escapeXml(skill.Description) + "</description>\n";
    sb += "    <location>" + escapeXml(skill.FilePath) + "</location>\n";
    sb += "  </skill>\n";
  }

  sb += "</available_skills>";
  return sb;
}

/*
PORT STATUS
source path: backend/skills/format.go
source lines: 47
draft lines: 44
confidence: high
status: phase_b_compile
*/
