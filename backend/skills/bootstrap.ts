// PORT: backend/skills/bootstrap.go

import { Effect } from "effect";
import fs from "node:fs";
import { HomeDir, SkillsDir } from "./paths";
import { ExtractHollowSkillIfMissing } from "./bundle";
import { SyncSkills } from "./sync";

// EnsureBootstrapped seeds the skills library on first launch, mirroring
// Hermes' idempotent sync_skills(quiet=True) on every chat/TUI entry.
// Failures are swallowed — skills enhance the agent but are not hard-required.
export function EnsureBootstrapped(): Effect.Effect<void, never> {
  const initDirs = Effect.try({
    try: () => {
      const home = HomeDir();
      if (!fs.existsSync(home)) {
        fs.mkdirSync(home, { recursive: true, mode: 0o700 });
      }
      const skillsDir = SkillsDir();
      if (!fs.existsSync(skillsDir)) {
        fs.mkdirSync(skillsDir, { recursive: true, mode: 0o700 });
      }
    },
    catch: (cause) => (cause instanceof Error ? cause : new Error(String(cause))),
  }).pipe(Effect.catchAll(() => Effect.void));

  return initDirs.pipe(
    Effect.flatMap(() =>
      ExtractHollowSkillIfMissing().pipe(
        Effect.flatMap(() => SyncSkills(true)),
        Effect.match({
          onFailure: () => undefined,
          onSuccess: () => undefined,
        })
      )
    )
  );
}

/*
PORT STATUS
source path: backend/skills/bootstrap.go
source lines: 19
draft lines: 41
confidence: high
status: phase_b_compile
*/
