// PORT: backend/skills/bundle.go

import { Effect } from "effect";
import fs from "node:fs/promises";
import fsSync from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { SkillsDir } from "./paths";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

export const agentReferenceSkillPath = path.join(
  __dirname,
  "bundled",
  "autonomous-ai-agents",
  "hollow-agent",
  "SKILL.md"
);

let cachedSkillBytes: Buffer | null = null;

export function getAgentReferenceSkillBytes(): Buffer {
  if (cachedSkillBytes === null) {
    try {
      cachedSkillBytes = fsSync.readFileSync(agentReferenceSkillPath);
    } catch {
      cachedSkillBytes = Buffer.from("");
    }
  }
  return cachedSkillBytes;
}

// ExtractHollowSkillIfMissing ensures the canonical hollow-agent reference skill
// exists under ~/.hollow/skills/ (SyncSkills is the primary path; this is a
// lightweight fallback for first-run before sync completes).
export function ExtractHollowSkillIfMissing(): Effect.Effect<void, Error> {
  const dir = path.join(SkillsDir(), "autonomous-ai-agents", "hollow-agent");
  const target = path.join(dir, "SKILL.md");

  return Effect.tryPromise({
    try: async () => {
      try {
        await fs.stat(target);
        return;
      } catch {}

      await fs.mkdir(dir, { recursive: true, mode: 0o700 });
      const bytes = getAgentReferenceSkillBytes();
      await fs.writeFile(target, bytes, { mode: 0o644 });
    },
    catch: (cause) => (cause instanceof Error ? cause : new Error(String(cause))),
  });
}

/*
PORT STATUS
source path: backend/skills/bundle.go
source lines: 29
draft lines: 57
confidence: high
status: phase_b_compile
*/
