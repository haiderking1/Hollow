// PORT: backend/skills/paths.go

import path from "node:path";
import { home_dir } from "../hollowhome/home";

export function HomeDir(): string {
  return home_dir();
}

export function SkillsDir(): string {
  return path.join(home_dir(), "skills");
}

export function LegacyAgentSkillsDir(): string {
  return path.join(home_dir(), "agent", "skills");
}

export function SnapshotPath(): string {
  return path.join(home_dir(), ".skills_prompt_snapshot.json");
}

export function SkillBundlesDir(): string {
  return path.join(home_dir(), "skill-bundles");
}

export function UsagePath(): string {
  return path.join(home_dir(), "skills", ".usage.json");
}

export function ArchiveDir(): string {
  return path.join(home_dir(), "skills", ".archive");
}

/*
PORT STATUS
source path: backend/skills/paths.go
source lines: 36
draft lines: 32
confidence: high
status: phase_b_compile
*/
