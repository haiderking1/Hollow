// ResolveSkillLookupName normalizes legacy skill names from older Hollow builds
// or ported Hermes docs to the canonical bundled reference skill.
export function ResolveSkillLookupName(name: string): string {
  switch (name.trim().toLowerCase()) {
    case "hollow":
    case "hollow-agent":
    case "enough":
      return "hollow-agent";
    default:
      return name.trim();
  }
}

