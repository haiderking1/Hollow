// PORT: backend/skills/sync.go
import fs from "node:fs";
import path from "node:path";
import crypto from "node:crypto";
import { Effect } from "effect";
import { home_dir } from "../hollowhome/home";
import { SkillsDir } from "./paths";
import { ParseFrontmatter } from "./frontmatter";
import { computeSkillCategory } from "./frontmatter";
import { IsSuppressed } from "./curator";
import { atomicWrite } from "./usage";

export interface SyncResult {
  copied: string[];
  updated: string[];
  skipped: number;
  user_modified: string[];
  cleaned: string[];
  suppressed: string[];
  total_bundled: number;
  optional_provenance_backfilled: string[];
  skipped_opt_out?: boolean;
}

export function ReadManifest(): Record<string, string> {
  const manifest: Record<string, string> = {};
  const p = path.join(SkillsDir(), ".bundled_manifest");
  try {
    if (!fs.existsSync(p)) {
      return manifest;
    }
    const data = fs.readFileSync(p, "utf8");
    const lines = data.split("\n");
    for (let line of lines) {
      line = line.trim();
      if (line === "") {
        continue;
      }
      const idx = line.indexOf(":");
      if (idx >= 0) {
        const name = line.slice(0, idx).trim();
        const hash = line.slice(idx + 1).trim();
        manifest[name] = hash;
      } else {
        manifest[line] = "";
      }
    }
  } catch {}
  return manifest;
}

function writeManifest(manifest: Record<string, string>): Effect.Effect<void, Error> {
  const p = path.join(SkillsDir(), ".bundled_manifest");
  const lines: string[] = [];
  for (const [k, v] of Object.entries(manifest)) {
    lines.push(`${k}:${v}`);
  }
  lines.sort();
  const data = Buffer.from(lines.join("\n") + "\n");
  return atomicWrite(p, data);
}

function computeDirHash(dirPath: string): string {
  const h = crypto.createHash("md5");
  const paths: string[] = [];

  const walk = (current: string) => {
    let entries: fs.Dirent[] = [];
    try {
      entries = fs.readdirSync(current, { withFileTypes: true });
    } catch {
      return;
    }
    for (const entry of entries) {
      const full = path.join(current, entry.name);
      let isDir = entry.isDirectory();
      if (entry.isSymbolicLink()) {
        try {
          const stat = fs.statSync(full);
          isDir = stat.isDirectory();
        } catch {}
      }
      if (isDir) {
        walk(full);
      } else {
        paths.push(full);
      }
    }
  };

  if (fs.existsSync(dirPath)) {
    walk(dirPath);
  }
  paths.sort();

  for (const p of paths) {
    try {
      const rel = path.relative(dirPath, p);
      const relSlash = rel.split(path.sep).join("/");
      h.update(relSlash);
      const data = fs.readFileSync(p);
      h.update(data);
    } catch {}
  }
  return h.digest("hex");
}

function readSkillNameFromEmbed(skillMdPath: string, fallback: string): string {
  try {
    const data = fs.readFileSync(skillMdPath, "utf8");
    const [fm] = ParseFrontmatter(data);
    if (fm !== null && typeof fm["name"] === "string" && fm["name"] !== "") {
      return fm["name"];
    }
  } catch {}
  return fallback;
}

function copyEmbeddedDir(srcDir: string, destDir: string): void {
  const walk = (src: string, dest: string) => {
    fs.mkdirSync(dest, { recursive: true, mode: 0o700 });
    const entries = fs.readdirSync(src, { withFileTypes: true });
    for (const entry of entries) {
      const srcPath = path.join(src, entry.name);
      const destPath = path.join(dest, entry.name);
      let isDir = entry.isDirectory();
      if (entry.isSymbolicLink()) {
        try {
          const stat = fs.statSync(srcPath);
          isDir = stat.isDirectory();
        } catch {}
      }
      if (isDir) {
        walk(srcPath, destPath);
      } else {
        const data = fs.readFileSync(srcPath);
        Effect.runSync(atomicWrite(destPath, data));
      }
    }
  };
  walk(srcDir, destDir);
}

function rmtreeWritable(dirPath: string): void {
  const walk = (current: string) => {
    let entries: fs.Dirent[] = [];
    try {
      entries = fs.readdirSync(current, { withFileTypes: true });
    } catch {
      return;
    }
    for (const entry of entries) {
      const full = path.join(current, entry.name);
      let isDir = entry.isDirectory();
      if (entry.isSymbolicLink()) {
        try {
          const stat = fs.statSync(full);
          isDir = stat.isDirectory();
        } catch {}
      }
      try {
        fs.chmodSync(full, 0o777);
      } catch {}
      if (isDir) {
        walk(full);
      }
    }
  };
  if (fs.existsSync(dirPath)) {
    try {
      fs.chmodSync(dirPath, 0o777);
    } catch {}
    walk(dirPath);
    try {
      fs.rmSync(dirPath, { recursive: true, force: true });
    } catch {}
  }
}

interface BundledSkillInfo {
  Name: string;
  SrcDir: string;
  Category: string;
}

export function SyncSkills(quiet: boolean): Effect.Effect<SyncResult, Error> {
  return Effect.try({
    try: () => {
      const home = home_dir();
      if (fs.existsSync(path.join(home, ".no-bundled-skills"))) {
        if (!quiet) {
          console.log("  (skipped — profile opted out of bundled skills via .no-bundled-skills)");
        }
        return {
          copied: [],
          updated: [],
          skipped: 0,
          user_modified: [],
          cleaned: [],
          suppressed: [],
          total_bundled: 0,
          optional_provenance_backfilled: [],
          skipped_opt_out: true,
        };
      }

      const skillsDir = SkillsDir();
      fs.mkdirSync(skillsDir, { recursive: true, mode: 0o700 });

      const manifest = ReadManifest();
      const suppressedSkipped: string[] = [];

      const bundledRoot = path.join(__dirname, "bundled");
      const bundledSkills: BundledSkillInfo[] = [];

      const walkBundled = (dir: string) => {
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
            walkBundled(full);
          } else if (entry.name === "SKILL.md") {
            const srcDir = path.dirname(full);
            const fallback = path.basename(srcDir);
            const name = readSkillNameFromEmbed(full, fallback);
            const cat = computeSkillCategory(full, bundledRoot);
            bundledSkills.push({
              Name: name,
              SrcDir: srcDir,
              Category: cat,
            });
          }
        }
      };

      if (fs.existsSync(bundledRoot)) {
        walkBundled(bundledRoot);
      }

      const copied: string[] = [];
      const updated: string[] = [];
      const userModified: string[] = [];
      let skipped = 0;

      const bundledNames: Record<string, boolean> = {};

      for (const bSkill of bundledSkills) {
        bundledNames[bSkill.Name] = true;

        if (IsSuppressed(bSkill.Name)) {
          suppressedSkipped.push(bSkill.Name);
          continue;
        }

        const destRel = path.relative(bundledRoot, bSkill.SrcDir);
        const dest = path.join(skillsDir, destRel);
        const bundledHash = computeDirHash(bSkill.SrcDir);

        const originHash = manifest[bSkill.Name];
        const inManifest = originHash !== undefined;
        const destExists = fs.existsSync(dest);

        if (!inManifest) {
          if (destExists) {
            const userHash = computeDirHash(dest);
            if (userHash === bundledHash) {
              manifest[bSkill.Name] = bundledHash;
              skipped++;
            } else {
              skipped++;
              if (!quiet) {
                console.warn(`  ⚠ ${bSkill.Name}: bundled version shipped but you already have a local skill by this name — yours was kept. Run \`hollow skills reset ${bSkill.Name}\` to replace it with the bundled version.`);
              }
            }
          } else {
            try {
              copyEmbeddedDir(bSkill.SrcDir, dest);
              copied.push(bSkill.Name);
              manifest[bSkill.Name] = bundledHash;
              if (!quiet) {
                console.log(`  + ${bSkill.Name}`);
              }
            } catch (err: any) {
              if (!quiet) {
                console.error(`  ! Failed to copy ${bSkill.Name}: ${err.message}`);
              }
            }
          }
        } else if (destExists) {
          const userHash = computeDirHash(dest);
          if (originHash === "") {
            // v1 migration
            manifest[bSkill.Name] = userHash;
            skipped++;
            continue;
          }

          if (userHash !== originHash) {
            userModified.push(bSkill.Name);
            if (!quiet) {
              console.log(`  ~ ${bSkill.Name} (user-modified, skipping)`);
            }
            continue;
          }

          if (bundledHash !== originHash) {
            const backup = dest + ".bak";
            rmtreeWritable(backup);
            try {
              fs.renameSync(dest, backup);
              try {
                copyEmbeddedDir(bSkill.SrcDir, dest);
                manifest[bSkill.Name] = bundledHash;
                updated.push(bSkill.Name);
                if (!quiet) {
                  console.log(`  ↑ ${bSkill.Name} (updated)`);
                }
                rmtreeWritable(backup);
              } catch (err: any) {
                try {
                  fs.renameSync(backup, dest);
                } catch {}
                if (!quiet) {
                  console.error(`  ! Failed to update ${bSkill.Name}: ${err.message}`);
                }
              }
            } catch {
              if (!quiet) {
                console.error(`  ! Failed to update ${bSkill.Name}: backup failed`);
              }
            }
          } else {
            skipped++;
          }
        } else {
          // Deleted by user
          skipped++;
        }
      }

      // Clean manifest entries removed from bundled
      const cleaned: string[] = [];
      for (const name of Object.keys(manifest)) {
        if (!bundledNames[name]) {
          cleaned.push(name);
        }
      }
      for (const name of cleaned) {
        delete manifest[name];
      }
      cleaned.sort();

      // Copy category DESCRIPTION.md files
      const walkDescriptions = (dir: string) => {
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
            walkDescriptions(full);
          } else if (entry.name === "DESCRIPTION.md") {
            try {
              const rel = path.relative(bundledRoot, full);
              const destDesc = path.join(skillsDir, rel);
              if (!fs.existsSync(destDesc)) {
                fs.mkdirSync(path.dirname(destDesc), { recursive: true, mode: 0o700 });
                const data = fs.readFileSync(full);
                Effect.runSync(atomicWrite(destDesc, data));
              }
            } catch {}
          }
        }
      };
      if (fs.existsSync(bundledRoot)) {
        walkDescriptions(bundledRoot);
      }

      Effect.runSync(writeManifest(manifest));

      return {
        copied,
        updated,
        skipped,
        user_modified: userModified,
        cleaned,
        suppressed: suppressedSkipped,
        total_bundled: bundledSkills.length,
        optional_provenance_backfilled: [],
      };
    },
    catch: (cause) => (cause instanceof Error ? cause : new Error(String(cause))),
  });
}

export function ResetBundledSkill(
  name: string,
  restore: boolean
): Effect.Effect<[boolean, string, SyncResult | null], Error> {
  return Effect.try({
    try: () => {
      const manifest = ReadManifest();
      const bundledSkillsMap: Record<string, string> = {};

      const bundledRoot = path.join(__dirname, "bundled");
      const walkBundled = (dir: string) => {
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
            walkBundled(full);
          } else if (entry.name === "SKILL.md") {
            const srcDir = path.dirname(full);
            const fallback = path.basename(srcDir);
            const skillName = readSkillNameFromEmbed(full, fallback);
            bundledSkillsMap[skillName] = srcDir;
          }
        }
      };
      if (fs.existsSync(bundledRoot)) {
        walkBundled(bundledRoot);
      }

      const inManifest = manifest[name] !== undefined;
      const srcDir = bundledSkillsMap[name];
      const isBundled = srcDir !== undefined;

      if (!inManifest && !isBundled) {
        return [false, `'${name}' is not a tracked bundled skill. Nothing to reset.`, null] as [boolean, string, SyncResult | null];
      }

      if (restore) {
        if (!isBundled) {
          return [false, `'${name}' has no bundled source — manifest entry preserved but cannot restore from bundled.`, null] as [boolean, string, SyncResult | null];
        }
        const destRel = path.relative(bundledRoot, srcDir);
        const dest = path.join(SkillsDir(), destRel);
        if (fs.existsSync(dest)) {
          rmtreeWritable(dest);
        }
      }

      if (inManifest) {
        delete manifest[name];
        Effect.runSync(writeManifest(manifest));
      }

      const synced = Effect.runSync(SyncSkills(true));
      let message = `Cleared manifest entry for '${name}'. Future updates will re-baseline against your copy.`;
      if (restore) {
        message = `Restored '${name}' from bundled source.`;
      }
      return [true, message, synced] as [boolean, string, SyncResult | null];
    },
    catch: (cause) => (cause instanceof Error ? cause : new Error(String(cause))),
  });
}

/*
PORT STATUS
source path: backend/skills/sync.go
source lines: 430
draft lines: 400
confidence: high
status: phase_b_compile
*/
