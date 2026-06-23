// PORT: backend/memory/scan.go

import { SkillGuardThreatPatterns } from "../skills/guard_patterns";
import { InvisibleChars } from "../skills/guard";

// Threat scanning for content that gets injected into the system prompt
// (SOUL.md, AGENTS.md context files, MEMORY.md / USER.md entries).
//
// Patterns are shared with the skill guard (backend/skills/guard_patterns.go)
// — the single source of truth. Two scopes, mirroring Hermes:
//
//   - "strict" (memory entries): critical + high severity across ALL
//     categories, plus invisible unicode. Memory entries are user-curated
//     and enter the system prompt as a FROZEN snapshot, so a poisoned entry
//     persists for the entire session and across sessions until removed.
//   - "context" (SOUL.md, AGENTS.md): injection-category patterns plus
//     invisible unicode. Context files legitimately mention shell commands,
//     credentials directories, installs, etc. — only prompt-injection
//     content is blocked.
//
// Medium/low findings (e.g. unpinned installs) are never blocked:
// declarative facts like "project installs deps with pip install -r
// requirements.txt" are legitimate memory content.

export enum ScanScope {
  // ScopeStrict blocks critical/high findings in any category.
  ScopeStrict = 0,
  // ScopeContext blocks injection-category findings only.
  ScopeContext = 1,
}

// threatPatternIDs returns the IDs of all blocking threat patterns matched in
// content for the given scope, deduplicated. Empty when content is clean.
export function threatPatternIDs(content: string, scope: ScanScope): string[] {
  const ids: string[] = [];
  const seen = new Set<string>();
  const lines = content.split("\n");

  for (const p of SkillGuardThreatPatterns) {
    switch (scope) {
      case ScanScope.ScopeStrict:
        if (p.Severity !== "critical" && p.Severity !== "high") {
          continue;
        }
        break;
      case ScanScope.ScopeContext:
        if (p.Category !== "injection") {
          continue;
        }
        if (p.Severity !== "critical" && p.Severity !== "high") {
          continue;
        }
        break;
    }
    if (seen.has(p.PatternID)) {
      continue;
    }
    for (const line of lines) {
      if (p.Regex.test(line)) {
        seen.add(p.PatternID);
        ids.push(p.PatternID);
        break;
      }
    }
  }

  for (const char of InvisibleChars) {
    if (content.includes(char)) {
      if (!seen.has("invisible_unicode")) {
        seen.add("invisible_unicode");
        ids.push("invisible_unicode");
      }
      break;
    }
  }

  return ids;
}

// FirstThreatMessage scans memory content (strict scope) and returns an error
// message naming the matched pattern(s), or "" when clean.
export function FirstThreatMessage(content: string): string {
  const ids = threatPatternIDs(content, ScanScope.ScopeStrict);
  if (ids.length === 0) {
    return "";
  }
  return `Blocked: content matched threat pattern(s): ${ids.join(", ")}. Memory content is injected into the system prompt, so injection/exfiltration patterns are rejected. Rephrase the entry as a plain declarative fact.`;
}

// ContextThreatIDs scans context-file content (SOUL.md, AGENTS.md) for prompt
// injection. Returns matched pattern IDs, empty when clean.
export function ContextThreatIDs(content: string): string[] {
  return threatPatternIDs(content, ScanScope.ScopeContext);
}

/*
PORT STATUS
source path: backend/memory/scan.go
source lines: 103
draft lines: 98
confidence: high
status: phase_b_compile
*/
