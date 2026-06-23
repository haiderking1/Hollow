// PORT: backend/skills/prompt_index.go
import fs from "node:fs";
import path from "node:path";
import { Effect } from "effect";
import { type runtime } from "../config/config";
import { type SearchDir } from "./locations";
import { type SkillSnapshotEntry, type SkillsPromptSnapshot } from "./types";
import { SnapshotPath } from "./paths";
import { home_dir } from "../hollowhome/home";
import { get_available_toolsets } from "../toolsets/registry";
import { resolvePlatform, IterSkillIndexFiles } from "./discovery";
import { ParseFrontmatter, skillMatchesPlatform } from "./frontmatter";
import { SkillMatchesEnvironment } from "./environment";
import { skillShouldShow } from "./frontmatter";
import { SearchLocations } from "./locations";
import { DiscoverAllSkills } from "./discovery";
import { atomicWrite } from "./usage";
import {
  MaxSkillNameLength,
  MaxSkillDescriptionLength,
  PromptIndexDescriptionMax,
  SkillsPromptCacheMax,
  SkillsSnapshotVersion,
} from "./constants";

let promptCache: Record<string, string> = {};
let promptCacheKeys: string[] = [];

export function ClearSkillsPromptCache(): void {
  promptCache = {};
  promptCacheKeys = [];

  const snap = SnapshotPath();
  try {
    if (fs.existsSync(snap)) {
      fs.unlinkSync(snap);
    }
  } catch {}
}

function getFromCache(key: string): string | null {
  const val = promptCache[key];
  if (val !== undefined) {
    // Move key to the end of keys (most recently used)
    const idx = promptCacheKeys.indexOf(key);
    if (idx >= 0) {
      promptCacheKeys.splice(idx, 1);
    }
    promptCacheKeys.push(key);
    return val;
  }
  return null;
}

function setToCache(key: string, val: string): void {
  if (promptCache[key] !== undefined) {
    promptCache[key] = val;
    const idx = promptCacheKeys.indexOf(key);
    if (idx >= 0) {
      promptCacheKeys.splice(idx, 1);
    }
    promptCacheKeys.push(key);
    return;
  }

  if (promptCacheKeys.length >= SkillsPromptCacheMax) {
    const oldest = promptCacheKeys.shift();
    if (oldest !== undefined) {
      delete promptCache[oldest];
    }
  }
  promptCache[key] = val;
  promptCacheKeys.push(key);
}

export function buildFullManifest(dirs: SearchDir[]): Record<string, [number, number]> {
  const manifest: Record<string, [number, number]> = {};
  for (const dir of dirs) {
    if (!fs.existsSync(dir.Path)) {
      continue;
    }
    for (const filename of ["SKILL.md", "DESCRIPTION.md"]) {
      for (const fp of IterSkillIndexFiles(dir.Path, filename)) {
        try {
          const fi = fs.statSync(fp);
          let abs = fp;
          try {
            abs = path.resolve(fp);
          } catch {}
          const key = abs.split(path.sep).join("/");
          const mtimeNs = (fi as any).mtimeNs ? Number((fi as any).mtimeNs) : Math.round(fi.mtimeMs) * 1000000;
          manifest[key] = [mtimeNs, fi.size];
        } catch {}
      }
    }
  }
  return manifest;
}

function getManifestHashString(manifest: Record<string, [number, number]>): string {
  const keys = Object.keys(manifest).sort();
  let sb = "";
  for (const k of keys) {
    const val = manifest[k];
    sb += `${k}:${val[0]}:${val[1]};`;
  }
  return sb;
}

function buildPromptCacheKey(
  workDir: string,
  cfg: runtime,
  toolNames: string[],
  manifest: Record<string, [number, number]>
): string {
  let sb = "";
  sb += home_dir() + "|";
  sb += workDir + "|";
  const disabled = [...(cfg.skills?.disabled || [])].sort();
  sb += disabled.join(",") + "|";
  const paths = [...(cfg.skills?.paths || [])].sort();
  sb += paths.join(",") + "|";
  const tools = [...toolNames].sort();
  sb += tools.join(",") + "|";
  const activeToolsets = get_available_toolsets(toolNames);
  sb += activeToolsets.join(",") + "|";
  sb += resolvePlatform() + "|";
  sb += (cfg.agent?.coding_context || "") + "|";
  sb += getManifestHashString(manifest);
  return sb;
}

export function loadSkillsSnapshot(
  manifest: Record<string, [number, number]>
): SkillsPromptSnapshot | null {
  const snap = SnapshotPath();
  try {
    if (!fs.existsSync(snap)) {
      return null;
    }
    const dataBytes = fs.readFileSync(snap);
    const snapshot = JSON.parse(dataBytes.toString()) as SkillsPromptSnapshot;
    if (snapshot.version !== SkillsSnapshotVersion) {
      return null;
    }

    const manifestKeys = Object.keys(manifest);
    const snapshotManifestKeys = Object.keys(snapshot.manifest);
    if (manifestKeys.length !== snapshotManifestKeys.length) {
      return null;
    }

    for (const k of manifestKeys) {
      const old = snapshot.manifest[k];
      const v = manifest[k];
      if (!old || old[0] !== v[0] || old[1] !== v[1]) {
        return null;
      }
    }

    return snapshot;
  } catch {
    return null;
  }
}

export function writeSkillsSnapshot(
  manifest: Record<string, [number, number]>,
  skills: SkillSnapshotEntry[],
  categoryDescs: Record<string, string>
): void {
  const snap = SnapshotPath();
  const snapshot: SkillsPromptSnapshot = {
    version: SkillsSnapshotVersion,
    manifest: manifest,
    skills: skills,
    category_descriptions: categoryDescs,
  };
  try {
    const dataBytes = Buffer.from(JSON.stringify(snapshot, null, "  "));
    Effect.runSync(atomicWrite(snap, dataBytes));
  } catch {}
}

function readCategoryDescriptions(dir: string): Record<string, string> {
  const descriptions: Record<string, string> = {};
  let resolvedRoot = dir;
  try {
    resolvedRoot = path.resolve(dir);
  } catch {}

  for (const fp of IterSkillIndexFiles(dir, "DESCRIPTION.md")) {
    try {
      const data = fs.readFileSync(fp, "utf8");
      const [fm] = ParseFrontmatter(data);
      if (fm === null) {
        continue;
      }
      const descVal = fm["description"];
      if (typeof descVal !== "string" || descVal === "") {
        continue;
      }
      let rel = fp;
      try {
        rel = path.relative(resolvedRoot, fp);
      } catch {}
      const relSlash = rel.split(path.sep).join("/");
      const parts = relSlash.split("/");
      let cat = "general";
      if (parts.length > 1) {
        cat = parts.slice(0, parts.length - 1).join("/");
      }
      descriptions[cat] = descVal.trim().replace(/^['"]|['"]$/g, "");
    } catch {}
  }
  return descriptions;
}

export function readAllCategoryDescriptions(dirs: SearchDir[]): Record<string, string> {
  const descriptions: Record<string, string> = {};
  for (const dir of dirs) {
    try {
      if (fs.existsSync(dir.Path)) {
        const descs = readCategoryDescriptions(dir.Path);
        for (const [cat, desc] of Object.entries(descs)) {
          if (descriptions[cat] === undefined) {
            descriptions[cat] = desc;
          }
        }
      }
    } catch {}
  }
  return descriptions;
}

export const SkillsIndexHeader = "You also have access to the following custom skills. Each skill is represented as a slash command. When you need to perform a task covered by a skill, you MUST use that skill's slash command as your primary tool, as it contains specialized instructions, checklists, and configurations tailored for that task.";

export const SkillsIndexFooter = "To invoke a skill, type the slash command (e.g., `/my-skill-name`) as a message. If a skill requires arguments, you may pass them as additional text after the slash command. You can also view a skill's full instructions and checklist by calling `skill_view(name=\"<skill-name>\")` or `skill_view(name=\"<category>/<skill-name>\")`.";

export function BuildIndexPrompt(workDir: string, cfg: runtime, toolNames: string[]): string {
  const dirs = SearchLocations(workDir, cfg, "");
  const manifest = buildFullManifest(dirs);
  const cacheKey = buildPromptCacheKey(workDir, cfg, toolNames, manifest);

  const cached = getFromCache(cacheKey);
  if (cached !== null) {
    return cached;
  }

  const toolsSet: Record<string, boolean> = {};
  for (const tn of toolNames) {
    toolsSet[tn] = true;
  }

  let categoryDescs: Record<string, string>;
  let skillsToRender: SkillSnapshotEntry[] = [];

  const snapshot = loadSkillsSnapshot(manifest);
  if (snapshot !== null) {
    skillsToRender = snapshot.skills;
    categoryDescs = snapshot.category_descriptions || {};
  } else {
    const discoverRes = Effect.runSync(DiscoverAllSkills(workDir, cfg));
    const [discovered] = discoverRes;

    const entries: SkillSnapshotEntry[] = [];
    for (const sk of discovered) {
      entries.push({
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

    categoryDescs = readAllCategoryDescriptions(dirs);
    writeSkillsSnapshot(manifest, entries, categoryDescs);
    skillsToRender = entries;
  }

  const toolsetsList = get_available_toolsets(toolNames);
  const toolsetsSet: Record<string, boolean> = {};
  for (const ts of toolsetsList) {
    toolsetsSet[ts] = true;
  }

  const skillsByCategory: Record<string, Record<string, string>> = {};
  const seenNames = new Set<string>();

  for (const entry of skillsToRender) {
    if (entry.disable_model_invocation) {
      continue;
    }
    const fmDummy = {
      platforms: entry.platforms,
      environments: entry.environments,
    };
    if (!skillMatchesPlatform(fmDummy)) {
      continue;
    }
    if (!SkillMatchesEnvironment(fmDummy)) {
      continue;
    }
    if (!skillShouldShow(entry.conditions, toolsSet, toolsetsSet)) {
      continue;
    }

    const name = entry.frontmatter_name;
    if (seenNames.has(name)) {
      continue;
    }
    seenNames.add(name);

    let cat = entry.category;
    if (cat === "") {
      cat = "general";
    }
    if (!skillsByCategory[cat]) {
      skillsByCategory[cat] = {};
    }

    let desc = entry.description;
    if (desc.length > PromptIndexDescriptionMax) {
      desc = desc.slice(0, PromptIndexDescriptionMax - 3) + "...";
    }
    skillsByCategory[cat][name] = desc;
  }

  if (Object.keys(skillsByCategory).length === 0) {
    return "";
  }

  const isCoding = isCodingContext(workDir, cfg);
  let configMode = (cfg.agent?.coding_context || "").trim().toLowerCase();
  if (configMode === "") {
    configMode = "auto";
  }
  let hasDemoted = false;

  const indexLines: string[] = [];
  const categories = Object.keys(skillsByCategory).sort();

  for (const cat of categories) {
    const skillNames = Object.keys(skillsByCategory[cat]).sort();

    if (shouldDemoteCategory(cat, isCoding, configMode)) {
      hasDemoted = true;
      indexLines.push(`  ${cat} [names only]: ${skillNames.join(", ")}`);
      continue;
    }

    const desc = categoryDescs[cat];
    if (desc && desc !== "") {
      indexLines.push(`  ${cat}: ${desc}`);
    } else {
      indexLines.push(`  ${cat}:`);
    }

    for (const name of skillNames) {
      const desc = skillsByCategory[cat][name];
      if (desc && desc !== "") {
        indexLines.push(`    - ${name}: ${desc}`);
      } else {
        indexLines.push(`    - ${name}`);
      }
    }
  }

  let hiddenNote = "";
  if (hasDemoted) {
    hiddenNote = "\n(Categories marked [names only] are outside the current coding context, so their descriptions are omitted — the skills work normally and load with skill_view(name) as usual.)";
  }

  const result =
    SkillsIndexHeader + "\n" +
    "<available_skills>\n" +
    indexLines.join("\n") +
    "\n</available_skills>\n" +
    SkillsIndexFooter +
    hiddenNote;

  setToCache(cacheKey, result);
  return result;
}

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

const NonCodingCategories: Record<string, boolean> = {
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
    return fs.existsSync(gitDir) && fs.statSync(gitDir).isDirectory();
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
  const home = home_dir();
  for (let depth = 0; depth <= 6; depth++) {
    if (curr === home) {
      break;
    }
    for (const marker of projectMarkers) {
      if (fs.existsSync(path.join(curr, marker))) {
        return curr;
      }
    }
    const parent = path.dirname(curr);
    if (parent === curr) {
      break;
    }
    curr = parent;
  }
  return "";
}

function isCodingContext(workDir: string, cfg: runtime): boolean {
  let mode = (cfg.agent?.coding_context || "").trim().toLowerCase();
  if (mode === "") {
    mode = "auto";
  }
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

  const home = home_dir();
  let gitRoot = findGitRoot(workDir);
  if (gitRoot !== "" && gitRoot === home) {
    gitRoot = "";
  }

  if (gitRoot !== "" || findMarkerRoot(workDir) !== "") {
    return true;
  }

  return false;
}

function shouldDemoteCategory(cat: string, isCoding: boolean, configMode: string): boolean {
  if (!isCoding || configMode !== "focus") {
    return false;
  }
  const parts = cat.split("/");
  return !!NonCodingCategories[parts[0]];
}

/*
PORT STATUS
source path: backend/skills/prompt_index.go
source lines: 508
draft lines: 470
confidence: high
status: phase_b_compile
*/
