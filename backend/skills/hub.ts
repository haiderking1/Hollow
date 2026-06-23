// PORT: backend/skills/hub.go

import fs from "node:fs";
import path from "node:path";
import crypto from "node:crypto";
import dns from "node:dns/promises";
import { execSync } from "node:child_process";
import readline from "node:readline";
import { Effect } from "effect";

import { ParseFrontmatter, extractSkillDescription, extractSkillTags } from "./frontmatter";
import { ScanSkill, shouldAllowInstall } from "./guard";
import { atomicWrite } from "./usage";
import { ClearSkillsPromptCache } from "./prompt_index";
import { SkillsDir } from "./paths";
import { type SkillScanResult } from "./types";
import {
  HermesIndexSource,
  SkillsShSource,
  WellKnownSkillSource,
  UrlSource,
  GitHubSource,
  ClawHubSource,
  LobeHubSource,
  BrowseShSource
} from "./hub_adapters";

// ---------------------------------------------------------------------------
// Paths & Constants
// ---------------------------------------------------------------------------

export function HubDir(): string {
  return path.join(SkillsDir(), ".hub");
}

export function LockFilePath(): string {
  return path.join(HubDir(), "lock.json");
}

export function QuarantineDir(): string {
  return path.join(HubDir(), "quarantine");
}

export function AuditLogPath(): string {
  return path.join(HubDir(), "audit.log");
}

export function TapsFilePath(): string {
  return path.join(HubDir(), "taps.json");
}

export function IndexCacheDir(): string {
  return path.join(HubDir(), "index-cache");
}

export const IndexCacheTTL = 3600; // 1 hour

// ---------------------------------------------------------------------------
// Data Models
// ---------------------------------------------------------------------------

export interface SkillMeta {
  name: string;
  description: string;
  source: string;
  identifier: string;
  trust_level: string;
  repo?: string;
  path?: string;
  tags: string[];
  extra?: Record<string, any>;
}

export interface SkillBundle {
  name: string;
  files: Record<string, Uint8Array>;
  source: string;
  identifier: string;
  trust_level: string;
  metadata?: Record<string, any>;
}

export interface SkillSource {
  Search(query: string, limit: number): Effect.Effect<SkillMeta[], Error>;
  Fetch(identifier: string): Effect.Effect<SkillBundle, Error>;
  Inspect(identifier: string): Effect.Effect<SkillMeta, Error>;
  SourceID(): string;
  TrustLevelFor(identifier: string): string;
}

export class OptionalSkillSource implements SkillSource {
  SourceID(): string {
    return "official";
  }

  TrustLevelFor(identifier: string): string {
    return "builtin";
  }

  Search(query: string, limit: number): Effect.Effect<SkillMeta[], Error> {
    return Effect.try({
      try: () => {
        const results: SkillMeta[] = [];
        const queryLower = query.toLowerCase();
        const optionalRoot = path.join(__dirname, "optional");

        if (fs.existsSync(optionalRoot)) {
          const walkDirSync = (dir: string) => {
            let entries: fs.Dirent[];
            try {
              entries = fs.readdirSync(dir, { withFileTypes: true });
            } catch {
              return;
            }
            for (const entry of entries) {
              const fullPath = path.join(dir, entry.name);
              if (entry.isDirectory()) {
                walkDirSync(fullPath);
              } else if (entry.isFile() && entry.name === "SKILL.md") {
                try {
                  const data = fs.readFileSync(fullPath, "utf8");
                  const [fm] = ParseFrontmatter(data);
                  if (fm) {
                    let name = fm.name || "";
                    if (!name) {
                      name = path.basename(path.dirname(fullPath));
                    }
                    const desc = extractSkillDescription(fm);
                    const tags = extractSkillTags(fm);

                    const searchable = `${name} ${desc} ${tags.join(" ")}`.toLowerCase();
                    if (searchable.includes(queryLower)) {
                      const relPath = path.relative(optionalRoot, path.dirname(fullPath)).split(path.sep).join("/");
                      results.push({
                        name,
                        description: desc,
                        source: "official",
                        identifier: "official/" + relPath,
                        trust_level: "builtin",
                        path: relPath,
                        tags,
                        extra: {}
                      });
                    }
                  }
                } catch {
                  // ignore
                }
              }
            }
          };
          walkDirSync(optionalRoot);
        }

        if (limit > 0 && results.length > limit) {
          return results.slice(0, limit);
        }
        return results;
      },
      catch: (err) => err instanceof Error ? err : new Error(String(err))
    });
  }

  Fetch(identifier: string): Effect.Effect<SkillBundle, Error> {
    return Effect.try({
      try: () => {
        const rel = identifier.startsWith("official/") ? identifier.substring(9) : identifier;
        const skillDir = path.join(__dirname, "optional", rel);
        if (!fs.existsSync(skillDir) || !fs.statSync(skillDir).isDirectory()) {
          throw new Error(`optional skill not found: ${identifier}`);
        }
        const files: Record<string, Uint8Array> = {};
        const walk = (dir: string) => {
          const entries = fs.readdirSync(dir, { withFileTypes: true });
          for (const entry of entries) {
            const fullPath = path.join(dir, entry.name);
            if (entry.isDirectory()) {
              walk(fullPath);
            } else if (entry.isFile()) {
              const data = fs.readFileSync(fullPath);
              const relPath = path.relative(skillDir, fullPath).split(path.sep).join("/");
              files[relPath] = data;
            }
          }
        };
        walk(skillDir);
        const name = path.basename(skillDir);
        return {
          name,
          files,
          source: "official",
          identifier,
          trust_level: "builtin"
        };
      },
      catch: (err) => err instanceof Error ? err : new Error(String(err))
    });
  }

  Inspect(identifier: string): Effect.Effect<SkillMeta, Error> {
    return Effect.try({
      try: () => {
        const rel = identifier.startsWith("official/") ? identifier.substring(9) : identifier;
        const skillMdPath = path.join(__dirname, "optional", rel, "SKILL.md");
        if (!fs.existsSync(skillMdPath)) {
          throw new Error(`SKILL.md not found for: ${identifier}`);
        }
        const data = fs.readFileSync(skillMdPath, "utf8");
        const [fm] = ParseFrontmatter(data);
        if (!fm) {
          throw new Error(`invalid frontmatter in optional skill SKILL.md`);
        }
        let name = fm.name || "";
        if (!name) {
          name = path.basename(path.dirname(skillMdPath));
        }
        const desc = extractSkillDescription(fm);
        const tags = extractSkillTags(fm);
        return {
          name,
          description: desc,
          source: "official",
          identifier,
          trust_level: "builtin",
          path: rel,
          tags,
          extra: {}
        };
      },
      catch: (err) => err instanceof Error ? err : new Error(String(err))
    });
  }
}

// ---------------------------------------------------------------------------
// Path validation and Traversal protection
// ---------------------------------------------------------------------------

export function normalizeBundlePath(val: string, fieldName: string, allowNested: boolean): Effect.Effect<string, Error> {
  return Effect.try({
    try: () => {
      const raw = val.trim();
      if (raw === "") {
        throw new Error(`unsafe ${fieldName}: empty path`);
      }
      const normalized = raw.replace(/\\/g, "/");
      const parts = normalized.split("/");
      const cleanParts: string[] = [];
      for (const p of parts) {
        if (p === "" || p === ".") {
          continue;
        }
        if (p === "..") {
          throw new Error(`unsafe ${fieldName}: contains traversal segment: ${val}`);
        }
        cleanParts.push(p);
      }
      if (cleanParts.length === 0) {
        throw new Error(`unsafe ${fieldName}: empty path`);
      }
      if (normalized.startsWith("/") || path.isAbsolute(raw)) {
        throw new Error(`unsafe ${fieldName}: absolute path not allowed: ${val}`);
      }
      if (cleanParts[0].length === 2 && cleanParts[0][1] === ":") {
        throw new Error(`unsafe ${fieldName}: drive letter check failed: ${val}`);
      }
      if (!allowNested && cleanParts.length !== 1) {
        throw new Error(`unsafe ${fieldName}: nested path not allowed: ${val}`);
      }
      return cleanParts.join("/");
    },
    catch: (err) => err instanceof Error ? err : new Error(String(err))
  });
}

export function validateSkillName(name: string): Effect.Effect<string, Error> {
  return normalizeBundlePath(name, "skill name", false);
}

export function validateInstallParentPath(category: string): Effect.Effect<string, Error> {
  return normalizeBundlePath(category, "install parent path", true);
}

export function normalizeLockInstallPath(installPath: string, skillName: string): Effect.Effect<string, Error> {
  return Effect.gen(function* () {
    const safeSkillName = yield* validateSkillName(skillName);
    const normalized = yield* normalizeBundlePath(installPath, "install path", true);
    const parts = normalized.split("/");
    if (parts.length === 0 || parts[parts.length - 1] !== safeSkillName) {
      return yield* Effect.fail(new Error(`unsafe install path: final component must match skill name "${safeSkillName}", got: ${installPath}`));
    }
    return normalized;
  });
}

export function isPathRedirect(filePath: string): boolean {
  try {
    const stat = fs.lstatSync(filePath);
    return stat.isSymbolicLink();
  } catch {
    return false;
  }
}

export function resolveLockInstallPath(installPath: string, skillName: string): Effect.Effect<string, Error> {
  return Effect.gen(function* () {
    const normalized = yield* normalizeLockInstallPath(installPath, skillName);
    const skillsRoot = path.resolve(SkillsDir());
    let target = skillsRoot;
    const parts = normalized.split("/");
    for (const part of parts) {
      target = path.join(target, part);
      if (isPathRedirect(target)) {
        return yield* Effect.fail(new Error(`unsafe install path: path contains symlink redirect: ${target}`));
      }
    }
    const resolved = path.resolve(target);
    if (resolved === skillsRoot) {
      return yield* Effect.fail(new Error(`unsafe install path resolved to skills root: ${installPath}`));
    }
    if (!resolved.startsWith(skillsRoot + path.sep)) {
      return yield* Effect.fail(new Error(`unsafe install path: escapes skills root: ${installPath}`));
    }
    return resolved;
  });
}

// ---------------------------------------------------------------------------
// SSRF & URL Safety
// ---------------------------------------------------------------------------

const blockedHostnames = new Set([
  "metadata.google.internal",
  "metadata.goog"
]);

const alwaysBlockedIPv4Set = new Set([
  "169.254.169.254",
  "169.254.170.2",
  "169.254.169.253",
  "100.100.100.200"
]);

function getAllowPrivate(): boolean {
  let envVal = (process.env.HOLLOW_ALLOW_PRIVATE_URLS || "").trim().toLowerCase();
  if (envVal === "") {
    envVal = (process.env.HERMES_ALLOW_PRIVATE_URLS || "").trim().toLowerCase();
  }
  return envVal === "true" || envVal === "1" || envVal === "yes";
}

function getIPv4(ip: string): string | null {
  const clean = ip.trim().toLowerCase();
  if (clean.startsWith("::ffff:")) {
    const sub = clean.substring(7);
    if (parseIPv4(sub) !== null) {
      return sub;
    }
  }
  if (parseIPv4(clean) !== null) {
    return clean;
  }
  return null;
}

function parseIPv4(ip: string): number | null {
  const parts = ip.split(".");
  if (parts.length !== 4) return null;
  const octets = [];
  for (const part of parts) {
    const o = parseInt(part, 10);
    if (isNaN(o) || o < 0 || o > 255 || String(o) !== part) return null;
    octets.push(o);
  }
  return ((octets[0] << 24) | (octets[1] << 16) | (octets[2] << 8) | octets[3]) >>> 0;
}

function isAlwaysBlockedIPv4(ipv4: string): boolean {
  if (alwaysBlockedIPv4Set.has(ipv4)) return true;
  // check linkLocalNet: 169.254.0.0/16
  const ipVal = parseIPv4(ipv4);
  if (ipVal !== null && (ipVal & 0xFFFF0000) === 0xA9FE0000) {
    return true;
  }
  return false;
}

function isAlwaysBlockedIPv6(ipv6: string): boolean {
  return ipv6 === "fd00:ec2::254";
}

function isBlockedIPv4(ipv4: string): boolean {
  const ipVal = parseIPv4(ipv4);
  if (ipVal === null) return true;
  // IsLoopback: 127.0.0.0/8
  if ((ipVal & 0xFF000000) === 0x7F000000) return true;
  // IsMulticast: 224.0.0.0/4
  if ((ipVal & 0xF0000000) === 0xE0000000) return true;
  // IsUnspecified: 0.0.0.0
  if (ipVal === 0) return true;
  // IsPrivate: 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16
  if ((ipVal & 0xFF000000) === 0x0A000000) return true;
  if ((ipVal & 0xFFF00000) === 0xAC100000) return true;
  if ((ipVal & 0xFFFF0000) === 0xC0A80000) return true;
  // CGNAT: 100.64.0.0/10
  if ((ipVal & 0xFFC00000) === 0x64400000) return true;
  return false;
}

function normalizeIPv6(ip: string): string {
  let clean = ip.trim().toLowerCase();
  if (clean.startsWith("[") && clean.endsWith("]")) {
    clean = clean.slice(1, -1);
  }
  return clean;
}

function isBlockedIPv6(ipv6: string): boolean {
  // IsLoopback: ::1
  if (ipv6 === "::1" || ipv6 === "0:0:0:0:0:0:0:1") return true;
  // IsUnspecified: ::
  if (ipv6 === "::" || ipv6 === "0:0:0:0:0:0:0:0") return true;
  // IsMulticast: ff00::/8
  if (ipv6.startsWith("ff")) return true;
  // IsPrivate: fc00::/7 (starts with fc or fd)
  if (/^f[cd]/i.test(ipv6)) return true;
  return false;
}

export function isSafeURL(urlStr: string): Effect.Effect<boolean, Error> {
  return Effect.tryPromise({
    try: async () => {
      let u: URL;
      try {
        u = new URL(urlStr);
      } catch {
        return false;
      }
      const scheme = u.protocol.toLowerCase();
      if (scheme !== "http:" && scheme !== "https:") {
        return false;
      }
      const host = u.hostname.toLowerCase();
      if (host === "") {
        return false;
      }
      if (blockedHostnames.has(host)) {
        return false;
      }

      const allowPrivate = getAllowPrivate();

      const ipv4 = getIPv4(host);
      if (ipv4 !== null) {
        if (isAlwaysBlockedIPv4(ipv4)) return false;
        if (!allowPrivate && isBlockedIPv4(ipv4)) return false;
        return true;
      }
      
      if (host.includes(":")) {
        const normalized = normalizeIPv6(host);
        if (isAlwaysBlockedIPv6(normalized)) return false;
        if (!allowPrivate && isBlockedIPv6(normalized)) return false;
        return true;
      }

      let ips: { address: string; family: number }[];
      try {
        ips = await dns.lookup(host, { all: true });
      } catch {
        return false;
      }

      for (const ipInfo of ips) {
        const ip = ipInfo.address;
        const mappedIpv4 = getIPv4(ip);
        if (mappedIpv4 !== null) {
          if (isAlwaysBlockedIPv4(mappedIpv4)) return false;
          if (!allowPrivate && isBlockedIPv4(mappedIpv4)) return false;
        } else {
          const normalized = normalizeIPv6(ip);
          if (isAlwaysBlockedIPv6(normalized)) return false;
          if (!allowPrivate && isBlockedIPv6(normalized)) return false;
        }
      }

      return true;
    },
    catch: (err) => err instanceof Error ? err : new Error(String(err))
  });
}

// ---------------------------------------------------------------------------
// HTTP request helper with SSRF validation
// ---------------------------------------------------------------------------

export function guardedHttpGet(
  urlStr: string,
  headers?: Record<string, string>
): Effect.Effect<{ body: Buffer; status: number }, Error> {
  return Effect.gen(function* () {
    let currentURL = urlStr;
    let redirects = 0;
    const resolvedHeaders = { ...headers };
    if (!resolvedHeaders["User-Agent"]) {
      resolvedHeaders["User-Agent"] = "Hollow/1.0 (+https://github.com/haiderking1/Hollow)";
    }

    while (true) {
      const isSafe = yield* isSafeURL(currentURL);
      if (!isSafe) {
        return yield* Effect.fail(new Error(`blocked unsafe URL: ${currentURL}`));
      }

      const response = yield* Effect.tryPromise({
        try: () => fetch(currentURL, {
          headers: resolvedHeaders,
          redirect: "manual",
          signal: AbortSignal.timeout(20000)
        }),
        catch: (err) => err instanceof Error ? err : new Error(String(err))
      });

      if (response.status >= 300 && response.status < 400) {
        redirects++;
        if (redirects >= 5) {
          return yield* Effect.fail(new Error("stopped after 5 redirects"));
        }
        const loc = response.headers.get("location");
        if (!loc) {
          break;
        }
        currentURL = new URL(loc, currentURL).toString();
        continue;
      }

      const arrayBuffer = yield* Effect.tryPromise({
        try: () => response.arrayBuffer(),
        catch: (err) => err instanceof Error ? err : new Error(String(err))
      });

      return {
        body: Buffer.from(arrayBuffer),
        status: response.status
      };
    }

    return yield* Effect.fail(new Error(`failed to fetch ${urlStr}`));
  });
}

// ---------------------------------------------------------------------------
// GitHub Auth
// ---------------------------------------------------------------------------

export class GitHubAuth {
  token: string;
  authMethod: string;

  constructor(token: string, authMethod: string) {
    this.token = token;
    this.authMethod = authMethod;
  }

  GetHeaders(): Record<string, string> {
    const headers: Record<string, string> = {
      "Accept": "application/vnd.github.v3+json",
    };
    if (this.token !== "") {
      headers["Authorization"] = "token " + this.token;
    }
    return headers;
  }
}

export function ResolveGitHubAuth(): GitHubAuth {
  let token = process.env.GITHUB_TOKEN || process.env.GH_TOKEN || "";
  if (token !== "") {
    return new GitHubAuth(token, "pat");
  }
  token = tryGhCli();
  if (token !== "") {
    return new GitHubAuth(token, "gh-cli");
  }
  return new GitHubAuth("", "anonymous");
}

function tryGhCli(): string {
  try {
    const stdout = execSync("gh auth token", { stdio: ["ignore", "pipe", "ignore"], encoding: "utf8" });
    return stdout.trim();
  } catch {
    return "";
  }
}

// ---------------------------------------------------------------------------
// Lock File Manager
// ---------------------------------------------------------------------------

export interface InstalledSkill {
  source: string;
  identifier: string;
  trust_level: string;
  scan_verdict: string;
  content_hash: string;
  install_path: string;
  files: string[];
  metadata?: Record<string, any>;
  installed_at: string;
  updated_at: string;
}

export class HubLockFile {
  version: number = 1;
  installed: Record<string, InstalledSkill> = {};

  static Load(): HubLockFile {
    const lock = new HubLockFile();
    const p = LockFilePath();
    try {
      if (fs.existsSync(p)) {
        const data = fs.readFileSync(p, "utf8");
        const obj = JSON.parse(data);
        if (obj) {
          lock.version = obj.version || 1;
          lock.installed = obj.installed || {};
        }
      }
    } catch {
      // ignore
    }
    return lock;
  }

  static Save(lock: HubLockFile): Effect.Effect<void, Error> {
    const p = LockFilePath();
    return Effect.gen(function* () {
      const dir = path.dirname(p);
      if (!fs.existsSync(dir)) {
        fs.mkdirSync(dir, { recursive: true, mode: 0o700 });
      }
      const dataBytes = Buffer.from(JSON.stringify(lock, null, "  ") + "\n");
      yield* atomicWrite(p, dataBytes);
    });
  }

  RecordInstall(
    name: string,
    source: string,
    identifier: string,
    trustLevel: string,
    scanVerdict: string,
    skillHash: string,
    installPath: string,
    files: string[],
    metadata?: Record<string, any>
  ): Effect.Effect<void, Error> {
    return Effect.gen(function* () {
      const safeName = yield* validateSkillName(name);
      const safeInstallPath = yield* normalizeLockInstallPath(installPath, safeName);
      
      const lock = HubLockFile.Load();
      const now = new Date().toISOString();
      let installedAt = now;
      const existing = lock.installed[safeName];
      if (existing) {
        installedAt = existing.installed_at;
      }

      lock.installed[safeName] = {
        source,
        identifier,
        trust_level: trustLevel,
        scan_verdict: scanVerdict,
        content_hash: skillHash,
        install_path: safeInstallPath,
        files,
        metadata,
        installed_at: installedAt,
        updated_at: now
      };
      yield* HubLockFile.Save(lock);
    });
  }

  RecordUninstall(name: string): Effect.Effect<void, Error> {
    return Effect.gen(function* () {
      const lock = HubLockFile.Load();
      delete lock.installed[name];
      yield* HubLockFile.Save(lock);
    });
  }

  GetInstalled(name: string): InstalledSkill | null {
    const lock = HubLockFile.Load();
    return lock.installed[name] || null;
  }

  ListInstalled(): InstalledSkill[] {
    const lock = HubLockFile.Load();
    const out: InstalledSkill[] = [];
    for (const [name, entry] of Object.entries(lock.installed)) {
      const metadata = { ...(entry.metadata || {}), name };
      out.push({
        ...entry,
        metadata
      });
    }
    return out;
  }
}

export function LoadLockFile(): HubLockFile {
  return HubLockFile.Load();
}

export function SaveLockFile(lock: HubLockFile): Effect.Effect<void, Error> {
  return HubLockFile.Save(lock);
}

// ---------------------------------------------------------------------------
// Taps Manager
// ---------------------------------------------------------------------------

export interface TapEntry {
  repo: string;
  path: string;
}

export function LoadTaps(): TapEntry[] {
  const p = TapsFilePath();
  try {
    if (fs.existsSync(p)) {
      const data = fs.readFileSync(p, "utf8");
      const obj = JSON.parse(data);
      if (obj && Array.isArray(obj.taps)) {
        return obj.taps;
      }
    }
  } catch {
    // ignore
  }
  return [];
}

export function SaveTaps(taps: TapEntry[]): Effect.Effect<void, Error> {
  const p = TapsFilePath();
  return Effect.gen(function* () {
    const dir = path.dirname(p);
    if (!fs.existsSync(dir)) {
      fs.mkdirSync(dir, { recursive: true, mode: 0o700 });
    }
    const dataBytes = Buffer.from(JSON.stringify({ taps }, null, "  ") + "\n");
    yield* atomicWrite(p, dataBytes);
  });
}

export function AddTap(repo: string, pathVal: string): Effect.Effect<boolean, Error> {
  return Effect.gen(function* () {
    let pVal = pathVal;
    if (pVal === "") {
      pVal = "skills/";
    }
    const taps = LoadTaps();
    for (const t of taps) {
      if (t.repo === repo) {
        return false;
      }
    }
    taps.push({ repo, path: pVal });
    yield* SaveTaps(taps);
    return true;
  });
}

export function RemoveTap(repo: string): Effect.Effect<boolean, Error> {
  return Effect.gen(function* () {
    const taps = LoadTaps();
    const newTaps: TapEntry[] = [];
    let found = false;
    for (const t of taps) {
      if (t.repo === repo) {
        found = true;
        continue;
      }
      newTaps.push(t);
    }
    if (!found) {
      return false;
    }
    yield* SaveTaps(newTaps);
    return true;
  });
}

// ---------------------------------------------------------------------------
// Audit Logging
// ---------------------------------------------------------------------------

export function appendAuditLog(
  action: string,
  skillName: string,
  source: string,
  trustLevel: string,
  verdict: string,
  extra: string
): void {
  try {
    const p = AuditLogPath();
    const dir = path.dirname(p);
    if (!fs.existsSync(dir)) {
      fs.mkdirSync(dir, { recursive: true, mode: 0o700 });
    }
    const timestamp = new Date().toISOString();
    let line = `${timestamp} ${action} ${skillName} ${source}:${trustLevel} ${verdict}`;
    if (extra !== "") {
      line += " " + extra;
    }
    line += "\n";
    fs.appendFileSync(p, line, { mode: 0o600 });
  } catch {
    // ignore
  }
}

// ---------------------------------------------------------------------------
// Index Cache
// ---------------------------------------------------------------------------

export function readIndexCache(key: string): [Buffer | null, boolean] {
  const p = path.join(IndexCacheDir(), key + ".json");
  try {
    const stat = fs.statSync(p);
    const ageSeconds = (Date.now() - stat.mtimeMs) / 1000;
    if (ageSeconds > IndexCacheTTL) {
      return [null, false];
    }
    return [fs.readFileSync(p), true];
  } catch {
    return [null, false];
  }
}

export function writeIndexCache(key: string, data: Uint8Array): void {
  try {
    const cacheDir = IndexCacheDir();
    if (!fs.existsSync(cacheDir)) {
      fs.mkdirSync(cacheDir, { recursive: true, mode: 0o700 });
    }
    const ignoreFile = path.join(HubDir(), ".ignore");
    if (!fs.existsSync(ignoreFile)) {
      fs.writeFileSync(ignoreFile, "# Exclude hub internals from search tools\n*\n", { mode: 0o600 });
    }
    const p = path.join(cacheDir, key + ".json");
    Effect.runSync(atomicWrite(p, data));
  } catch {
    // ignore
  }
}

// ---------------------------------------------------------------------------
// Hub Operations (quarantine, install, uninstall)
// ---------------------------------------------------------------------------

export function ensureHubDirs(): void {
  try {
    fs.mkdirSync(HubDir(), { recursive: true, mode: 0o700 });
    fs.mkdirSync(QuarantineDir(), { recursive: true, mode: 0o700 });
    fs.mkdirSync(IndexCacheDir(), { recursive: true, mode: 0o700 });

    const lockPath = LockFilePath();
    if (!fs.existsSync(lockPath)) {
      fs.writeFileSync(lockPath, JSON.stringify({ version: 1, installed: {} }, null, "  ") + "\n", { mode: 0o600 });
    }

    const auditPath = AuditLogPath();
    if (!fs.existsSync(auditPath)) {
      fs.writeFileSync(auditPath, "", { mode: 0o600 });
    }

    const tapsPath = TapsFilePath();
    if (!fs.existsSync(tapsPath)) {
      fs.writeFileSync(tapsPath, JSON.stringify({ taps: [] }, null, "  ") + "\n", { mode: 0o600 });
    }
  } catch {
    // ignore
  }
}

export function quarantineBundle(bundle: SkillBundle): Effect.Effect<string, Error> {
  return Effect.gen(function* () {
    ensureHubDirs();
    const safeName = yield* validateSkillName(bundle.name);
    const dest = path.join(QuarantineDir(), safeName);
    try {
      if (fs.existsSync(dest)) {
        fs.rmSync(dest, { recursive: true, force: true });
      }
      fs.mkdirSync(dest, { recursive: true, mode: 0o700 });
    } catch (err) {
      return yield* Effect.fail(err instanceof Error ? err : new Error(String(err)));
    }

    for (const [relPath, content] of Object.entries(bundle.files)) {
      const safeRel = yield* normalizeBundlePath(relPath, "bundle file path", true);
      const fileDest = path.join(dest, safeRel.split("/").join(path.sep));
      try {
        const dir = path.dirname(fileDest);
        if (!fs.existsSync(dir)) {
          fs.mkdirSync(dir, { recursive: true, mode: 0o700 });
        }
      } catch (err) {
        try { fs.rmSync(dest, { recursive: true, force: true }); } catch {}
        return yield* Effect.fail(err instanceof Error ? err : new Error(String(err)));
      }
      yield* atomicWrite(fileDest, content);
    }
    return dest;
  });
}

export function copyDir(src: string, dst: string): Effect.Effect<void, Error> {
  return Effect.try({
    try: () => {
      const walk = (currentSrc: string, currentDst: string) => {
        const stat = fs.statSync(currentSrc);
        if (stat.isDirectory()) {
          fs.mkdirSync(currentDst, { recursive: true, mode: stat.mode });
          const entries = fs.readdirSync(currentSrc);
          for (const entry of entries) {
            walk(path.join(currentSrc, entry), path.join(currentDst, entry));
          }
        } else {
          const dir = path.dirname(currentDst);
          if (!fs.existsSync(dir)) {
            fs.mkdirSync(dir, { recursive: true, mode: 0o700 });
          }
          const data = fs.readFileSync(currentSrc);
          fs.writeFileSync(currentDst, data, { mode: stat.mode });
        }
      };
      walk(src, dst);
    },
    catch: (err) => err instanceof Error ? err : new Error(String(err))
  });
}

export function computeDirHashOnDisk(dirPath: string): string {
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

export function installFromQuarantine(
  quarantinePath: string,
  skillName: string,
  category: string,
  bundle: SkillBundle,
  scanResult: SkillScanResult
): Effect.Effect<string, Error> {
  return Effect.gen(function* () {
    const safeSkillName = yield* validateSkillName(skillName);
    let safeCategory = "";
    if (category !== "") {
      safeCategory = yield* validateInstallParentPath(category);
    }

    const qResolved = path.resolve(quarantinePath);
    const qRoot = path.resolve(QuarantineDir());
    if (!qResolved.startsWith(qRoot + path.sep)) {
      return yield* Effect.fail(new Error(`unsafe quarantine path: ${quarantinePath}`));
    }

    let installRelPath = safeSkillName;
    if (safeCategory !== "") {
      installRelPath = safeCategory + "/" + safeSkillName;
    }

    const installDir = yield* resolveLockInstallPath(installRelPath, safeSkillName);

    try {
      if (fs.existsSync(installDir)) {
        fs.rmSync(installDir, { recursive: true, force: true });
      }
    } catch {}

    const checkSymlinks = (dir: string): void => {
      const entries = fs.readdirSync(dir, { withFileTypes: true });
      for (const entry of entries) {
        const fullPath = path.join(dir, entry.name);
        if (entry.isSymbolicLink()) {
          const rel = path.relative(qResolved, fullPath).split(path.sep).join("/");
          throw new Error(`installed skill contains symlinks, which is not allowed: ${rel}`);
        }
        if (entry.isDirectory()) {
          checkSymlinks(fullPath);
        }
      }
    };

    try {
      checkSymlinks(qResolved);
    } catch (err) {
      return yield* Effect.fail(err instanceof Error ? err : new Error(String(err)));
    }

    try {
      const parentDir = path.dirname(installDir);
      if (!fs.existsSync(parentDir)) {
        fs.mkdirSync(parentDir, { recursive: true, mode: 0o700 });
      }
    } catch (err) {
      return yield* Effect.fail(err instanceof Error ? err : new Error(String(err)));
    }

    try {
      fs.renameSync(quarantinePath, installDir);
    } catch {
      yield* copyDir(quarantinePath, installDir);
      try {
        fs.rmSync(quarantinePath, { recursive: true, force: true });
      } catch {}
    }

    const fileList = Object.keys(bundle.files);
    fileList.sort();

    const lock = new HubLockFile();
    yield* lock.RecordInstall(
      safeSkillName,
      bundle.source,
      bundle.identifier,
      bundle.trust_level,
      scanResult.verdict,
      computeDirHashOnDisk(installDir),
      path.relative(SkillsDir(), installDir).split(path.sep).join("/"),
      fileList,
      bundle.metadata
    );

    appendAuditLog(
      "INSTALL",
      safeSkillName,
      bundle.source,
      bundle.trust_level,
      scanResult.verdict,
      computeDirHashOnDisk(installDir)
    );

    return installDir;
  });
}

export function UninstallSkill(skillName: string): Effect.Effect<[boolean, string], Error> {
  return Effect.gen(function* () {
    const lock = new HubLockFile();
    const entry = lock.GetInstalled(skillName);
    if (!entry) {
      return [false, `'${skillName}' is not a hub-installed skill (may be a builtin)`] as [boolean, string];
    }

    const installPath = yield* resolveLockInstallPath(entry.install_path, skillName);
    try {
      if (fs.existsSync(installPath)) {
        fs.rmSync(installPath, { recursive: true, force: true });
      }
    } catch {}

    yield* lock.RecordUninstall(skillName);
    appendAuditLog("UNINSTALL", skillName, entry.source, entry.trust_level, "n/a", "user_request");

    return [true, `Uninstalled '${skillName}' from ${entry.install_path}`] as [boolean, string];
  });
}

export function bundleContentHash(bundle: SkillBundle): string {
  const h = crypto.createHash("sha256");
  const keys = Object.keys(bundle.files);
  keys.sort();
  for (const k of keys) {
    h.update(Buffer.from(k, "utf8"));
    h.update(Buffer.from([0]));
    h.update(bundle.files[k]);
  }
  return `sha256:${h.digest("hex")}`.substring(0, 23);
}

// ---------------------------------------------------------------------------
// General Source Router
// ---------------------------------------------------------------------------

export function CreateSourceRouter(auth?: GitHubAuth): SkillSource[] {
  const g = auth || ResolveGitHubAuth();
  return [
    new OptionalSkillSource(),
    new HermesIndexSource(g),
    new SkillsShSource(g),
    new WellKnownSkillSource(),
    new UrlSource(),
    new GitHubSource(g),
    new ClawHubSource(),
    new LobeHubSource(),
    new BrowseShSource()
  ];
}

// ---------------------------------------------------------------------------
// Unified Search
// ---------------------------------------------------------------------------

export function UnifiedSearch(
  query: string,
  sources: SkillSource[],
  sourceFilter: string,
  limit: number
): Effect.Effect<SkillMeta[], Error> {
  return Effect.gen(function* () {
    const active: SkillSource[] = [];
    for (const src of sources) {
      const sid = src.SourceID();
      if (sourceFilter !== "all" && sid !== sourceFilter && sid !== "official") {
        continue;
      }
      active.push(src);
    }

    const searchEffects = active.map(src => src.Search(query, limit).pipe(
      Effect.orElseSucceed(() => [] as SkillMeta[])
    ));

    const resultsArray = yield* Effect.all(searchEffects, { concurrency: "unbounded" });
    const all = resultsArray.flat();

    const trustRank: Record<string, number> = {
      "builtin": 2,
      "trusted": 1,
      "community": 0
    };

    const seen = new Map<string, SkillMeta>();
    for (const r of all) {
      const existing = seen.get(r.identifier);
      if (!existing) {
        seen.set(r.identifier, r);
      } else {
        const rRank = trustRank[r.trust_level] ?? 0;
        const eRank = trustRank[existing.trust_level] ?? 0;
        if (rRank > eRank) {
          seen.set(r.identifier, r);
        }
      }
    }

    const deduped = Array.from(seen.values());

    deduped.sort((a, b) => {
      const aOfficial = a.source === "official";
      const bOfficial = b.source === "official";
      if (aOfficial !== bOfficial) {
        return aOfficial ? -1 : 1;
      }
      const aRank = trustRank[a.trust_level] ?? 0;
      const bRank = trustRank[b.trust_level] ?? 0;
      if (aRank !== bRank) {
        return bRank - aRank;
      }
      return a.name.toLowerCase().localeCompare(b.name.toLowerCase());
    });

    if (limit > 0 && deduped.length > limit) {
      return deduped.slice(0, limit);
    }
    return deduped;
  });
}

export function ResolveShortName(name: string, sources: SkillSource[]): Effect.Effect<string, Error> {
  return Effect.gen(function* () {
    const results = yield* UnifiedSearch(name, sources, "all", 20);
    const exact: SkillMeta[] = [];
    const nameLower = name.toLowerCase();
    for (const r of results) {
      if (r.name.toLowerCase() === nameLower) {
        exact.push(r);
      }
    }
    if (exact.length === 1) {
      return exact[0].identifier;
    }
    return "";
  });
}

export function CheckForSkillUpdates(
  name: string,
  sources: SkillSource[]
): Effect.Effect<any[], Error> {
  return Effect.gen(function* () {
    const lock = new HubLockFile();
    let installed = lock.ListInstalled();
    if (name !== "") {
      installed = installed.filter(entry => {
        const n = entry.metadata?.name || "";
        return n === name;
      });
    }

    const results: any[] = [];
    for (const entry of installed) {
      const n = entry.metadata?.name || "";
      const identifier = entry.identifier;
      const sourceName = entry.source;

      let bundle: SkillBundle | null = null;
      for (const src of sources) {
        const sid = src.SourceID();
        if (sid === sourceName || (sourceName === "skills-sh" && sid === "skills.sh")) {
          const fetchResult = yield* src.Fetch(identifier).pipe(
            Effect.either
          );
          if (fetchResult._tag === "Right") {
            bundle = fetchResult.right;
            break;
          }
        }
      }

      if (!bundle) {
        results.push({
          name: n,
          identifier,
          source: sourceName,
          status: "unavailable"
        });
        continue;
      }

      const currentHash = entry.content_hash;
      const latestHash = bundleContentHash(bundle);
      let status = "up_to_date";
      if (currentHash !== latestHash) {
        status = "update_available";
      }

      results.push({
        name: n,
        identifier: identifier,
        source: sourceName,
        status,
        current_hash: currentHash,
        latest_hash: latestHash,
        bundle
      });
    }
    return results;
  });
}

function askQuestion(query: string): Promise<string> {
  const rl = readline.createInterface({
    input: process.stdin,
    output: process.stdout
  });
  return new Promise(resolve => rl.question(query, ans => {
    rl.close();
    resolve(ans);
  }));
}

const askQuestionEffect = (query: string) => Effect.tryPromise({
  try: () => askQuestion(query),
  catch: (err) => err instanceof Error ? err : new Error(String(err))
});

export function InstallSkill(
  identifier: string,
  category: string,
  nameOverride: string,
  force: boolean,
  skipConfirm: boolean,
  auth?: GitHubAuth
): Effect.Effect<string, Error> {
  return Effect.gen(function* () {
    ensureHubDirs();
    const g = auth || ResolveGitHubAuth();
    const sources = CreateSourceRouter(g);

    let finalIdentifier = identifier;
    if (!identifier.includes("/")) {
      const resolved = yield* ResolveShortName(identifier, sources);
      if (resolved === "") {
        return yield* Effect.fail(new Error(`could not resolve short name "${identifier}" to a unique skill`));
      }
      finalIdentifier = resolved;
    }

    let meta: SkillMeta | null = null;
    let bundle: SkillBundle | null = null;

    for (const src of sources) {
      if (!meta) {
        const metaRes = yield* src.Inspect(finalIdentifier).pipe(Effect.either);
        if (metaRes._tag === "Right") {
          meta = metaRes.right;
        }
      }
      const bundleRes = yield* src.Fetch(finalIdentifier).pipe(Effect.either);
      if (bundleRes._tag === "Right") {
        bundle = bundleRes.right;
        if (!meta) {
          const metaRes2 = yield* src.Inspect(finalIdentifier).pipe(Effect.either);
          if (metaRes2._tag === "Right") {
            meta = metaRes2.right;
          }
        }
        break;
      }
    }

    if (!bundle) {
      return yield* Effect.fail(new Error(`could not fetch skill "${finalIdentifier}" from any source`));
    }

    if (bundle.source === "url" && (bundle.name === "" || (bundle.metadata && bundle.metadata.awaiting_name === true))) {
      if (nameOverride !== "") {
        bundle.name = nameOverride;
      } else if (skipConfirm) {
        return yield* Effect.fail(new Error(`cannot install from URL "${finalIdentifier}": SKILL.md lacks frontmatter name and no override provided via --name`));
      } else {
        const ans = yield* askQuestionEffect(`\nThe SKILL.md at ${finalIdentifier} doesn't declare a name in frontmatter.\nEnter a skill name: `);
        if (ans.trim() === "") {
          return yield* Effect.fail(new Error("installation cancelled (invalid name)"));
        }
        bundle.name = ans.trim();
      }
    }

    let finalCategory = category;
    if (bundle.source === "official" && finalCategory === "") {
      const parts = bundle.identifier.split("/");
      if (parts.length >= 3) {
        finalCategory = parts.slice(1, parts.length - 1).join("/");
      }
    }

    const lock = HubLockFile.Load();
    const existing = lock.GetInstalled(bundle.name);
    if (existing && !force) {
      return yield* Effect.fail(new Error(`skill "${bundle.name}" is already installed at ${existing.install_path} (use --force to reinstall)`));
    }

    const qPath = yield* quarantineBundle(bundle).pipe(
      Effect.catchAll(err => {
        appendAuditLog("BLOCKED", bundle!.name, bundle!.source, bundle!.trust_level, "invalid_path", err.message);
        return Effect.fail(err);
      })
    );

    const scanSource = bundle.identifier || finalIdentifier;
    const scanResult = ScanSkill(qPath, scanSource);

    const [allowed, reason] = shouldAllowInstall(scanResult, force);
    if (!allowed) {
      try { fs.rmSync(qPath, { recursive: true, force: true }); } catch {}
      appendAuditLog("BLOCKED", bundle.name, bundle.source, bundle.trust_level, scanResult.verdict, "blocked_by_guard");
      return yield* Effect.fail(new Error(`installation blocked: ${reason}`));
    }

    if (!force && !skipConfirm) {
      const answer = yield* askQuestionEffect(`\nInstall skill "${bundle.name}"? [y/N]: `);
      const cleanAns = answer.trim().toLowerCase();
      if (cleanAns !== "y" && cleanAns !== "yes") {
        try { fs.rmSync(qPath, { recursive: true, force: true }); } catch {}
        return yield* Effect.fail(new Error("installation cancelled by user"));
      }
    }

    const installDir = yield* installFromQuarantine(qPath, bundle.name, finalCategory, bundle, scanResult).pipe(
      Effect.catchAll(err => {
        try { fs.rmSync(qPath, { recursive: true, force: true }); } catch {}
        appendAuditLog("BLOCKED", bundle!.name, bundle!.source, bundle!.trust_level, "invalid_path", err.message);
        return Effect.fail(err);
      })
    );

    invalidateCache();

    return installDir;
  });
}

export function InspectSkill(
  identifier: string,
  auth?: GitHubAuth
): Effect.Effect<{ meta: SkillMeta; bundle: SkillBundle }, Error> {
  return Effect.gen(function* () {
    ensureHubDirs();
    const g = auth || ResolveGitHubAuth();
    const sources = CreateSourceRouter(g);

    let finalIdentifier = identifier;
    if (!identifier.includes("/")) {
      const resolved = yield* ResolveShortName(identifier, sources);
      if (resolved === "") {
        return yield* Effect.fail(new Error(`could not resolve short name "${identifier}"`));
      }
      finalIdentifier = resolved;
    }

    let meta: SkillMeta | null = null;
    let bundle: SkillBundle | null = null;

    for (const src of sources) {
      if (!meta) {
        const metaRes = yield* src.Inspect(finalIdentifier).pipe(Effect.either);
        if (metaRes._tag === "Right") {
          meta = metaRes.right;
        }
      }
      const bundleRes = yield* src.Fetch(finalIdentifier).pipe(Effect.either);
      if (bundleRes._tag === "Right") {
        bundle = bundleRes.right;
        if (!meta) {
          const metaRes2 = yield* src.Inspect(finalIdentifier).pipe(Effect.either);
          if (metaRes2._tag === "Right") {
            meta = metaRes2.right;
          }
        }
        break;
      }
    }

    if (!meta || !bundle) {
      return yield* Effect.fail(new Error(`could not find "${finalIdentifier}" in any source`));
    }

    return { meta, bundle };
  });
}

export function UpdateSkills(
  name: string,
  force: boolean,
  auth?: GitHubAuth
): Effect.Effect<string[], Error> {
  return Effect.gen(function* () {
    const g = auth || ResolveGitHubAuth();
    const sources = CreateSourceRouter(g);
    const updates = yield* CheckForSkillUpdates(name, sources);

    const updated: string[] = [];
    for (const up of updates) {
      const status = up.status;
      if (status === "update_available" || status === "unavailable") {
        const n = up.name;
        const identifier = up.identifier;

        const lock = HubLockFile.Load();
        const existing = lock.GetInstalled(n);
        let category = "";
        if (existing) {
          category = _deriveCategoryFromInstallPath(existing.install_path);
        }

        const installResult = yield* InstallSkill(identifier, category, n, true, true, g).pipe(
          Effect.either
        );
        if (installResult._tag === "Right") {
          updated.push(n);
        }
      }
    }
    return updated;
  });
}

function _deriveCategoryFromInstallPath(installPath: string): string {
  const cleaned = path.normalize(installPath);
  const dir = path.dirname(cleaned);
  if (dir === "." || dir === "/" || dir === "") {
    return "";
  }
  return dir.split(path.sep).join("/");
}

export function invalidateCache(): void {
  ClearSkillsPromptCache();
}

/*
PORT STATUS
source path: backend/skills/hub.go
source lines: 1382
draft lines: 1045
confidence: high
status: phase_b_compile
*/
