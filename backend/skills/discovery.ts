// PORT: backend/skills/discovery.go
import fs from "node:fs";
import path from "node:path";
import { type runtime } from "../config/config";
import { type Skill } from "./types";
import { type SearchDir } from "./locations";
import { Effect } from "effect";
import {
  ExcludedSkillDirs,
  MaxSkillNameLength,
  MaxSkillDescriptionLength,
  SkillNameValidRe,
} from "./constants";
import {
  ParseFrontmatter,
  extractSkillDescription,
  extractSkillTags,
  extractRelatedSkills,
  normalizePlatforms,
  extractSkillConditions,
  toStringList,
  computeSkillCategory,
  skillMatchesPlatform,
} from "./frontmatter";
import { SkillMatchesEnvironment } from "./environment";
import { SearchLocations } from "./locations";

export interface IgnorePattern {
  regex: RegExp;
  negated: boolean;
  dirOnly: boolean;
}

export class GitIgnoreMatcher {
  patterns: IgnorePattern[] = [];

  clone(): GitIgnoreMatcher {
    const clone = new GitIgnoreMatcher();
    clone.patterns = [...this.patterns];
    return clone;
  }

  matches(relPath: string, isDir: boolean): boolean {
    const relPathSlash = relPath.split(path.sep).join("/");
    let ignored = false;
    for (const p of this.patterns) {
      if (p.dirOnly && !isDir) {
        continue;
      }
      if (p.regex.test(relPathSlash)) {
        ignored = !p.negated;
      }
    }
    return ignored;
  }
}

export function parseGitIgnore(content: string, prefix: string): GitIgnoreMatcher {
  const matcher = new GitIgnoreMatcher();
  const lines = content.split(/\r?\n/);
  for (let line of lines) {
    line = line.trim();
    if (line === "" || line.startsWith("#")) {
      continue;
    }

    let negated = false;
    if (line.startsWith("!")) {
      negated = true;
      line = line.slice(1);
    } else if (line.startsWith("\\!")) {
      line = line.slice(1);
    }

    let dirOnly = false;
    if (line.endsWith("/")) {
      dirOnly = true;
      line = line.slice(0, -1);
    }

    if (line === "") {
      continue;
    }

    const pat = prefix !== "" ? prefix + "/" + line : line;
    const regex = gitIgnoreToRegex(pat);
    if (regex !== null) {
      matcher.patterns.push({
        regex,
        negated,
        dirOnly,
      });
    }
  }
  return matcher;
}

export function gitIgnoreToRegex(pattern: string): RegExp | null {
  let sb = "^";

  const hasSlash = pattern.includes("/");
  if (!hasSlash) {
    sb += "(?:.*/)?";
  } else if (pattern.startsWith("/")) {
    pattern = pattern.slice(1);
  }

  for (let i = 0; i < pattern.length; i++) {
    const c = pattern[i];
    switch (c) {
      case "*":
        if (i + 1 < pattern.length && pattern[i + 1] === "*") {
          sb += ".*";
          i++;
          if (i + 1 < pattern.length && pattern[i + 1] === "/") {
            i++;
          }
        } else {
          sb += "[^/]*";
        }
        break;
      case "?":
        sb += "[^/]";
        break;
      case ".":
      case "+":
      case "(":
      case ")":
      case "^":
      case "$":
      case "{":
      case "}":
      case "[":
      case "]":
      case "|":
      case "\\":
        sb += "\\" + c;
        break;
      default:
        sb += c;
    }
  }
  sb += "$";
  try {
    return new RegExp(sb);
  } catch {
    return null;
  }
}

export function addIgnoreRules(ig: GitIgnoreMatcher, dir: string, rootDir: string): void {
  let prefix = "";
  try {
    const rel = path.relative(rootDir, dir);
    if (rel !== "" && rel !== ".") {
      prefix = rel.split(path.sep).join("/");
    }
  } catch {}

  const ignoreFileNames = [".gitignore", ".ignore", ".fdignore"];
  for (const filename of ignoreFileNames) {
    const p = path.join(dir, filename);
    try {
      if (fs.existsSync(p) && fs.statSync(p).isFile()) {
        const data = fs.readFileSync(p, "utf8");
        const parsed = parseGitIgnore(data, prefix);
        ig.patterns.push(...parsed.patterns);
      }
    } catch {}
  }
}

export function isExcludedSkillPath(filePath: string): boolean {
  const parts = filePath.split(/[/\\]/);
  for (const part of parts) {
    if (ExcludedSkillDirs[part]) {
      return true;
    }
  }
  return false;
}

export function IterSkillIndexFiles(skillsDir: string, filename: string): string[] {
  if (!fs.existsSync(skillsDir)) {
    return [];
  }

  const matches: string[] = [];
  const walk = (dir: string) => {
    let entries: fs.Dirent[] = [];
    try {
      entries = fs.readdirSync(dir, { withFileTypes: true });
    } catch {
      return;
    }
    for (const entry of entries) {
      const fullPath = path.join(dir, entry.name);
      let isDir = entry.isDirectory();
      if (entry.isSymbolicLink()) {
        try {
          const stat = fs.statSync(fullPath);
          isDir = stat.isDirectory();
        } catch {}
      }
      if (isDir && ExcludedSkillDirs[entry.name]) {
        continue;
      }
      if (entry.name === "skills-cursor") {
        continue;
      }
      if (isDir) {
        walk(fullPath);
      } else if (entry.name === filename) {
        matches.push(fullPath);
      }
    }
  };
  walk(skillsDir);

  let resolvedRoot = skillsDir;
  try {
    resolvedRoot = path.resolve(skillsDir);
  } catch {}

  matches.sort((a, b) => {
    let relA = a;
    let relB = b;
    try {
      relA = path.relative(resolvedRoot, a);
    } catch {}
    try {
      relB = path.relative(resolvedRoot, b);
    } catch {}
    const relASlash = relA.split(path.sep).join("/");
    const relBSlash = relB.split(path.sep).join("/");
    return relASlash.localeCompare(relBSlash);
  });

  return matches;
}

export function validateName(name: string): string[] {
  const errs: string[] = [];
  if (name.length > MaxSkillNameLength) {
    errs.push(`name exceeds ${MaxSkillNameLength} characters (${name.length})`);
  }
  if (!SkillNameValidRe.test(name)) {
    errs.push("name contains invalid characters (must be lowercase a-z, 0-9, hyphens only)");
  }
  if (name.startsWith("-") || name.endsWith("-")) {
    errs.push("name must not start or end with a hyphen");
  }
  if (name.includes("--")) {
    errs.push("name must not contain consecutive hyphens");
  }
  return errs;
}

export function validateDescription(desc: string): string[] {
  const errs: string[] = [];
  if (desc.trim() === "") {
    errs.push("description is required");
  } else if (desc.length > MaxSkillDescriptionLength) {
    errs.push(`description exceeds ${MaxSkillDescriptionLength} characters (${desc.length})`);
  }
  return errs;
}

export function loadSkillFromFile(
  filePath: string,
  source: string,
  skillsRoot: string
): [Skill | null, string[]] {
  const warnings: string[] = [];
  let data = "";
  try {
    data = fs.readFileSync(filePath, "utf8");
  } catch (err: any) {
    return [null, [err.message]];
  }

  const [fm, body] = ParseFrontmatter(data);
  if (fm === null) {
    return [null, ["missing frontmatter"]];
  }

  const desc = extractSkillDescription(fm);
  const descErrs = validateDescription(desc);
  warnings.push(...descErrs);

  const skillDir = path.dirname(filePath);
  const parentDirName = path.basename(skillDir);

  let name = typeof fm["name"] === "string" ? fm["name"] : "";
  if (name === "") {
    name = parentDirName;
  }

  const nameErrs = validateName(name);
  warnings.push(...nameErrs);

  const descFull = typeof fm["description"] === "string" ? fm["description"] : "";
  if (descFull === "") {
    return [null, warnings];
  }

  if (!skillMatchesPlatform(fm)) {
    return [null, warnings];
  }

  if (!SkillMatchesEnvironment(fm)) {
    return [null, warnings];
  }

  const disableModelInvocation = !!fm["disable-model-invocation"];

  const category = computeSkillCategory(filePath, skillsRoot);
  const conditions = extractSkillConditions(fm);
  const tags = extractSkillTags(fm);
  const related = extractRelatedSkills(fm);
  const platforms = normalizePlatforms(fm);

  let scope = "project";
  if (skillsRoot.includes(".hollow/skills") || skillsRoot.includes(".hollow/agent/skills")) {
    scope = "user";
  }

  let envs = toStringList(fm["environments"]);
  if (!envs) {
    envs = [];
  }

  const skill: Skill = {
    Name: name,
    Description: desc,
    FilePath: filePath,
    BaseDir: skillDir,
    DescriptionFull: descFull,
    SourceInfo: {
      source,
      scope,
      baseDir: skillsRoot,
    },
    DisableModelInvocation: disableModelInvocation,
    Category: category,
    Platforms: platforms,
    Tags: tags,
    RelatedSkills: related,
    Conditions: conditions,
    Environments: envs,
  };

  return [skill, warnings];
}

export function loadSkillsFromDirInternal(
  dir: string,
  source: string,
  includeRootFiles: boolean,
  ig: GitIgnoreMatcher | null,
  rootDir: string,
  skillsRoot: string
): [Skill[], string[]] {
  const skills: Skill[] = [];
  const diagnostics: string[] = [];

  if (!fs.existsSync(dir)) {
    return [[], []];
  }

  if (ig === null) {
    ig = new GitIgnoreMatcher();
  }
  addIgnoreRules(ig, dir, rootDir);

  let entries: fs.Dirent[] = [];
  try {
    entries = fs.readdirSync(dir, { withFileTypes: true });
  } catch {
    return [[], []];
  }

  // 1. Check if SKILL.md is present
  for (const entry of entries) {
    if (entry.name === "SKILL.md") {
      const fullPath = path.join(dir, entry.name);
      let rel = "";
      try {
        rel = path.relative(rootDir, fullPath);
      } catch {}
      if (ig.matches(rel, false)) {
        continue;
      }
      const [sk, warns] = loadSkillFromFile(fullPath, source, skillsRoot);
      diagnostics.push(...warns);
      if (sk !== null) {
        skills.push(sk);
      }
      return [skills, diagnostics];
    }
  }

  // 2. Otherwise recurse into subdirectories and load root .md files if allowed
  for (const entry of entries) {
    if (entry.name.startsWith(".")) {
      continue;
    }
    if (
      entry.name === "node_modules" ||
      entry.name === "skills-cursor" ||
      ExcludedSkillDirs[entry.name]
    ) {
      continue;
    }

    const fullPath = path.join(dir, entry.name);
    let rel = "";
    try {
      rel = path.relative(rootDir, fullPath);
    } catch {
      continue;
    }

    let isDir = entry.isDirectory();
    if (entry.isSymbolicLink()) {
      try {
        const stat = fs.statSync(fullPath);
        isDir = stat.isDirectory();
      } catch {}
    }

    if (ig.matches(rel, isDir)) {
      continue;
    }

    if (isDir) {
      const subIg = ig.clone();
      const [subSkills, subDiag] = loadSkillsFromDirInternal(
        fullPath,
        source,
        false,
        subIg,
        rootDir,
        skillsRoot
      );
      skills.push(...subSkills);
      diagnostics.push(...subDiag);
    } else if (includeRootFiles && entry.name.endsWith(".md")) {
      const [sk, warns] = loadSkillFromFile(fullPath, source, skillsRoot);
      diagnostics.push(...warns);
      if (sk !== null) {
        skills.push(sk);
      }
    }
  }

  return [skills, diagnostics];
}

export function resolvePlatform(): string {
  if (process.env.HOLLOW_PLATFORM) {
    return process.env.HOLLOW_PLATFORM;
  }
  if (process.env.HERMES_PLATFORM) {
    return process.env.HERMES_PLATFORM;
  }
  if (process.env.HOLLOW_SESSION_PLATFORM) {
    return process.env.HOLLOW_SESSION_PLATFORM;
  }
  if (process.env.HERMES_SESSION_PLATFORM) {
    return process.env.HERMES_SESSION_PLATFORM;
  }
  return "cli";
}

export function IsSkillDisabled(name: string, cfg: runtime): boolean {
  const platform = resolvePlatform();
  if (cfg.skills?.platform_disabled) {
    const list = cfg.skills.platform_disabled[platform];
    if (Array.isArray(list)) {
      for (const d of list) {
        if (d === name) {
          return true;
        }
      }
    }
  }
  if (cfg.skills?.disabled && Array.isArray(cfg.skills.disabled)) {
    for (const d of cfg.skills.disabled) {
      if (d === name) {
        return true;
      }
    }
  }
  return false;
}

export function DiscoverAllSkills(
  cwd: string,
  cfg: runtime
): Effect.Effect<[Skill[], string[]], Error> {
  return Effect.try({
    try: () => {
      const dirs = SearchLocations(cwd, cfg, "");
      return LoadSkillsFromDirs(cwd, dirs, cfg);
    },
    catch: (cause) => (cause instanceof Error ? cause : new Error(String(cause))),
  });
}

export function LoadSkillsFromDirs(
  cwd: string,
  dirs: SearchDir[],
  cfg: runtime
): [Skill[], string[]] {
  const allSkills: Skill[] = [];
  const allDiagnostics: string[] = [];
  const skillMap = new Map<string, Skill>();
  const canonicalSet = new Set<string>();

  const exclusionRegexes: RegExp[] = [];
  if (cfg.skills?.paths && Array.isArray(cfg.skills.paths)) {
    for (const p of cfg.skills.paths) {
      if (p.startsWith("!")) {
        const pat = p.slice(1);
        const rx = gitIgnoreToRegex(pat);
        if (rx !== null) {
          exclusionRegexes.push(rx);
        }
      }
    }
  }

  const addSkills = (skills: Skill[], diags: string[]) => {
    allDiagnostics.push(...diags);
    for (const sk of skills) {
      if (IsSkillDisabled(sk.Name, cfg)) {
        continue;
      }

      let excluded = false;
      const filePathSlash = sk.FilePath.split(path.sep).join("/");
      for (const rx of exclusionRegexes) {
        if (rx.test(filePathSlash)) {
          excluded = true;
          break;
        }
      }
      if (excluded) {
        continue;
      }

      let canonical = sk.FilePath;
      try {
        canonical = fs.realpathSync(sk.FilePath);
      } catch {}

      if (canonicalSet.has(canonical)) {
        continue;
      }

      const existing = skillMap.get(sk.Name);
      if (existing) {
        allDiagnostics.push(
          `name "${sk.Name}" collision: winner=${sk.FilePath} loser=${existing.FilePath}`
        );
      }

      skillMap.set(sk.Name, sk);
      canonicalSet.add(canonical);
    }
  };

  // Loop in reverse order (lowest precedence to highest) so later scans overwrite earlier
  for (let i = dirs.length - 1; i >= 0; i--) {
    const dir = dirs[i];
    try {
      if (fs.existsSync(dir.Path)) {
        const [s, d] = loadSkillsFromDirInternal(
          dir.Path,
          dir.Source,
          dir.IncludeRootMD,
          null,
          dir.Path,
          dir.Path
        );
        addSkills(s, d);
      }
    } catch {}
  }

  for (const sk of skillMap.values()) {
    allSkills.push(sk);
  }

  // Sort skills by category, then by name for stable list output
  allSkills.sort((a, b) => {
    if (a.Category !== b.Category) {
      return a.Category.localeCompare(b.Category);
    }
    return a.Name.localeCompare(b.Name);
  });

  return [allSkills, allDiagnostics];
}

/*
PORT STATUS
source path: backend/skills/discovery.go
source lines: 516
draft lines: 550
confidence: high
status: phase_b_compile
*/
