// PORT: backend/skills/tool_view.go
import fs from "node:fs";
import path from "node:path";
import { Effect } from "effect";
import { type runtime } from "../config/config";
import {
  type SkillReadinessStatus,
  type RequiredEnvVar,
  type SetupBlock,
  ReadinessAvailable,
  ReadinessSetupNeeded,
  ReadinessUnsupported,
  LoadHollowEnv,
  isEnvVarSet,
  normalizeSetupMetadata,
  getRequiredEnvironmentVariables,
} from "./readiness";

export interface SkillViewResult {
  Success: boolean;
  Name?: string;
  Description?: string;
  Category?: string;
  Content?: string;
  RawContent?: string;
  SkillDir?: string;
  LinkedFiles?: Record<string, string[]>;
  Tags?: string[];
  RelatedSkills?: string[];
  Warnings?: string[];
  UsageHint?: string;
  Error?: string;
  Matches?: string[];
  Hint?: string;
  File?: string;
  ReadinessStatus?: SkillReadinessStatus;
  RequiredEnvironmentVariables?: RequiredEnvVar[];
  MissingRequiredEnvironmentVariables?: string[];
  SetupNeeded: boolean;
  Setup?: SetupBlock;
}
import { ResolveSkillLookupName } from "./skill_aliases";
import { SearchLocations } from "./locations";
import { IterSkillIndexFiles, isExcludedSkillPath } from "./discovery";
import { ExcludedSkillDirs } from "./constants";
import { ParseFrontmatter } from "./frontmatter";
import { isPathWithinDir, hasTraversalComponent, validateWithinDir } from "./path_security";
import { PreprocessSkillContent } from "./preprocessing";
import { SkillsDir } from "./paths";
import { SkillGuardThreatPatterns } from "./guard_patterns";
import {
  parse_qualified_name,
  is_valid_namespace,
  is_plugin_disabled,
  find_plugin_skill,
  list_plugin_skills,
  get_plugin_sibling_banner,
} from "../plugins/registry";
import { skillMatchesPlatform, extractSkillTags, extractRelatedSkills, computeSkillCategory } from "./frontmatter";
import { IsSkillDisabled } from "./discovery";
import { BumpView, BumpUse } from "./usage";

interface SkillCandidate {
  skillDir: string;
  skillMd: string;
}

function findSkillCandidates(name: string, workDir: string, cfg: runtime): SkillCandidate[] {
  name = ResolveSkillLookupName(name);
  const candidates: SkillCandidate[] = [];
  const seen = new Set<string>();

  const recordCandidate = (skillDir: string, skillMd: string) => {
    let resolved = skillMd;
    try {
      resolved = path.resolve(skillMd);
    } catch {}
    if (seen.has(resolved)) {
      return;
    }
    seen.add(resolved);
    candidates.push({ skillDir, skillMd });
  };

  const dirs = SearchLocations(workDir, cfg, "");

  let localCategoryName = "";
  const idx = name.indexOf(":");
  if (idx >= 0) {
    const ns = name.slice(0, idx);
    const bare = name.slice(idx + 1);
    if (bare !== "") {
      localCategoryName = ns + "/" + bare;
    }
  }

  for (const dir of dirs) {
    const searchDir = dir.Path;
    if (!fs.existsSync(searchDir)) {
      continue;
    }

    // 1. Direct path searchDir/name
    const directPath = path.join(searchDir, name);
    try {
      if (fs.existsSync(directPath)) {
        const fi = fs.statSync(directPath);
        if (fi.isDirectory()) {
          const skillMd = path.join(directPath, "SKILL.md");
          if (fs.existsSync(skillMd)) {
            recordCandidate(directPath, skillMd);
          }
        } else if (directPath.endsWith(".md")) {
          recordCandidate("", directPath);
        } else {
          if (fs.existsSync(directPath + ".md")) {
            recordCandidate("", directPath + ".md");
          }
        }
      } else {
        if (fs.existsSync(directPath + ".md")) {
          recordCandidate("", directPath + ".md");
        }
      }
    } catch {}

    // 2. category/name via : syntax
    if (localCategoryName !== "") {
      const categorizedPath = path.join(searchDir, localCategoryName);
      try {
        if (fs.existsSync(categorizedPath)) {
          const fi = fs.statSync(categorizedPath);
          if (fi.isDirectory()) {
            const skillMd = path.join(categorizedPath, "SKILL.md");
            if (fs.existsSync(skillMd)) {
              recordCandidate(categorizedPath, skillMd);
            }
          } else if (categorizedPath.endsWith(".md")) {
            recordCandidate("", categorizedPath);
          } else {
            if (fs.existsSync(categorizedPath + ".md")) {
              recordCandidate("", categorizedPath + ".md");
            }
          }
        } else {
          if (fs.existsSync(categorizedPath + ".md")) {
            recordCandidate("", categorizedPath + ".md");
          }
        }
      } catch {}
    }

    // 3. Walk IterSkillIndexFiles matching basename
    for (const foundSkillMd of IterSkillIndexFiles(searchDir, "SKILL.md")) {
      if (isExcludedSkillPath(foundSkillMd)) {
        continue;
      }
      if (path.basename(path.dirname(foundSkillMd)) === name) {
        recordCandidate(path.dirname(foundSkillMd), foundSkillMd);
      }
    }

    // 4. legacy flat name.md
    const walkForLegacyFlat = (currentDir: string) => {
      let entries: fs.Dirent[] = [];
      try {
        entries = fs.readdirSync(currentDir, { withFileTypes: true });
      } catch {
        return;
      }
      for (const entry of entries) {
        const full = path.join(currentDir, entry.name);
        let isDir = entry.isDirectory();
        if (entry.isSymbolicLink()) {
          try {
            const stat = fs.statSync(full);
            isDir = stat.isDirectory();
          } catch {}
        }
        if (isDir) {
          if (!ExcludedSkillDirs[entry.name] && entry.name !== "skills-cursor") {
            walkForLegacyFlat(full);
          }
          continue;
        }
        if (entry.name === name + ".md" && entry.name !== "SKILL.md") {
          recordCandidate("", full);
        }
      }
    };
    walkForLegacyFlat(searchDir);
  }

  // Resolve collisions by precedence
  const candidateName = (c: SkillCandidate): string => {
    try {
      const data = fs.readFileSync(c.skillMd, "utf8");
      const [fm] = ParseFrontmatter(data);
      if (fm !== null && typeof fm["name"] === "string" && fm["name"] !== "") {
        return fm["name"];
      }
    } catch {}
    if (c.skillDir !== "") {
      return path.basename(c.skillDir);
    }
    const base = path.basename(c.skillMd);
    if (base.endsWith(".md")) {
      return base.slice(0, -3);
    }
    return base;
  };

  const groups: Record<string, SkillCandidate[]> = {};
  for (const c of candidates) {
    const nameVal = candidateName(c);
    if (nameVal !== "") {
      if (!groups[nameVal]) {
        groups[nameVal] = [];
      }
      groups[nameVal].push(c);
    } else {
      groups[c.skillMd] = [c];
    }
  }

  const filtered: SkillCandidate[] = [];
  for (const groupCands of Object.values(groups)) {
    if (groupCands.length <= 1) {
      filtered.push(...groupCands);
      continue;
    }

    let bestIdx = dirs.length;
    let bestCand = groupCands[0];
    let hasBest = false;

    for (const c of groupCands) {
      let cAbs = c.skillMd;
      try {
        cAbs = path.resolve(c.skillMd);
      } catch {}
      let cIdx = dirs.length;
      for (let idx = 0; idx < dirs.length; idx++) {
        if (cAbs.startsWith(dirs[idx].Path)) {
          cIdx = idx;
          break;
        }
      }
      if (cIdx < bestIdx) {
        bestIdx = cIdx;
        bestCand = c;
        hasBest = true;
      }
    }

    if (hasBest) {
      filtered.push(bestCand);
    }
  }

  return filtered;
}

function scanLinkedFiles(skillDir: string): Record<string, string[]> {
  const linked: Record<string, string[]> = {
    references: [],
    templates: [],
    assets: [],
    scripts: [],
    other: [],
  };

  const walk = (dir: string) => {
    let entries: fs.Dirent[] = [];
    try {
      entries = fs.readdirSync(dir, { withFileTypes: true });
    } catch {
      return;
    }
    for (const entry of entries) {
      const full = path.join(dir, entry.name);
      let isDir = entry.isDirectory();
      if (entry.isSymbolicLink()) {
        try {
          const stat = fs.statSync(full);
          isDir = stat.isDirectory();
        } catch {}
      }
      if (isDir) {
        walk(full);
        continue;
      }
      if (entry.name === "SKILL.md") {
        continue;
      }
      let rel = "";
      try {
        rel = path.relative(skillDir, full);
      } catch {
        continue;
      }
      const relPosix = rel.split(path.sep).join("/");
      if (relPosix.startsWith("references/")) {
        linked.references.push(relPosix);
      } else if (relPosix.startsWith("templates/")) {
        linked.templates.push(relPosix);
      } else if (relPosix.startsWith("assets/")) {
        linked.assets.push(relPosix);
      } else if (relPosix.startsWith("scripts/")) {
        linked.scripts.push(relPosix);
      } else {
        const ext = path.extname(entry.name).toLowerCase();
        if (
          ext === ".md" ||
          ext === ".py" ||
          ext === ".yaml" ||
          ext === ".yml" ||
          ext === ".json" ||
          ext === ".tex" ||
          ext === ".sh"
        ) {
          linked.other.push(relPosix);
        }
      }
    }
  };
  walk(skillDir);

  const filtered: Record<string, string[]> = {};
  for (const [k, v] of Object.entries(linked)) {
    if (v.length > 0) {
      v.sort();
      filtered[k] = v;
    }
  }
  return filtered;
}

export function executeSkillViewInternal(
  name: string,
  filePath: string,
  workDir: string,
  cfg: runtime,
  sessionId: string,
  preprocess: boolean
): SkillViewResult {
  name = ResolveSkillLookupName(name);
  name = name.trim();
  filePath = filePath.trim();

  if (name === "") {
    return { Success: false, Error: "Skill name is required.", SetupNeeded: false };
  }

  if (name.includes(":")) {
    const [ns, bare] = parse_qualified_name(name);
    if (is_valid_namespace(ns)) {
      if (is_plugin_disabled(ns, cfg)) {
        return {
          Success: false,
          Error: `Plugin '${ns}' is disabled. Re-enable with: hollow plugins enable ${ns}`,
          SetupNeeded: false,
        };
      }

      const available = Effect.runSync(list_plugin_skills(ns));
      const findRes = Effect.runSync(Effect.either(find_plugin_skill(name)));

      if (findRes._tag === "Right" || available.length > 0) {
        if (findRes._tag === "Left") {
          const qualified = available.map((s) => ns + ":" + s);
          return {
            Success: false,
            Error: `Skill '${bare}' not found in plugin '${ns}'.`,
            Matches: qualified,
            Hint: `The '${ns}' plugin provides ${available.length} skill(s).`,
            SetupNeeded: false,
          };
        }

        const pluginSkillMd = findRes.right;
        let content = "";
        try {
          content = fs.readFileSync(pluginSkillMd, "utf8");
        } catch (err: any) {
          return {
            Success: false,
            Error: `Failed to read skill '${name}': ${err.message}`,
            SetupNeeded: false,
          };
        }

        const warnings: string[] = [];
        const contentLower = content.toLowerCase();
        let injectionDetected = false;
        for (const p of SkillGuardThreatPatterns) {
          if (p.Regex.test(contentLower)) {
            injectionDetected = true;
            break;
          }
        }
        if (injectionDetected) {
          warnings.push("skill content contains patterns that may indicate prompt injection");
        }

        const [fm, body] = ParseFrontmatter(content);
        const resolvedFm = fm || {};

        if (!skillMatchesPlatform(resolvedFm)) {
          return {
            Success: false,
            Error: `Skill '${name}' is not supported on this platform.`,
            ReadinessStatus: ReadinessUnsupported,
            SetupNeeded: false,
          };
        }

        const desc = typeof resolvedFm["description"] === "string" ? resolvedFm["description"] : "";

        const pluginSkillDir = path.dirname(pluginSkillMd);
        let processedBody = body;
        if (preprocess) {
          processedBody = Effect.runSync(
            PreprocessSkillContent(
              body,
              pluginSkillDir,
              sessionId,
              cfg.skills?.inline_shell || false,
              cfg.skills?.inline_shell_timeout || 10
            )
          );
        }

        const banner = get_plugin_sibling_banner(ns, bare);

        const requiredEnvVars = getRequiredEnvironmentVariables(resolvedFm);
        const envMap = LoadHollowEnv();

        const missingRequiredEnvVars: string[] = [];
        for (const envVar of requiredEnvVars) {
          if (!envVar.Optional && !isEnvVarSet(envVar.Name, envMap)) {
            missingRequiredEnvVars.push(envVar.Name);
          }
        }

        const setupNeeded = missingRequiredEnvVars.length > 0;
        const readinessStatus = setupNeeded ? ReadinessSetupNeeded : ReadinessAvailable;

        const setupObj = normalizeSetupMetadata(resolvedFm);

        const tags = extractSkillTags(resolvedFm);
        const related = extractRelatedSkills(resolvedFm);

        if (filePath !== "") {
          if (hasTraversalComponent(filePath)) {
            return {
              Success: false,
              Error: "Path traversal ('..') is not allowed.",
              Hint: "Use a relative path within the skill directory",
              SetupNeeded: false,
            };
          }
          const travErr = validateWithinDir(filePath, pluginSkillDir);
          if (travErr !== "") {
            return {
              Success: false,
              Error: travErr,
              Hint: "Use a relative path within the skill directory",
              SetupNeeded: false,
            };
          }

          const targetFile = path.join(pluginSkillDir, filePath);
          if (!fs.existsSync(targetFile)) {
            return {
              Success: false,
              Error: `File '${filePath}' not found in skill '${name}'.`,
              LinkedFiles: scanLinkedFiles(pluginSkillDir),
              SetupNeeded: false,
            };
          }

          let fileData = "";
          try {
            fileData = fs.readFileSync(targetFile, "utf8");
          } catch (err: any) {
            return {
              Success: false,
              Error: `Failed to read file: ${err.message}`,
              SetupNeeded: false,
            };
          }

          return {
            Success: true,
            Name: name,
            File: filePath,
            Content: fileData,
            SkillDir: pluginSkillDir,
            Warnings: warnings,
            SetupNeeded: false,
          };
        }

        return {
          Success: true,
          Name: name,
          Description: desc,
          Category: ns,
          Content: banner + processedBody,
          RawContent: body,
          SkillDir: pluginSkillDir,
          LinkedFiles: scanLinkedFiles(pluginSkillDir),
          Tags: tags,
          RelatedSkills: related,
          Warnings: warnings,
          ReadinessStatus: readinessStatus,
          RequiredEnvironmentVariables: requiredEnvVars,
          MissingRequiredEnvironmentVariables: missingRequiredEnvVars,
          SetupNeeded: setupNeeded,
          Setup: setupObj,
        };
      }
    }
  }

  const candidates = findSkillCandidates(name, workDir, cfg);

  if (candidates.length > 1) {
    const paths = candidates.map((c) => c.skillMd);
    return {
      Success: false,
      Error: `Ambiguous skill name '${name}': ${candidates.length} skills match. Refusing to guess.`,
      Matches: paths,
      Hint: "Pass the full relative path instead of the bare name (e.g., 'category/skill-name').",
      SetupNeeded: false,
    };
  }

  if (candidates.length === 0) {
    return {
      Success: false,
      Error: `Skill '${name}' not found.`,
      Hint: "Use skills_list to see all available skills",
      SetupNeeded: false,
    };
  }

  const candidate = candidates[0];
  const skillDir = candidate.skillDir;
  const skillMd = candidate.skillMd;
  const resolvedSkillDir = skillDir !== "" ? skillDir : path.dirname(skillMd);

  let content = "";
  try {
    content = fs.readFileSync(skillMd, "utf8");
  } catch (err: any) {
    return {
      Success: false,
      Error: `Failed to read skill '${name}': ${err.message}`,
      SetupNeeded: false,
    };
  }

  // Security checks: traversal outside trusted
  const warnings: string[] = [];
  let outsideTrusted = true;
  const dirs = SearchLocations(workDir, cfg, "");
  for (const dir of dirs) {
    if (isPathWithinDir(skillMd, dir.Path)) {
      outsideTrusted = false;
      break;
    }
  }

  if (outsideTrusted) {
    warnings.push(`skill file is outside the trusted skills directory (${SkillsDir()}): ${skillMd}`);
  }

  // Check injection
  const contentLower = content.toLowerCase();
  let injectionDetected = false;
  for (const p of SkillGuardThreatPatterns) {
    if (p.Regex.test(contentLower)) {
      injectionDetected = true;
      break;
    }
  }
  if (injectionDetected) {
    warnings.push("skill content contains patterns that may indicate prompt injection");
  }

  const [fm, body] = ParseFrontmatter(content);
  const resolvedFm = fm || {};

  if (!skillMatchesPlatform(resolvedFm)) {
    return {
      Success: false,
      Error: `Skill '${name}' is not supported on this platform.`,
      ReadinessStatus: ReadinessUnsupported,
      SetupNeeded: false,
    };
  }

  let resolvedName = typeof resolvedFm["name"] === "string" ? resolvedFm["name"] : "";
  if (resolvedName === "") {
    resolvedName = path.basename(resolvedSkillDir);
  }

  if (IsSkillDisabled(resolvedName, cfg)) {
    return {
      Success: false,
      Error: `Skill '${resolvedName}' is disabled.`,
      SetupNeeded: false,
    };
  }

  // Read supporting file if file_path is specified
  if (filePath !== "" && skillDir !== "") {
    if (hasTraversalComponent(filePath)) {
      return {
        Success: false,
        Error: "Path traversal ('..') is not allowed.",
        Hint: "Use a relative path within the skill directory",
        SetupNeeded: false,
      };
    }
    const travErr = validateWithinDir(filePath, skillDir);
    if (travErr !== "") {
      return {
        Success: false,
        Error: travErr,
        Hint: "Use a relative path within the skill directory",
        SetupNeeded: false,
      };
    }

    const targetFile = path.join(skillDir, filePath);
    if (!fs.existsSync(targetFile)) {
      return {
        Success: false,
        Error: `File '${filePath}' not found in skill '${name}'.`,
        LinkedFiles: scanLinkedFiles(skillDir),
        SetupNeeded: false,
      };
    }

    let fileData = "";
    try {
      fileData = fs.readFileSync(targetFile, "utf8");
    } catch (err: any) {
      return {
        Success: false,
        Error: `Failed to read file: ${err.message}`,
        SetupNeeded: false,
      };
    }

    return {
      Success: true,
      Name: resolvedName,
      File: filePath,
      Content: fileData,
      SkillDir: skillDir,
      Warnings: warnings,
      SetupNeeded: false,
    };
  }

  let processedBody = body;
  if (preprocess) {
    processedBody = Effect.runSync(
      PreprocessSkillContent(
        body,
        resolvedSkillDir,
        sessionId,
        cfg.skills?.inline_shell || false,
        cfg.skills?.inline_shell_timeout || 10
      )
    );
  }

  const linkedFiles = scanLinkedFiles(resolvedSkillDir);
  const tags = extractSkillTags(resolvedFm);
  const related = extractRelatedSkills(resolvedFm);
  const desc = typeof resolvedFm["description"] === "string" ? resolvedFm["description"] : "";

  let usageHint = "";
  if (Object.keys(linkedFiles).length > 0) {
    usageHint = "To view linked files, call skill_view(name, file_path) where file_path is e.g. 'references/api.md'";
  }

  const requiredEnvVars = getRequiredEnvironmentVariables(resolvedFm);
  const envMap = LoadHollowEnv();

  const missingRequiredEnvVars: string[] = [];
  for (const envVar of requiredEnvVars) {
    if (!envVar.Optional && !isEnvVarSet(envVar.Name, envMap)) {
      missingRequiredEnvVars.push(envVar.Name);
    }
  }

  const setupNeeded = missingRequiredEnvVars.length > 0;
  const readinessStatus = setupNeeded ? ReadinessSetupNeeded : ReadinessAvailable;
  const setupObj = normalizeSetupMetadata(resolvedFm);

  return {
    Success: true,
    Name: resolvedName,
    Description: desc,
    Category: computeSkillCategory(skillMd, SkillsDir()),
    Content: processedBody,
    RawContent: body,
    SkillDir: resolvedSkillDir,
    LinkedFiles: linkedFiles,
    Tags: tags,
    RelatedSkills: related,
    Warnings: warnings,
    UsageHint: usageHint,
    ReadinessStatus: readinessStatus,
    RequiredEnvironmentVariables: requiredEnvVars,
    MissingRequiredEnvironmentVariables: missingRequiredEnvVars,
    SetupNeeded: setupNeeded,
    Setup: setupObj,
  };
}

export function ExecuteSkillView(
  argsJSON: string,
  workDir: string,
  cfg: runtime,
  sessionId: string
): [string, boolean] {
  let args = { name: "", file_path: "" };
  try {
    args = JSON.parse(argsJSON);
  } catch {}

  const result = executeSkillViewInternal(args.name, args.file_path || "", workDir, cfg, sessionId, true);
  if (result.Success) {
    const resolved = result.Name || args.name;
    BumpView(resolved);
    BumpUse(resolved);
  }

  try {
    const outBytes = JSON.stringify(result, null, "  ");
    return [outBytes, !result.Success];
  } catch {
    return [`{"success": false, "error": "json marshal error"}`, true];
  }
}

/*
PORT STATUS
source path: backend/skills/tool_view.go
source lines: 634
draft lines: 580
confidence: high
status: phase_b_compile
*/
