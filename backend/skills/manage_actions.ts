// PORT: backend/skills/manage_actions.go
import fs from "node:fs";
import path from "node:path";
import { Effect } from "effect";
import { type load_runtime } from "../config/config";
import { type SkillManageResult } from "./types";
import { SkillsDir, SnapshotPath } from "./paths";
import { ClearSkillsPromptCache } from "./prompt_index";
import { SecurityScanSkillDir } from "./guard";
import { ArchiveSkill, LoadUsage } from "./usage";
import { IsProtectedBuiltin, IsBundledSkillName, MarkSuppressed } from "./curator";
import { ResolveSkillLookupName } from "./skill_aliases";
import {
  ExcludedSkillDirs,
  AllowedSkillSubdirs,
  MaxSkillNameLength,
  MaxSkillDescriptionLength,
  MaxSkillContentChars,
  MaxSkillFileBytes,
  SkillManageNameRe,
} from "./constants";
import { ParseFrontmatter } from "./frontmatter";
import { SearchLocations } from "./locations";
import { IterSkillIndexFiles, isExcludedSkillPath } from "./discovery";
import { hasTraversalComponent, validateWithinDir } from "./path_security";
import { atomicWrite } from "./usage";
import { load_runtime as configLoadRuntime } from "../config/config";

import * as fslockUnix from "../fslock/lock_unix";
import * as fslockWindows from "../fslock/lock_windows";

const fslock = process.platform === "win32" ? fslockWindows : fslockUnix;

function withFileLock<A, E>(
  filePath: string,
  f: () => Effect.Effect<A, E>
): Effect.Effect<A, E | Error> {
  const lockPath = filePath + ".lock";
  const parentDir = path.dirname(lockPath);
  try {
    if (!fs.existsSync(parentDir)) {
      fs.mkdirSync(parentDir, { recursive: true, mode: 0o700 });
    }
  } catch {}

  return Effect.acquireUseRelease(
    // acquire file descriptor
    Effect.try({
      try: () => {
        const fd = fs.openSync(lockPath, "w+");
        return { fd, lockPath };
      },
      catch: (cause) => new Error("failed to create lock file: " + String(cause)),
    }),
    // use lock
    ({ fd }) => {
      return fslock.lock({ fd }).pipe(
        Effect.mapError((err) => new Error("failed to acquire file lock: " + String(err.cause))),
        Effect.flatMap(() => f()),
        Effect.ensuring(
          fslock.unlock({ fd }).pipe(
            Effect.catchAll(() => Effect.void)
          )
        )
      );
    },
    // release file descriptor
    ({ fd, lockPath }) => {
      return Effect.sync(() => {
        try {
          fs.closeSync(fd);
        } catch {}
        try {
          fs.unlinkSync(lockPath);
        } catch {}
      });
    }
  );
}

function validateManageName(name: string): string {
  if (name === "") {
    return "Skill name is required.";
  }
  if (name.length > MaxSkillNameLength) {
    return `Skill name exceeds ${MaxSkillNameLength} characters.`;
  }
  if (!SkillManageNameRe.test(name)) {
    return `Invalid skill name '${name}'. Use lowercase letters, numbers, hyphens, dots, and underscores. Must start with a letter or digit.`;
  }
  return "";
}

function validateCategory(category: string): string {
  const trimmed = category.trim();
  if (trimmed === "") {
    return "";
  }
  if (trimmed.includes("/") || trimmed.includes("\\")) {
    return `Invalid category '${category}'. Use lowercase letters, numbers, hyphens, dots, and underscores. Categories must be a single directory name.`;
  }
  if (trimmed.length > MaxSkillNameLength) {
    return `Category exceeds ${MaxSkillNameLength} characters.`;
  }
  if (!SkillManageNameRe.test(trimmed)) {
    return `Invalid category '${category}'. Use lowercase letters, numbers, hyphens, dots, and underscores. Categories must be a single directory name.`;
  }
  return "";
}

function validateFrontmatter(content: string): string {
  const trimmed = content.trim();
  if (trimmed === "") {
    return "Content cannot be empty.";
  }
  if (!trimmed.startsWith("---")) {
    return "SKILL.md must start with YAML frontmatter (---). See existing skills for format.";
  }

  const [fm, body] = ParseFrontmatter(content);
  if (fm === null) {
    return "SKILL.md frontmatter is not closed. Ensure you have a closing '---' line.";
  }

  const name = typeof fm["name"] === "string" ? fm["name"] : "";
  if (name === "") {
    return "Frontmatter must include 'name' field.";
  }
  const desc = typeof fm["description"] === "string" ? fm["description"] : "";
  if (desc === "") {
    return "Frontmatter must include 'description' field.";
  }
  if (desc.length > MaxSkillDescriptionLength) {
    return `Description exceeds ${MaxSkillDescriptionLength} characters.`;
  }

  if (body.trim() === "") {
    return "SKILL.md must have content after the frontmatter (instructions, procedures, etc.).";
  }

  return "";
}

function validateContentSize(content: string, label: string): string {
  if (content.length > MaxSkillContentChars) {
    return `${label} content is ${formatNumber(content.length)} characters (limit: ${formatNumber(MaxSkillContentChars)}). Consider splitting into a smaller SKILL.md with supporting files in references/ or templates/.`;
  }
  return "";
}

function formatNumber(n: number): string {
  let s = String(n);
  if (s.length <= 3) {
    return s;
  }
  const parts: string[] = [];
  while (s.length > 3) {
    parts.unshift(s.slice(-3));
    s = s.slice(0, -3);
  }
  if (s.length > 0) {
    parts.unshift(s);
  }
  return parts.join(",");
}

export function FindSkillDirectory(name: string): string {
  let cwd = "";
  try {
    cwd = process.cwd();
  } catch {}
  let cfg: any = {};
  try {
    cfg = Effect.runSync(configLoadRuntime());
  } catch {}

  const dirs = SearchLocations(cwd, cfg, "");
  for (const dir of dirs) {
    for (const skillFile of IterSkillIndexFiles(dir.Path, "SKILL.md")) {
      if (isExcludedSkillPath(skillFile)) {
        continue;
      }
      if (path.basename(path.dirname(skillFile)) === name) {
        return path.dirname(skillFile);
      }
      // Check frontmatter name
      try {
        const data = fs.readFileSync(skillFile, "utf8");
        const [fm] = ParseFrontmatter(data);
        if (fm !== null && fm["name"] === name) {
          return path.dirname(skillFile);
        }
      } catch {}
    }
  }
  return "";
}

function resolveSkillDir(name: string, category: string): string {
  const skillsDir = SkillsDir();
  const trimmedCat = category.trim();
  if (trimmedCat !== "") {
    return path.join(skillsDir, trimmedCat, name);
  }
  return path.join(skillsDir, name);
}

function skillNotFoundError(name: string, suffix: string): string {
  let base = `Skill '${name}' not found. Use skills_list() to see available skills.`;
  if (suffix !== "") {
    base += suffix;
  }
  return base;
}

function validateFilePath(filePath: string): string {
  if (filePath === "") {
    return "file_path is required.";
  }
  if (hasTraversalComponent(filePath)) {
    return "Path traversal ('..') is not allowed.";
  }
  const parts = filePath.split(/[/\\]/).filter((p) => p !== "");
  if (parts.length === 0 || !AllowedSkillSubdirs[parts[0]]) {
    const allowed = Object.keys(AllowedSkillSubdirs).sort();
    return `File must be under one of: ${allowed.join(", ")}. Got: '${filePath}'`;
  }
  if (parts.length < 2) {
    return `Provide a file path, not just a directory. Example: '${parts[0]}/myfile.md'`;
  }
  return "";
}

function resolveSkillTarget(skillDir: string, filePath: string): [string, string] {
  const target = path.join(skillDir, filePath);
  const err = validateWithinDir(target, skillDir);
  if (err !== "") {
    return ["", err];
  }
  return [target, ""];
}

export function normalizeForFuzzyMatch(text: string): string {
  text = text.replaceAll("\r\n", "\n").replaceAll("\r", "\n");
  const lines = text.split("\n").map((line) => line.trimEnd());
  text = lines.join("\n");

  const replacements: Record<string, string> = {
    "\u2018": "'", "\u2019": "'", "\u201A": "'", "\u201B": "'",
    "\u201C": "\"", "\u201D": "\"", "\u201E": "\"", "\u201F": "\"",
    "\u2010": "-", "\u2011": "-", "\u2012": "-", "\u2013": "-", "\u2014": "-", "\u2015": "-", "\u2212": "-",
    "\u00A0": " ", "\u202F": " ", "\u205F": " ", "\u3000": " ",
  };

  let result = "";
  for (let i = 0; i < text.length; i++) {
    const c = text[i];
    const code = text.charCodeAt(i);
    if (replacements[c] !== undefined) {
      result += replacements[c];
    } else if (code >= 8194 && code <= 8202) {
      result += " ";
    } else {
      result += c;
    }
  }
  return result;
}

export interface FuzzyMatchResult {
  found: boolean;
  index: number;
  matchLength: number;
  usedFuzzy: boolean;
  contentForReplacement: string;
}

export function fuzzyFindText(content: string, oldText: string): FuzzyMatchResult {
  const idx = content.indexOf(oldText);
  if (idx !== -1) {
    return {
      found: true,
      index: idx,
      matchLength: oldText.length,
      usedFuzzy: false,
      contentForReplacement: content,
    };
  }

  const fuzzyContent = normalizeForFuzzyMatch(content);
  const fuzzyOldText = normalizeForFuzzyMatch(oldText);
  const fuzzyIdx = fuzzyContent.indexOf(fuzzyOldText);

  if (fuzzyIdx === -1) {
    return {
      found: false,
      index: -1,
      matchLength: 0,
      usedFuzzy: false,
      contentForReplacement: content,
    };
  }

  return {
    found: true,
    index: fuzzyIdx,
    matchLength: fuzzyOldText.length,
    usedFuzzy: true,
    contentForReplacement: fuzzyContent,
  };
}

export function countOccurrences(str: string, substr: string): number {
  if (substr.length === 0) return 0;
  let count = 0;
  let pos = 0;
  while ((pos = str.indexOf(substr, pos)) !== -1) {
    count++;
    pos += substr.length;
  }
  return count;
}

export function fuzzyFindAndReplace(
  content: string,
  oldString: string,
  newString: string,
  replaceAll: boolean
): [string, number, string, Error | null] {
  const fuzzyContent = normalizeForFuzzyMatch(content);
  const fuzzyOld = normalizeForFuzzyMatch(oldString);

  const occurrenceCount = countOccurrences(fuzzyContent, fuzzyOld);
  if (occurrenceCount === 0) {
    let preview = content;
    if (preview.length > 500) {
      preview = preview.slice(0, 500) + "...";
    }
    return [
      content,
      0,
      preview,
      new Error("Could not find the text to replace. The old_string must match (fuzzy matching is applied)."),
    ];
  }

  if (!replaceAll && occurrenceCount > 1) {
    return [
      content,
      occurrenceCount,
      "",
      new Error(`Found ${occurrenceCount} occurrences of the text. The text must be unique unless replace_all=true.`),
    ];
  }

  const match = fuzzyFindText(content, oldString);
  if (!match.found) {
    return [content, 0, "", new Error("Could not find the text to replace.")];
  }

  let base = match.contentForReplacement;
  let matchCount = 0;

  if (replaceAll) {
    const splitStr = match.usedFuzzy ? fuzzyOld : oldString;
    const parts = base.split(splitStr);
    matchCount = parts.length - 1;
    base = parts.join(newString);
  } else {
    base = base.slice(0, match.index) + newString + base.slice(match.index + match.matchLength);
    matchCount = 1;
  }

  if (base === match.contentForReplacement && newString === oldString) {
    return [content, 0, "", new Error("No changes made. The replacement produced identical content.")];
  }

  return [base, matchCount, "", null];
}

function writeTextAtomic(
  filePath: string,
  content: string,
  guardEnabled: boolean,
  skillDir: string
): Effect.Effect<string, Error> {
  const data = Buffer.from(content);
  return atomicWrite(filePath, data).pipe(
    Effect.map(() => SecurityScanSkillDir(skillDir, guardEnabled)),
    Effect.catchAll((err) => Effect.succeed("Failed to write file atomically: " + err.message))
  );
}

function pruneEmptyCategoryDir(skillDir: string): void {
  try {
    const skillsRoot = path.resolve(SkillsDir());
    const parent = path.dirname(skillDir);
    if (path.resolve(parent) === skillsRoot) {
      return;
    }
    if (!fs.existsSync(parent)) {
      return;
    }
    const files = fs.readdirSync(parent);
    if (files.length === 0) {
      fs.rmdirSync(parent);
    }
  } catch {}
}

function invalidateCache(): void {
  ClearSkillsPromptCache();
}

export function createSkill(
  name: string,
  content: string,
  category: string,
  guardEnabled: boolean
): Effect.Effect<SkillManageResult, Error> {
  const nameErr = validateManageName(name);
  if (nameErr !== "") {
    return Effect.succeed({ success: false, error: nameErr });
  }
  const catErr = validateCategory(category);
  if (catErr !== "") {
    return Effect.succeed({ success: false, error: catErr });
  }
  if (content === "") {
    return Effect.succeed({ success: false, error: "content is required for 'create'. Provide the full SKILL.md text (frontmatter + body)." });
  }
  const fmErr = validateFrontmatter(content);
  if (fmErr !== "") {
    return Effect.succeed({ success: false, error: fmErr });
  }
  const sizeErr = validateContentSize(content, "SKILL.md");
  if (sizeErr !== "") {
    return Effect.succeed({ success: false, error: sizeErr });
  }

  const existing = FindSkillDirectory(name);
  if (existing !== "") {
    return Effect.succeed({ success: false, error: `A skill named '${name}' already exists at ${existing}.` });
  }

  const skillDir = resolveSkillDir(name, category);
  const skillMd = path.join(skillDir, "SKILL.md");

  return withFileLock(skillMd, () =>
    Effect.gen(function* () {
      yield* Effect.try({
        try: () => fs.mkdirSync(skillDir, { recursive: true, mode: 0o700 }),
        catch: (cause) => new Error("failed to create directory: " + String(cause)),
      });

      const scanErr = yield* writeTextAtomic(skillMd, content, guardEnabled, skillDir);
      if (scanErr !== "") {
        try {
          fs.rmSync(skillDir, { recursive: true, force: true });
        } catch {}
        return { success: false, error: scanErr };
      }

      invalidateCache();
      let relPath = skillDir;
      try {
        relPath = path.relative(SkillsDir(), skillDir);
      } catch {}
      relPath = relPath.split(path.sep).join("/");
      const result: SkillManageResult = {
        success: true,
        message: `Skill '${name}' created.`,
        path: relPath,
        skill_md: skillMd,
        hint: `To add reference files, templates, or scripts, use skill_manage(action='write_file', name='${name}', file_path='references/example.md', file_content='...')`,
      };
      if (category.trim() !== "") {
        result.category = category.trim();
      }
      return result;
    })
  );
}

export function editSkill(
  name: string,
  content: string,
  guardEnabled: boolean
): Effect.Effect<SkillManageResult, Error> {
  if (content === "") {
    return Effect.succeed({ success: false, error: "content is required for 'edit'. Provide the full updated SKILL.md text." });
  }
  const fmErr = validateFrontmatter(content);
  if (fmErr !== "") {
    return Effect.succeed({ success: false, error: fmErr });
  }
  const sizeErr = validateContentSize(content, "SKILL.md");
  if (sizeErr !== "") {
    return Effect.succeed({ success: false, error: sizeErr });
  }

  const skillDir = FindSkillDirectory(name);
  if (skillDir === "") {
    return Effect.succeed({ success: false, error: skillNotFoundError(name, "") });
  }

  const skillMd = path.join(skillDir, "SKILL.md");
  return withFileLock(skillMd, () =>
    Effect.gen(function* () {
      let originalContent: Buffer | null = null;
      try {
        if (fs.existsSync(skillMd) && fs.statSync(skillMd).isFile()) {
          originalContent = fs.readFileSync(skillMd);
        }
      } catch {}

      const scanErr = yield* writeTextAtomic(skillMd, content, guardEnabled, skillDir);
      if (scanErr !== "") {
        if (originalContent !== null) {
          try {
            yield* atomicWrite(skillMd, originalContent);
          } catch {}
        }
        return { success: false, error: scanErr };
      }
      invalidateCache();
      return {
        success: true,
        message: `Skill '${name}' updated.`,
        path: skillDir,
      };
    })
  );
}

export function patchSkill(
  name: string,
  oldString: string,
  newString: string,
  filePath: string,
  replaceAll: boolean,
  guardEnabled: boolean
): Effect.Effect<SkillManageResult, Error> {
  if (oldString === "") {
    return Effect.succeed({ success: false, error: "old_string is required for 'patch'." });
  }

  const skillDir = FindSkillDirectory(name);
  if (skillDir === "") {
    return Effect.succeed({ success: false, error: skillNotFoundError(name, "") });
  }

  let target = "";
  if (filePath !== "") {
    const pathErr = validateFilePath(filePath);
    if (pathErr !== "") {
      return Effect.succeed({ success: false, error: pathErr });
    }
    const [resTarget, err] = resolveSkillTarget(skillDir, filePath);
    if (err !== "") {
      return Effect.succeed({ success: false, error: err });
    }
    target = resTarget;
  } else {
    target = path.join(skillDir, "SKILL.md");
  }

  if (!fs.existsSync(target)) {
    let rel = target;
    try {
      rel = path.relative(skillDir, target);
    } catch {}
    rel = rel.split(path.sep).join("/");
    return Effect.succeed({ success: false, error: `File not found: ${rel}` });
  }

  return withFileLock(target, () =>
    Effect.gen(function* () {
      let data = "";
      try {
        data = fs.readFileSync(target, "utf8");
      } catch (err: any) {
        return { success: false, error: "Failed to read target file: " + err.message };
      }

      const [newContent, matchCount, filePreview, replaceErr] = fuzzyFindAndReplace(
        data,
        oldString,
        newString,
        replaceAll
      );
      if (replaceErr !== null) {
        const res: SkillManageResult = { success: false, error: replaceErr.message };
        if (filePreview !== "") {
          res.file_preview = filePreview;
        }
        return res;
      }

      const targetLabel = filePath !== "" ? filePath : "SKILL.md";
      const sizeErr = validateContentSize(newContent, targetLabel);
      if (sizeErr !== "") {
        return { success: false, error: sizeErr };
      }

      if (filePath === "") {
        const fmErr = validateFrontmatter(newContent);
        if (fmErr !== "") {
          return { success: false, error: `Patch would break SKILL.md structure: ${fmErr}` };
        }
      }

      const scanErr = yield* writeTextAtomic(target, newContent, guardEnabled, skillDir);
      if (scanErr !== "") {
        return { success: false, error: scanErr };
      }
      invalidateCache();

      const plural = matchCount > 1 ? "s" : "";
      return {
        success: true,
        message: `Patched ${targetLabel} in skill '${name}' (${matchCount} replacement${plural}).`,
      };
    })
  );
}

function pinnedGuard(name: string): string {
  const um = LoadUsage();
  const rec = um[name];
  if (rec && rec.pinned) {
    return `Skill '${name}' is pinned and cannot be deleted by skill_manage. Ask the user to run \`/curator-unpin ${name}\` or \`hollow curator unpin ${name}\` if they want to delete it. Patches and edits are allowed on pinned skills; only deletion is blocked.`;
  }
  return "";
}

export function archiveDeleteSkill(
  name: string,
  absorbedInto: string
): Effect.Effect<SkillManageResult, Error> {
  const lookupName = ResolveSkillLookupName(name);
  if (IsProtectedBuiltin(lookupName)) {
    return Effect.succeed({
      success: false,
      error: `Skill '${name}' is a protected built-in and cannot be archived or deleted by an autonomous pass.`,
    });
  }

  const pinErr = pinnedGuard(name);
  if (pinErr !== "") {
    return Effect.succeed({ success: false, error: pinErr });
  }

  const trimmedAbsorbed = absorbedInto.trim();
  if (trimmedAbsorbed !== "") {
    if (trimmedAbsorbed === name) {
      return Effect.succeed({
        success: false,
        error: `absorbed_into='${trimmedAbsorbed}' cannot equal the skill being deleted.`,
      });
    }
    if (FindSkillDirectory(trimmedAbsorbed) === "") {
      return Effect.succeed({
        success: false,
        error: `absorbed_into='${trimmedAbsorbed}' does not exist. Create or patch the umbrella skill first, then retry the delete.`,
      });
    }
  }

  return ArchiveSkill(name).pipe(
    Effect.map(([ok, msg]) => {
      if (!ok) {
        return { success: false, error: msg };
      }
      if (IsBundledSkillName(name)) {
        MarkSuppressed(name);
      }
      invalidateCache();

      let out = `Skill '${name}' archived (${msg}).`;
      if (trimmedAbsorbed !== "") {
        out += ` Content absorbed into '${trimmedAbsorbed}'.`;
      }
      return { success: true, message: out };
    })
  );
}

export function deleteSkill(
  name: string,
  absorbedInto: string,
  _guardEnabled: boolean
): Effect.Effect<SkillManageResult, Error> {
  const skillDir = FindSkillDirectory(name);
  if (skillDir === "") {
    return Effect.succeed({ success: false, error: skillNotFoundError(name, "") });
  }

  const pinErr = pinnedGuard(name);
  if (pinErr !== "") {
    return Effect.succeed({ success: false, error: pinErr });
  }

  const trimmedAbsorbed = absorbedInto.trim();
  if (trimmedAbsorbed !== "") {
    if (trimmedAbsorbed === name) {
      return Effect.succeed({
        success: false,
        error: `absorbed_into='${trimmedAbsorbed}' cannot equal the skill being deleted.`,
      });
    }
    const target = FindSkillDirectory(trimmedAbsorbed);
    if (target === "") {
      return Effect.succeed({
        success: false,
        error: `absorbed_into='${trimmedAbsorbed}' does not exist. Create or patch the umbrella skill first, then retry the delete.`,
      });
    }
  }

  const skillMd = path.join(skillDir, "SKILL.md");
  return withFileLock(skillMd, () =>
    Effect.gen(function* () {
      try {
        fs.rmSync(skillDir, { recursive: true, force: true });
      } catch (err: any) {
        return { success: false, error: "Failed to delete skill directory: " + err.message };
      }
      pruneEmptyCategoryDir(skillDir);
      invalidateCache();

      let msg = `Skill '${name}' deleted.`;
      if (trimmedAbsorbed !== "") {
        msg += ` Content absorbed into '${trimmedAbsorbed}'.`;
      }
      return { success: true, message: msg };
    })
  );
}

export function writeSkillFile(
  name: string,
  filePath: string,
  fileContent: string,
  guardEnabled: boolean
): Effect.Effect<SkillManageResult, Error> {
  const pathErr = validateFilePath(filePath);
  if (pathErr !== "") {
    return Effect.succeed({ success: false, error: pathErr });
  }

  const contentBytes = Buffer.byteLength(fileContent, "utf8");
  if (contentBytes > MaxSkillFileBytes) {
    return Effect.succeed({
      success: false,
      error: `File content is ${formatNumber(contentBytes)} bytes (limit: ${formatNumber(MaxSkillFileBytes)} bytes / 1 MiB). Consider splitting into smaller files.`,
    });
  }

  const sizeErr = validateContentSize(fileContent, filePath);
  if (sizeErr !== "") {
    return Effect.succeed({ success: false, error: sizeErr });
  }

  const skillDir = FindSkillDirectory(name);
  if (skillDir === "") {
    return Effect.succeed({
      success: false,
      error: skillNotFoundError(name, " Create it first with action='create'."),
    });
  }

  const [target, err] = resolveSkillTarget(skillDir, filePath);
  if (err !== "") {
    return Effect.succeed({ success: false, error: err });
  }

  return withFileLock(target, () =>
    Effect.gen(function* () {
      try {
        const parentDir = path.dirname(target);
        if (!fs.existsSync(parentDir)) {
          fs.mkdirSync(parentDir, { recursive: true, mode: 0o700 });
        }
      } catch {}

      let originalContent: Buffer | null = null;
      try {
        if (fs.existsSync(target) && fs.statSync(target).isFile()) {
          originalContent = fs.readFileSync(target);
        }
      } catch {}

      const scanErr = yield* writeTextAtomic(target, fileContent, guardEnabled, skillDir);
      if (scanErr !== "") {
        if (originalContent !== null) {
          try {
            yield* atomicWrite(target, originalContent);
          } catch {}
        } else {
          try {
            fs.unlinkSync(target);
          } catch {}
        }
        return { success: false, error: scanErr };
      }
      invalidateCache();
      return {
        success: true,
        message: `File '${filePath}' written to skill '${name}'.`,
        path: target,
      };
    })
  );
}

export function removeSkillFile(
  name: string,
  filePath: string
): Effect.Effect<SkillManageResult, Error> {
  const pathErr = validateFilePath(filePath);
  if (pathErr !== "") {
    return Effect.succeed({ success: false, error: pathErr });
  }

  const skillDir = FindSkillDirectory(name);
  if (skillDir === "") {
    return Effect.succeed({ success: false, error: skillNotFoundError(name, "") });
  }

  const [target, err] = resolveSkillTarget(skillDir, filePath);
  if (err !== "") {
    return Effect.succeed({ success: false, error: err });
  }

  if (!fs.existsSync(target)) {
    const available: string[] = [];
    const collectFiles = (dir: string, base: string) => {
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
          collectFiles(full, base);
        } else {
          try {
            const rel = path.relative(base, full);
            available.push(rel.split(path.sep).join("/"));
          } catch {}
        }
      }
    };
    for (const subdir of Object.keys(AllowedSkillSubdirs)) {
      const d = path.join(skillDir, subdir);
      if (fs.existsSync(d)) {
        collectFiles(d, skillDir);
      }
    }
    available.sort();

    return Effect.succeed({
      success: false,
      error: `File '${filePath}' not found in skill '${name}'.`,
      available_files: available,
    });
  }

  return withFileLock(target, () =>
    Effect.gen(function* () {
      try {
        fs.unlinkSync(target);
      } catch (err: any) {
        return { success: false, error: "Failed to remove file: " + err.message };
      }
      const parent = path.dirname(target);
      if (path.resolve(parent) !== path.resolve(skillDir)) {
        try {
          const files = fs.readdirSync(parent);
          if (files.length === 0) {
            fs.rmdirSync(parent);
          }
        } catch {}
      }
      invalidateCache();
      return {
        success: true,
        message: `File '${filePath}' removed from skill '${name}'.`,
      };
    })
  );
}

/*
PORT STATUS
source path: backend/skills/manage_actions.go
source lines: 727
draft lines: 700
confidence: high
status: phase_b_compile
*/
