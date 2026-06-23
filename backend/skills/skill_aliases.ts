// PORT: backend/skills/skill_aliases.go

// ResolveSkillLookupName normalizes legacy skill names from older Hollow builds
// or ported Hermes docs to the canonical bundled reference skill.
export function ResolveSkillLookupName(name: string): string {
  switch (name.trim().toLowerCase()) {
    case "hollow":
    case "hollow-agent":
      return "hollow-agent";
    default:
      return name.trim();
  }
}

/*
PORT STATUS
source path: backend/skills/skill_aliases.go
source lines: 15
draft lines: 15
confidence: high
status: phase_b_compile
*/
