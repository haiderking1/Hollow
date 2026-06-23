// PORT: backend/skills/locations.go

import fs from "node:fs";
import path from "node:path";
import os from "node:os";
import { type runtime } from "../config/config";
import { HomeDir } from "./paths";
import { ExcludedSkillDirs } from "./constants";

export interface SearchDir {
  Path: string;
  Source: string; // "project" | "user" | "path" | "flame"
  IncludeRootMD: boolean; // false for .agents/skills only
}

function isBlockedSkillRoot(filePath: string): boolean {
  const clean = path.normalize(filePath);
  return clean.includes(path.join(".cursor", "skills-cursor"));
}

function hasExcludedComponent(filePath: string): boolean {
  const clean = path.normalize(filePath);
  const parts = clean.split(path.sep);
  for (const part of parts) {
    if (ExcludedSkillDirs[part]) {
      return true;
    }
  }
  return false;
}

// SearchLocations returns every directory to scan for skills, in STRICT
// precedence order (first match wins on name collision).
export function SearchLocations(workDir: string, cfg: runtime, agentDirOverride: string): SearchDir[] {
  const dirs: SearchDir[] = [];
  const seen = new Set<string>();

  const addDir = (dirPath: string, source: string, includeRootMD: boolean) => {
    let abs: string;
    try {
      abs = path.resolve(dirPath);
    } catch {
      abs = path.normalize(dirPath);
    }
    if (seen.has(abs)) {
      return;
    }
    if (isBlockedSkillRoot(abs) || hasExcludedComponent(abs)) {
      return;
    }
    seen.add(abs);
    dirs.push({
      Path: abs,
      Source: source,
      IncludeRootMD: includeRootMD,
    });
  };

  // 1 & 2. Project: cwd→gitRoot (or FS root if no git repo)
  let current = workDir;
  let gitRoot = "";
  while (true) {
    if (fs.existsSync(path.join(current, ".git"))) {
      gitRoot = current;
      break;
    }
    const parent = path.dirname(current);
    if (parent === current) {
      break;
    }
    current = parent;
  }

  current = workDir;
  while (true) {
    // .hollow/skills
    addDir(path.join(current, ".hollow", "skills"), "project", true);
    // .agents/skills
    addDir(path.join(current, ".agents", "skills"), "project", false);

    if (gitRoot !== "" && current === gitRoot) {
      break;
    }
    const parent = path.dirname(current);
    if (parent === current) {
      break;
    }
    current = parent;
  }

  // cwd .cursor/skills
  addDir(path.join(workDir, ".cursor", "skills"), "project", true);

  // 3. Global user: ~/.hollow/skills, ~/.hollow/agent/skills, ~/.agents/skills, ~/.cursor/skills
  const home = HomeDir();
  addDir(path.join(home, "skills"), "user", true);
  if (agentDirOverride !== "") {
    addDir(path.join(agentDirOverride, "skills"), "user", true);
  } else {
    addDir(path.join(home, "agent", "skills"), "user", true);
  }

  const userHome = os.homedir();
  if (userHome) {
    addDir(path.join(userHome, ".agents", "skills"), "user", false);
    addDir(path.join(userHome, ".cursor", "skills"), "user", true);
  }

  // 4. Optional read-only: ~/.flame/skills (if exists)
  if (userHome) {
    const flameSkills = path.join(userHome, ".flame", "skills");
    try {
      const fi = fs.statSync(flameSkills);
      if (fi.isDirectory()) {
        addDir(flameSkills, "flame", true);
      }
    } catch {}
  }

  // 5. Config explicit paths (cfg.skills.paths, minus ! exclusions)
  const pathsList = cfg.skills?.paths || [];
  for (const p of pathsList) {
    if (p.startsWith("!")) {
      continue;
    }
    let absPath = p;
    if (!path.isAbsolute(absPath)) {
      if (absPath.startsWith("~") && userHome) {
        absPath = path.join(userHome, absPath.substring(1));
      } else {
        absPath = path.join(workDir, absPath);
      }
    }
    addDir(absPath, "path", true);
  }

  // 6. External directories from config (Hermes semantics)
  for (const extDir of getExternalSkillsDirs(cfg)) {
    addDir(extDir, "user", true);
  }

  return dirs;
}

function getExternalSkillsDirs(cfg: runtime): string[] {
  const userHome = os.homedir();
  const home = HomeDir();
  let localSkills: string;
  try {
    localSkills = path.resolve(path.join(home, "skills"));
  } catch {
    localSkills = path.normalize(path.join(home, "skills"));
  }

  const out: string[] = [];
  const seen = new Set<string>();
  const extDirs = cfg.skills?.external_dirs || [];

  for (let entry of extDirs) {
    entry = entry.trim();
    if (entry === "") {
      continue;
    }

    // Expand env variables
    let expanded = entry.replace(/\$(\w+)|\${(\w+)}/g, (_, m1, m2) => process.env[m1 || m2] || "");

    // Expand ~
    if (expanded.startsWith("~") && userHome) {
      expanded = path.join(userHome, expanded.substring(1));
    }

    // Resolve relative paths against HOLLOW_HOME (home)
    let abs = expanded;
    if (!path.isAbsolute(abs)) {
      abs = path.join(home, abs);
    }
    try {
      abs = path.resolve(abs);
    } catch {
      abs = path.normalize(abs);
    }

    if (abs === localSkills) {
      continue;
    }
    if (seen.has(abs)) {
      continue;
    }

    try {
      const fi = fs.statSync(abs);
      if (fi.isDirectory()) {
        seen.add(abs);
        out.push(abs);
      }
    } catch {}
  }
  return out;
}

/*
PORT STATUS
source path: backend/skills/locations.go
source lines: 187
draft lines: 185
confidence: high
status: phase_b_compile
*/
