// PORT: backend/agent/coding_context.go

import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { type runtime } from "../config/config";

const projectMarkers = [
  "pyproject.toml", "setup.py", "setup.cfg", "requirements.txt",
  "package.json", "tsconfig.json", "deno.json",
  "Cargo.toml", "go.mod", "pom.xml", "build.gradle", "build.gradle.kts",
  "Gemfile", "composer.json", "mix.exs", "pubspec.yaml",
  "CMakeLists.txt", "Makefile", "Dockerfile",
  "AGENTS.md", "CLAUDE.md", ".cursorrules",
];

const interactiveCodingPlatforms: Record<string, boolean> = {
  cli: true,
  tui: true,
  acp: true,
  desktop: true,
  "": true,
};

export const NonCodingCategories: Record<string, boolean> = {
  apple: true,
  communication: true,
  cooking: true,
  creative: true,
  email: true,
  finance: true,
  gaming: true,
  gifs: true,
  health: true,
  media: true,
  music: true,
  "note-taking": true,
  productivity: true,
  shopping: true,
  "smart-home": true,
  "social-media": true,
  travel: true,
  yuanbao: true,
};

function isGitRoot(dir: string): boolean {
  const gitDir = path.join(dir, ".git");
  try {
    const fi = fs.statSync(gitDir);
    return fi.isDirectory();
  } catch {
    return false;
  }
}

function findGitRoot(cwd: string): string {
  let curr = path.resolve(cwd);
  while (true) {
    if (isGitRoot(curr)) {
      return curr;
    }
    const parent = path.dirname(curr);
    if (parent === curr) {
      break;
    }
    curr = parent;
  }
  return "";
}

function findMarkerRoot(cwd: string): string {
  let curr = path.resolve(cwd);
  const home = os.homedir();
  for (let depth = 0; depth <= 6; depth++) {
    if (curr === home) {
      break;
    }
    for (const marker of projectMarkers) {
      try {
        fs.statSync(path.join(curr, marker));
        return curr;
      } catch {}
    }
    const parent = path.dirname(curr);
    if (parent === curr) {
      break;
    }
    curr = parent;
  }
  return "";
}

function resolvePlatform(): string {
  if (process.env.HOLLOW_PLATFORM) return process.env.HOLLOW_PLATFORM;
  if (process.env.HERMES_PLATFORM) return process.env.HERMES_PLATFORM;
  if (process.env.HOLLOW_SESSION_PLATFORM) return process.env.HOLLOW_SESSION_PLATFORM;
  if (process.env.HERMES_SESSION_PLATFORM) return process.env.HERMES_SESSION_PLATFORM;
  return "cli";
}

// DetectIsCoding reports whether the agent is currently in a coding context.
export function DetectIsCoding(workDir: string, cfg: runtime): boolean {
  const codingContext = cfg.agent?.coding_context || "auto";
  const mode = codingContext.trim().toLowerCase();

  if (mode === "off" || mode === "false" || mode === "never") {
    return false;
  }
  if (mode === "on" || mode === "true" || mode === "always") {
    return true;
  }

  const platform = resolvePlatform();
  if (!interactiveCodingPlatforms[platform.toLowerCase()]) {
    return false;
  }

  const home = os.homedir();
  let gitRoot = findGitRoot(workDir);
  if (gitRoot !== "" && (gitRoot === home || gitRoot === path.resolve(os.tmpdir()))) {
    gitRoot = "";
  }

  if (gitRoot !== "" || findMarkerRoot(workDir) !== "") {
    return true;
  }

  return false;
}

// ShouldDemoteCategory reports whether a skill category should be demoted to names-only format.
export function ShouldDemoteCategory(cat: string, isCoding: boolean, configMode: string): boolean {
  if (!isCoding || configMode !== "focus") {
    return false;
  }
  const parts = cat.split("/");
  return !!NonCodingCategories[parts[0]];
}

/*
PORT STATUS
source path: backend/agent/coding_context.go
source lines: 146
draft lines: 133
confidence: high
status: phase_b_compile
*/
