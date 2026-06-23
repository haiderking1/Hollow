// PORT: backend/skills/slash.go

import { Effect } from "effect";
import fs from "node:fs";
import path from "node:path";
import { type runtime } from "../config/config";
import { type SkillSnapshotEntry, type SkillsPromptSnapshot } from "./types";
import { PreprocessSkillContent } from "./preprocessing";
import { SkillsDir, SnapshotPath } from "./paths";
import { SearchLocations } from "./locations";
import { DiscoverAllSkills } from "./discovery";
import { executeSkillViewInternal } from "./tool_view";
import { buildFullManifest, readAllCategoryDescriptions, writeSkillsSnapshot } from "./prompt_index";
import { BumpUse } from "./usage";

const skillInvalidChars = /[^a-z0-9-]/g;
const skillMultiHyphen = /-{2,}/g;

export function SkillNameToSlashSlug(name: string): string {
  let cmd = name.toLowerCase();
  cmd = cmd.replaceAll(" ", "-");
  cmd = cmd.replaceAll("_", "-");
  cmd = cmd.replace(skillInvalidChars, "");
  cmd = cmd.replace(skillMultiHyphen, "-");
  // Trim leading/trailing hyphens
  cmd = cmd.replace(/^-+|-+$/g, "");
  return cmd;
}

function walkSupporting(skillDir: string): string[] {
  const supporting: string[] = [];
  for (const subdir of ["references", "templates", "scripts", "assets"]) {
    const subdirPath = path.join(skillDir, subdir);
    try {
      if (fs.existsSync(subdirPath) && fs.statSync(subdirPath).isDirectory()) {
        const walk = (dir: string) => {
          const entries = fs.readdirSync(dir);
          for (const entry of entries) {
            const full = path.join(dir, entry);
            const stat = fs.statSync(full);
            if (stat.isDirectory()) {
              walk(full);
            } else {
              let rel = path.relative(skillDir, full);
              supporting.push(rel.split(path.sep).join("/"));
            }
          }
        };
        walk(subdirPath);
      }
    } catch {}
  }
  return supporting;
}

export function BuildSkillInvocationMessage(
  loadedSkill: Record<string, any>,
  skillDir: string,
  userInstruction: string,
  sessionId: string,
  cfg: runtime
): string {
  let name = typeof loadedSkill["name"] === "string" ? loadedSkill["name"] : "";
  if (name === "") {
    name = "skill";
  }

  const activationNote = `[IMPORTANT: The user has invoked the "${name}" skill. Follow the skill instructions below as your primary guidance for this turn.]`;
  let content = typeof loadedSkill["content"] === "string" ? loadedSkill["content"] : "";

  // Preprocess content
  // Since PreprocessSkillContent returns an Effect, we run it synchronously using Effect.runSync
  // because BuildSkillInvocationMessage has a synchronous signature in Go.
  // Wait! In Wave 1 preprocessing.ts, PreprocessSkillContent returned: Effect.Effect<string, never>.
  // Effect.runSync is safe to run on Effect<string, never> as it has no error type.
  content = Effect.runSync(
    PreprocessSkillContent(
      content,
      skillDir,
      sessionId,
      cfg.skills?.inline_shell || false,
      cfg.skills?.inline_shell_timeout || 10
    )
  );

  const parts: string[] = [];
  parts.push(activationNote, "", content.trim());

  if (skillDir !== "") {
    parts.push(
      "",
      `[Skill directory: ${skillDir}]`,
      "Resolve any relative paths in this skill (e.g. `scripts/foo.js`, `templates/config.yaml`) against that directory, then run them with the terminal tool using the absolute path."
    );
  }

  let supporting: string[] = [];
  const linkedFilesVal = loadedSkill["linked_files"];
  if (linkedFilesVal && typeof linkedFilesVal === "object" && !Array.isArray(linkedFilesVal)) {
    for (const entries of Object.values(linkedFilesVal)) {
      if (Array.isArray(entries)) {
        for (const entry of entries) {
          if (typeof entry === "string") {
            supporting.push(entry);
          }
        }
      }
    }
  }

  if (supporting.length === 0 && skillDir !== "") {
    supporting = walkSupporting(skillDir);
  }

  if (supporting.length > 0 && skillDir !== "") {
    let skillViewTarget = path.basename(skillDir);
    const skillsRoot = SkillsDir();
    try {
      const rel = path.relative(skillsRoot, skillDir);
      skillViewTarget = rel.split(path.sep).join("/");
    } catch {}

    parts.push("", "[This skill has supporting files:]");
    for (const sf of supporting) {
      parts.push(`- ${sf}  ->  ${path.join(skillDir, sf)}`);
    }
    parts.push(
      `\nLoad any of these with skill_view(name="${skillViewTarget}", file_path="<path>"), or run scripts directly by absolute path.`
    );
  }

  if (userInstruction.trim() !== "") {
    parts.push(
      "",
      `The user has provided the following instruction alongside the skill invocation: ${userInstruction.trim()}`
    );
  }

  return parts.join("\n");
}

export function ExpandSkillSlashCommand(
  skillName: string,
  userArgs: string,
  workDir: string,
  cfg: runtime,
  sessionId: string
): Effect.Effect<[string, string], Error> {
  return Effect.try({
    try: () => {
      const viewRes = executeSkillViewInternal(skillName, "", workDir, cfg, sessionId, false);
      if (!viewRes.Success) {
        throw new Error(viewRes.Error || "Failed to view skill");
      }

      const loadedSkill = {
        name: viewRes.Name,
        content: viewRes.RawContent,
        linked_files: viewRes.LinkedFiles,
      };

      const message = BuildSkillInvocationMessage(loadedSkill, viewRes.SkillDir || "", userArgs, sessionId, cfg);
      const cleanBody = Effect.runSync(
        PreprocessSkillContent(
          viewRes.RawContent || "",
          viewRes.SkillDir || "",
          sessionId,
          cfg.skills?.inline_shell || false,
          cfg.skills?.inline_shell_timeout || 10
        )
      );
      return [message, cleanBody] as [string, string];
    },
    catch: (cause) => (cause instanceof Error ? cause : new Error(String(cause))),
  });
}

export interface ReloadDiff {
  Added: Record<string, string>[];
  Removed: Record<string, string>[];
  Unchanged: string[];
  Total: number;
  Commands: number;
}

export function ReloadSkills(workDir: string, cfg: runtime): Effect.Effect<ReloadDiff, Error> {
  const snap = SnapshotPath();
  const before: Record<string, string> = {};
  try {
    if (fs.existsSync(snap)) {
      const dataBytes = fs.readFileSync(snap);
      const snapshot = JSON.parse(dataBytes.toString()) as SkillsPromptSnapshot;
      if (snapshot && snapshot.skills) {
        for (const entry of snapshot.skills) {
          before[entry.skill_name] = entry.description;
        }
      }
    }
  } catch {}

  return DiscoverAllSkills(workDir, cfg).pipe(
    Effect.flatMap(([discovered]) => {
      const after: Record<string, string> = {};
      const afterEntries: SkillSnapshotEntry[] = [];

      for (const sk of discovered) {
        after[sk.Name] = sk.Description;
        afterEntries.push({
          skill_name: sk.Name,
          category: sk.Category,
          frontmatter_name: sk.Name,
          description: sk.Description,
          platforms: sk.Platforms || [],
          conditions: sk.Conditions,
          disable_model_invocation: sk.DisableModelInvocation,
          environments: sk.Environments || [],
        });
      }

      const dirs = SearchLocations(workDir, cfg, "");
      const manifest = buildFullManifest(dirs);
      const categoryDescs = readAllCategoryDescriptions(dirs);
      writeSkillsSnapshot(manifest, afterEntries, categoryDescs);

      const added: Record<string, string>[] = [];
      const removed: Record<string, string>[] = [];
      const unchanged: string[] = [];

      for (const [name, desc] of Object.entries(after)) {
        if (before[name] === undefined) {
          added.push({ name, description: desc });
        } else {
          unchanged.push(name);
        }
      }

      for (const [name, desc] of Object.entries(before)) {
        if (after[name] === undefined) {
          removed.push({ name, description: desc });
        }
      }

      unchanged.sort();
      added.sort((a, b) => a.name.localeCompare(b.name));
      removed.sort((a, b) => a.name.localeCompare(b.name));

      return Effect.succeed({
        Added: added,
        Removed: removed,
        Unchanged: unchanged,
        Total: Object.keys(after).length,
        Commands: Object.keys(after).length * 2,
      });
    })
  );
}

export function BuildPreloadedSkillsPrompt(
  skillIdentifiers: string[],
  workDir: string,
  sessionId: string,
  cfg: runtime
): Effect.Effect<[string, string[], string[]], Error> {
  return Effect.try({
    try: () => {
      const promptParts: string[] = [];
      const loadedNames: string[] = [];
      const missing: string[] = [];
      const seen = new Set<string>();

      for (const rawIdentifier of skillIdentifiers) {
        const identifier = rawIdentifier.trim();
        if (identifier === "" || seen.has(identifier)) {
          continue;
        }
        seen.add(identifier);

        const res = executeSkillViewInternal(identifier, "", workDir, cfg, sessionId, false);
        if (!res.Success) {
          missing.push(identifier);
          continue;
        }

        BumpUse(res.Name || identifier);

        const activationNote = `[IMPORTANT: The user launched this CLI session with the "${res.Name || identifier}" skill preloaded. Treat its instructions as active guidance for the duration of this session unless the user overrides them.]`;

        // Preprocess content
        const content = Effect.runSync(
          PreprocessSkillContent(
            res.RawContent || "",
            res.SkillDir || "",
            sessionId,
            cfg.skills?.inline_shell || false,
            cfg.skills?.inline_shell_timeout || 10
          )
        );

        const parts: string[] = [];
        parts.push(activationNote, "", content.trim());

        if (res.SkillDir && res.SkillDir !== "") {
          parts.push(
            "",
            `[Skill directory: ${res.SkillDir}]`,
            "Resolve any relative paths in this skill (e.g. `scripts/foo.js`, `templates/config.yaml`) against that directory, then run them with the terminal tool using the absolute path."
          );
        }

        if (res.SetupNeeded && res.Setup && res.Setup.Help && res.Setup.Help !== "") {
          parts.push("", `[Skill setup note: ${res.Setup.Help}]`);
        }

        let supporting: string[] = [];
        if (res.LinkedFiles) {
          for (const entries of Object.values(res.LinkedFiles)) {
            supporting.push(...entries);
          }
        }
        if (supporting.length === 0 && res.SkillDir && res.SkillDir !== "") {
          supporting = walkSupporting(res.SkillDir);
        }

        if (supporting.length > 0 && res.SkillDir && res.SkillDir !== "") {
          let skillViewTarget = path.basename(res.SkillDir);
          const skillsRoot = SkillsDir();
          try {
            const rel = path.relative(skillsRoot, res.SkillDir);
            skillViewTarget = rel.split(path.sep).join("/");
          } catch {}

          parts.push("", "[This skill has supporting files:]");
          for (const sf of supporting) {
            parts.push(`- ${sf}  ->  ${path.join(res.SkillDir, sf)}`);
          }
          parts.push(
            `\nLoad any of these with skill_view(name="${skillViewTarget}", file_path="<path>"), or run scripts directly by absolute path.`
          );
        }

        promptParts.push(parts.join("\n"));
        loadedNames.push(res.Name || identifier);
      }

      return [promptParts.join("\n\n"), loadedNames, missing] as [string, string[], string[]];
    },
    catch: (cause) => (cause instanceof Error ? cause : new Error(String(cause))),
  });
}

/*
PORT STATUS
source path: backend/skills/slash.go
source lines: 276
draft lines: 341
confidence: high
status: phase_b_compile
*/
