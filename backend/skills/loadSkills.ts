// PORT: backend/skills/loadSkills.go

import path from "node:path";
import { type runtime } from "../config/config";
import { type Skill } from "./types";
import { type SearchDir, SearchLocations } from "./locations";
import { LoadSkillsFromDirs } from "./discovery";

export interface LoadSkillsOptions {
  Cwd: string;
  AgentDir: string;
  SkillPaths: string[];
  IncludeDefaults: boolean;
}

export interface LoadSkillsResult {
  Skills: Skill[];
  Diagnostics: string[];
}

export function LoadSkills(opts: LoadSkillsOptions): LoadSkillsResult {
  const cfg = {
    skills: {
      enabled: true,
      enable_skill_commands: true,
      paths: opts.SkillPaths,
      disabled: [],
      external_dirs: [],
      platform_disabled: {},
      guard_agent_created: false,
      write_approval: false,
      inline_shell: false,
      inline_shell_timeout: 10,
    },
  } as unknown as runtime;

  let dirs: SearchDir[] = [];
  if (opts.IncludeDefaults) {
    dirs = SearchLocations(opts.Cwd, cfg, opts.AgentDir);
  } else {
    const seen = new Set<string>();
    for (const p of opts.SkillPaths) {
      if (p.startsWith("!")) {
        continue;
      }
      let abs: string;
      try {
        abs = path.resolve(p);
      } catch {
        abs = path.normalize(p);
      }
      if (seen.has(abs)) {
        continue;
      }
      seen.add(abs);
      dirs.push({
        Path: abs,
        Source: "path",
        IncludeRootMD: true,
      });
    }
  }

  const [skillsList, diags] = LoadSkillsFromDirs(opts.Cwd, dirs, cfg);
  return {
    Skills: skillsList,
    Diagnostics: diags,
  };
}

/*
PORT STATUS
source path: backend/skills/loadSkills.go
source lines: 62
draft lines: 72
confidence: high
status: phase_b_compile
*/
