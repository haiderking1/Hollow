// PORT: backend/skills/hub_adapters.go

import fs from "node:fs";
import path from "node:path";
import crypto from "node:crypto";
import zlib from "node:zlib";
import { Effect } from "effect";

import { ParseFrontmatter, extractSkillDescription, extractSkillTags } from "./frontmatter";
import {
  guardedHttpGet,
  normalizeBundlePath,
  readIndexCache,
  writeIndexCache,
  GitHubAuth,
  TapEntry,
  LoadTaps,
  type SkillMeta,
  type SkillBundle,
  type SkillSource
} from "./hub";

// ---------------------------------------------------------------------------
// Helper: Quick Frontmatter Parse
// ---------------------------------------------------------------------------

export function parseFrontmatterQuick(content: string): Record<string, any> {
  const [fm] = ParseFrontmatter(content);
  if (fm !== null) {
    return fm;
  }

  // Fallback regex YAML parser
  if (!content.startsWith("---")) {
    return {};
  }
  const idx = content.substring(3).indexOf("---");
  if (idx === -1) {
    return {};
  }
  const yamlText = content.substring(3, idx + 3);
  const lines = yamlText.split("\n");
  const out: Record<string, any> = {};
  for (const line of lines) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith("#")) {
      continue;
    }
    const colonIdx = line.indexOf(":");
    if (colonIdx !== -1) {
      const key = line.substring(0, colonIdx).trim();
      const val = line.substring(colonIdx + 1).trim();
      out[key] = val;
    }
  }
  return out;
}

// ---------------------------------------------------------------------------
// GitHub Source Adapter
// ---------------------------------------------------------------------------

export interface githubTreeEntry {
  path: string;
  type: string;
  sha: string;
}

export const defaultTaps: TapEntry[] = [
  { repo: "openai/skills", path: "skills/.curated/" },
  { repo: "openai/skills", path: "skills/.system/" },
  { repo: "anthropics/skills", path: "skills/" },
  { repo: "huggingface/skills", path: "skills/" },
  { repo: "NVIDIA/skills", path: "skills/" },
  { repo: "garrytan/gstack", path: "" }
];

export class GitHubSource implements SkillSource {
  auth: GitHubAuth;
  taps: TapEntry[] = [];
  treeCache: Record<string, { branch: string; entries: githubTreeEntry[] }> = {};
  rateLimited = false;

  constructor(auth: GitHubAuth) {
    this.auth = auth;
  }

  SourceID(): string {
    return "github";
  }

  TrustLevelFor(identifier: string): string {
    const parts = identifier.split("/");
    if (parts.length >= 2) {
      const repo = parts[0] + "/" + parts[1];
      const trusted = new Set([
        "openai/skills",
        "anthropics/skills",
        "huggingface/skills",
        "nvidia/skills"
      ]);
      if (trusted.has(repo.toLowerCase())) {
        return "trusted";
      }
    }
    return "community";
  }

  Search(query: string, limit: number): Effect.Effect<SkillMeta[], Error> {
    return Effect.gen(this, function* () {
      const queryLower = query.toLowerCase();
      const results: SkillMeta[] = [];

      const taps = [...defaultTaps, ...LoadTaps()];

      for (const tap of taps) {
        const skills = yield* this.listSkillsInRepo(tap.repo, tap.path);
        for (const sk of skills) {
          const searchable = `${sk.name} ${sk.description} ${sk.tags.join(" ")}`.toLowerCase();
          if (searchable.includes(queryLower)) {
            results.push(sk);
          }
        }
      }

      const seen = new Set<string>();
      const deduped: SkillMeta[] = [];
      for (const r of results) {
        if (!seen.has(r.identifier)) {
          seen.add(r.identifier);
          deduped.push(r);
        }
      }

      if (limit > 0 && deduped.length > limit) {
        return deduped.slice(0, limit);
      }
      return deduped;
    });
  }

  Fetch(identifier: string): Effect.Effect<SkillBundle, Error> {
    return Effect.gen(this, function* () {
      const parts = identifier.split("/");
      if (parts.length < 3) {
        return yield* Effect.fail(new Error(`invalid github identifier: ${identifier}`));
      }

      const repo = parts[0] + "/" + parts[1];
      const skillPath = parts.slice(2).join("/");

      const files = yield* this.downloadDirectory(repo, skillPath);
      if (Object.keys(files).length === 0) {
        return yield* Effect.fail(new Error(`failed to download directory or no files found: ${identifier}`));
      }

      if (!files["SKILL.md"]) {
        return yield* Effect.fail(new Error("SKILL.md not found in download"));
      }

      const skillName = parts[parts.length - 1];
      const trust = this.TrustLevelFor(identifier);

      return {
        name: skillName,
        files,
        source: "github",
        identifier,
        trust_level: trust
      };
    });
  }

  Inspect(identifier: string): Effect.Effect<SkillMeta, Error> {
    return Effect.gen(this, function* () {
      const parts = identifier.split("/");
      if (parts.length < 3) {
        return yield* Effect.fail(new Error(`invalid github identifier: ${identifier}`));
      }

      const repo = parts[0] + "/" + parts[1];
      const skillPath = parts.slice(2).join("/");
      const skillMdPath = skillPath + "/SKILL.md";

      const content = yield* this.fetchFileContent(repo, skillMdPath);
      const fm = parseFrontmatterQuick(content);

      let name = fm.name || "";
      if (!name) {
        name = parts[parts.length - 1];
      }
      const desc = extractSkillDescription(fm);
      const tags = extractSkillTags(fm);

      return {
        name,
        description: desc,
        source: "github",
        identifier,
        trust_level: this.TrustLevelFor(identifier),
        repo,
        path: skillPath,
        tags
      };
    });
  }

  getRepoTree(repo: string): Effect.Effect<{ branch: string; entries: githubTreeEntry[] }, Error> {
    return Effect.gen(this, function* () {
      const cached = this.treeCache[repo];
      if (cached) {
        return cached;
      }

      const headers = this.auth.GetHeaders();

      const repoURL = `https://api.github.com/repos/${repo}`;
      const repoRes = yield* guardedHttpGet(repoURL, headers);
      if (repoRes.status === 403 || repoRes.status === 429) {
        this.rateLimited = true;
      }
      if (repoRes.status !== 200) {
        return yield* Effect.fail(new Error(`GitHub API returned status ${repoRes.status}`));
      }

      const repoInfo = JSON.parse(repoRes.body.toString("utf8"));
      let branch = repoInfo.default_branch;
      if (!branch) {
        branch = "main";
      }

      const treeURL = `https://api.github.com/repos/${repo}/git/trees/${branch}?recursive=1`;
      const treeRes = yield* guardedHttpGet(treeURL, headers);
      if (treeRes.status !== 200) {
        return yield* Effect.fail(new Error(`GitHub Trees API returned status ${treeRes.status}`));
      }

      const treeData = JSON.parse(treeRes.body.toString("utf8"));
      const entries = (treeData.tree || []).map((e: any) => ({
        path: e.path || "",
        type: e.type || "",
        sha: e.sha || ""
      }));

      const result = { branch, entries };
      this.treeCache[repo] = result;
      return result;
    });
  }

  listSkillsInRepo(repo: string, rootPath: string): Effect.Effect<SkillMeta[], Error> {
    return Effect.gen(this, function* () {
      const cacheKey = `gh_skills_${repo.replace(/\//g, "_")}_${rootPath.replace(/\//g, "_")}`;
      const [cached, ok] = readIndexCache(cacheKey);
      if (ok && cached) {
        try {
          return JSON.parse(cached.toString("utf8"));
        } catch {}
      }

      const treeResult = yield* this.getRepoTree(repo).pipe(
        Effect.catchAll(() => Effect.succeed({ branch: "", entries: [] as githubTreeEntry[] }))
      );
      const entries = treeResult.entries;
      if (entries.length === 0) {
        return [];
      }

      let prefix = rootPath;
      if (prefix !== "" && !prefix.endsWith("/")) {
        prefix += "/";
      }

      const skills: SkillMeta[] = [];
      const seenDirs = new Set<string>();

      for (const entry of entries) {
        if (entry.type !== "blob" || !entry.path.endsWith("/SKILL.md")) {
          continue;
        }
        if (prefix !== "" && !entry.path.startsWith(prefix)) {
          continue;
        }

        const dir = path.dirname(entry.path);
        if (seenDirs.has(dir)) {
          continue;
        }
        seenDirs.add(dir);

        const ident = repo + "/" + dir;
        const metaRes = yield* this.Inspect(ident).pipe(Effect.either);
        if (metaRes._tag === "Right") {
          skills.push(metaRes.right);
        }
      }

      try {
        const dataBytes = Buffer.from(JSON.stringify(skills));
        writeIndexCache(cacheKey, dataBytes);
      } catch {}

      return skills;
    });
  }

  downloadDirectory(repo: string, skillPath: string): Effect.Effect<Record<string, Uint8Array>, Error> {
    return Effect.gen(this, function* () {
      const treeResult = yield* this.getRepoTree(repo);
      const entries = treeResult.entries;

      let prefix = skillPath;
      if (!prefix.endsWith("/")) {
        prefix += "/";
      }

      const files: Record<string, Uint8Array> = {};
      for (const entry of entries) {
        if (entry.type !== "blob" || !entry.path.startsWith(prefix)) {
          continue;
        }
        const rel = entry.path.substring(prefix.length);
        const contentRes = yield* this.fetchFileContent(repo, entry.path).pipe(
          Effect.either
        );
        if (contentRes._tag === "Right") {
          files[rel] = Buffer.from(contentRes.right, "utf8");
        }
      }
      return files;
    });
  }

  fetchFileContent(repo: string, filePath: string): Effect.Effect<string, Error> {
    return Effect.gen(this, function* () {
      const urlStr = `https://api.github.com/repos/${repo}/contents/${filePath}`;
      const headers = this.auth.GetHeaders();
      headers["Accept"] = "application/vnd.github.v3.raw";

      const res = yield* guardedHttpGet(urlStr, headers);
      if (res.status !== 200) {
        return yield* Effect.fail(new Error(`contents API returned status ${res.status}`));
      }
      return res.body.toString("utf8");
    });
  }
}

// ---------------------------------------------------------------------------
// URL Source Adapter
// ---------------------------------------------------------------------------

export class UrlSource implements SkillSource {
  SourceID(): string {
    return "url";
  }

  TrustLevelFor(identifier: string): string {
    return "community";
  }

  Search(query: string, limit: number): Effect.Effect<SkillMeta[], Error> {
    return Effect.succeed([]);
  }

  matches(identifier: string): boolean {
    const ident = identifier.trim();
    const lower = ident.toLowerCase();
    if (!lower.startsWith("http://") && !lower.startsWith("https://")) {
      return false;
    }
    if (lower.includes("/.well-known/skills/") || lower.endsWith("/index.json")) {
      return false;
    }
    try {
      const u = new URL(ident);
      return u.pathname.toLowerCase().endsWith(".md");
    } catch {
      return false;
    }
  }

  Inspect(identifier: string): Effect.Effect<SkillMeta, Error> {
    return Effect.gen(this, function* () {
      if (!this.matches(identifier)) {
        return yield* Effect.fail(new Error("not a direct url skill"));
      }

      const res = yield* guardedHttpGet(identifier);
      if (res.status !== 200) {
        return yield* Effect.fail(new Error(`failed to fetch url, status: ${res.status}`));
      }

      const content = res.body.toString("utf8");
      const fm = parseFrontmatterQuick(content);

      let name = fm.name || "";
      if (!name) {
        name = this.resolveSkillName(identifier);
      }

      const desc = extractSkillDescription(fm);
      const tags = extractSkillTags(fm);

      return {
        name,
        description: desc,
        source: "url",
        identifier,
        trust_level: "community",
        tags
      };
    });
  }

  Fetch(identifier: string): Effect.Effect<SkillBundle, Error> {
    return Effect.gen(this, function* () {
      if (!this.matches(identifier)) {
        return yield* Effect.fail(new Error("not a direct url skill"));
      }

      const res = yield* guardedHttpGet(identifier);
      if (res.status !== 200) {
        return yield* Effect.fail(new Error(`failed to fetch url, status: ${res.status}`));
      }

      const content = res.body.toString("utf8");
      const fm = parseFrontmatterQuick(content);
      let name = "";
      if (fm) {
        name = fm.name || "";
      }
      if (!name) {
        name = this.resolveSkillName(identifier);
      }

      const files: Record<string, Uint8Array> = {
        "SKILL.md": res.body
      };

      return {
        name,
        files,
        source: "url",
        identifier,
        trust_level: "community"
      };
    });
  }

  resolveSkillName(urlStr: string): string {
    try {
      const u = new URL(urlStr);
      const pathname = u.pathname.trim();
      const parts = pathname.split("/").filter(p => p !== "");
      if (parts.length === 0) {
        return "unnamed-skill";
      }
      let last = parts[parts.length - 1];
      if (last.toLowerCase() === "skill.md" && parts.length >= 2) {
        last = parts[parts.length - 2];
      }
      if (last.toLowerCase().endsWith(".md")) {
        last = last.substring(0, last.length - 3);
      }
      return last.toLowerCase();
    } catch {
      return "unnamed-skill";
    }
  }
}

// ---------------------------------------------------------------------------
// Well-Known Agent Skills Source Adapter
// ---------------------------------------------------------------------------

export class WellKnownSkillSource implements SkillSource {
  SourceID(): string {
    return "well-known";
  }

  TrustLevelFor(identifier: string): string {
    return "community";
  }

  parseIdentifier(identifier: string): { indexURL: string; baseURL: string; skillName: string; skillURL: string } | null {
    let raw = identifier;
    if (raw.startsWith("well-known:")) {
      raw = raw.substring(11);
    }
    if (!raw.startsWith("http://") && !raw.startsWith("https://")) {
      return null;
    }

    try {
      const u = new URL(raw);
      const cleanURL = u.protocol + "//" + u.host + u.pathname;
      const fragment = u.hash.startsWith("#") ? u.hash.substring(1) : u.hash;

      if (cleanURL.endsWith("/index.json")) {
        if (fragment === "") {
          return null;
        }
        const baseURL = cleanURL.substring(0, cleanURL.length - 11);
        return {
          indexURL: cleanURL,
          baseURL,
          skillName: fragment,
          skillURL: baseURL + "/" + fragment
        };
      }

      let skillURL = cleanURL;
      if (cleanURL.endsWith("/SKILL.md")) {
        skillURL = cleanURL.substring(0, cleanURL.length - 9);
      } else if (cleanURL.endsWith("/")) {
        skillURL = cleanURL.substring(0, cleanURL.length - 1);
      }

      if (!skillURL.includes("/.well-known/skills/")) {
        return null;
      }

      const idx = skillURL.lastIndexOf("/");
      if (idx === -1) {
        return null;
      }
      const baseURL = skillURL.substring(0, idx);
      const skillName = skillURL.substring(idx + 1);
      const indexURL = baseURL + "/index.json";

      return { indexURL, baseURL, skillName, skillURL };
    } catch {
      return null;
    }
  }

  fetchIndex(indexURL: string): Effect.Effect<any, Error> {
    return Effect.gen(this, function* () {
      const cacheKey = "well_known_index_" + md5Hash(indexURL);
      const [cached, ok] = readIndexCache(cacheKey);
      if (ok && cached) {
        try {
          return JSON.parse(cached.toString("utf8"));
        } catch {}
      }

      const res = yield* guardedHttpGet(indexURL);
      if (res.status !== 200) {
        return yield* Effect.fail(new Error(`failed to fetch well-known index: ${res.status}`));
      }

      const idx = JSON.parse(res.body.toString("utf8"));
      writeIndexCache(cacheKey, res.body);
      return idx;
    });
  }

  Search(query: string, limit: number): Effect.Effect<SkillMeta[], Error> {
    return Effect.gen(this, function* () {
      const q = query.trim();
      let indexURL = "";
      if (q.startsWith("http://") || q.startsWith("https://")) {
        if (q.endsWith("/index.json")) {
          indexURL = q;
        } else if (q.includes("/.well-known/skills/")) {
          indexURL = q.split("/.well-known/skills/")[0] + "/.well-known/skills/index.json";
        } else {
          const base = q.endsWith("/") ? q.substring(0, q.length - 1) : q;
          indexURL = base + "/.well-known/skills/index.json";
        }
      } else {
        return [];
      }

      const idx = yield* this.fetchIndex(indexURL);
      const baseURL = indexURL.substring(0, indexURL.length - 11);

      const results: SkillMeta[] = [];
      const skillsList = idx.skills || [];
      for (const entry of skillsList) {
        let files = entry.files;
        if (!files || !Array.isArray(files) || files.length === 0) {
          files = ["SKILL.md"];
        }
        results.push({
          name: entry.name || "",
          description: entry.description || "",
          source: "well-known",
          identifier: "well-known:" + baseURL + "/" + entry.name,
          trust_level: "community",
          path: entry.name || "",
          tags: [],
          extra: {
            index_url: indexURL,
            base_url: baseURL,
            files
          }
        });
      }

      if (limit > 0 && results.length > limit) {
        return results.slice(0, limit);
      }
      return results;
    });
  }

  Inspect(identifier: string): Effect.Effect<SkillMeta, Error> {
    return Effect.gen(this, function* () {
      const parsed = this.parseIdentifier(identifier);
      if (!parsed) {
        return yield* Effect.fail(new Error("invalid well-known identifier"));
      }

      const idx = yield* this.fetchIndex(parsed.indexURL);
      let match: any = null;
      const skillsList = idx.skills || [];
      for (const entry of skillsList) {
        if (entry.name === parsed.skillName) {
          match = entry;
          break;
        }
      }
      if (!match) {
        return yield* Effect.fail(new Error("skill not found in index"));
      }

      const skillMdURL = parsed.skillURL + "/SKILL.md";
      const res = yield* guardedHttpGet(skillMdURL);
      if (res.status !== 200) {
        return yield* Effect.fail(new Error("failed to fetch SKILL.md"));
      }

      const fm = parseFrontmatterQuick(res.body.toString("utf8"));
      let desc = match.description || "";
      if (fm) {
        const d = extractSkillDescription(fm);
        if (d !== "") {
          desc = d;
        }
      }

      let files = match.files;
      if (!files || !Array.isArray(files) || files.length === 0) {
        files = ["SKILL.md"];
      }

      return {
        name: parsed.skillName,
        description: desc,
        source: "well-known",
        identifier,
        trust_level: "community",
        path: parsed.skillName,
        tags: [],
        extra: {
          index_url: parsed.indexURL,
          base_url: parsed.baseURL,
          files,
          endpoint: parsed.skillURL
        }
      };
    });
  }

  Fetch(identifier: string): Effect.Effect<SkillBundle, Error> {
    return Effect.gen(this, function* () {
      const meta = yield* this.Inspect(identifier);
      const extra = meta.extra || {};
      const endpoint = extra.endpoint || "";
      const fileList = extra.files || [];

      const files: Record<string, Uint8Array> = {};
      for (const relPath of fileList) {
        const safeRel = yield* normalizeBundlePath(relPath, "well-known file path", true);
        const fileURL = endpoint + "/" + safeRel;
        const res = yield* guardedHttpGet(fileURL);
        if (res.status !== 200) {
          return yield* Effect.fail(new Error(`failed to fetch ${relPath}: status ${res.status}`));
        }
        files[safeRel] = res.body;
      }

      return {
        name: meta.name,
        files,
        source: "well-known",
        identifier,
        trust_level: "community",
        metadata: extra
      };
    });
  }
}

// ---------------------------------------------------------------------------
// Skills.sh Source Adapter
// ---------------------------------------------------------------------------

export class SkillsShSource implements SkillSource {
  auth: GitHubAuth;
  github: GitHubSource;

  constructor(auth: GitHubAuth) {
    this.auth = auth;
    this.github = new GitHubSource(auth);
  }

  SourceID(): string {
    return "skills-sh";
  }

  TrustLevelFor(identifier: string): string {
    return this.github.TrustLevelFor(this.normalizeIdentifier(identifier));
  }

  normalizeIdentifier(identifier: string): string {
    let raw = identifier;
    const prefixes = ["skills-sh/", "skills.sh/", "skils-sh/", "skils.sh/"];
    for (const prefix of prefixes) {
      if (raw.startsWith(prefix)) {
        return raw.substring(prefix.length);
      }
    }
    return raw;
  }

  Search(query: string, limit: number): Effect.Effect<SkillMeta[], Error> {
    return Effect.gen(this, function* () {
      const q = query.trim();
      if (q === "") {
        return yield* this.sitemapCatalog(limit);
      }

      const cacheKey = "skills_sh_search_" + md5Hash(`${q}|${limit}`);
      const [cached, ok] = readIndexCache(cacheKey);
      if (ok && cached) {
        try {
          return JSON.parse(cached.toString("utf8"));
        } catch {}
      }

      const searchURL = `https://skills.sh/api/search?q=${encodeURIComponent(q)}&limit=${limit}`;
      const res = yield* guardedHttpGet(searchURL);
      if (res.status !== 200) {
        return [];
      }

      const data = JSON.parse(res.body.toString("utf8"));
      const results: SkillMeta[] = [];
      const skillsList = data.skills || [];
      for (const item of skillsList) {
        let canonical = item.id || "";
        if (canonical === "") {
          canonical = (item.source || "") + "/" + (item.skillId || "");
        }
        const parts = canonical.split("/");
        if (parts.length < 3) {
          continue;
        }
        const repo = parts[0] + "/" + parts[1];
        const skillPath = parts[2];

        let installsLabel = "";
        if (item.installs > 0) {
          installsLabel = ` · ${item.installs} installs`;
        }

        results.push({
          name: item.name || "",
          description: `Indexed by skills.sh from ${repo}${installsLabel}`,
          source: "skills.sh",
          identifier: "skills-sh/" + canonical,
          trust_level: this.TrustLevelFor(canonical),
          repo,
          path: skillPath,
          tags: [],
          extra: {
            installs: item.installs,
            detail_url: "https://skills.sh/" + canonical,
            repo_url: "https://github.com/" + repo
          }
        });
      }

      try {
        writeIndexCache(cacheKey, Buffer.from(JSON.stringify(results)));
      } catch {}

      return results;
    });
  }

  sitemapCatalog(limit: number): Effect.Effect<SkillMeta[], Error> {
    return Effect.gen(this, function* () {
      const cacheKey = "skills_sh_sitemap_v1";
      const [cached, ok] = readIndexCache(cacheKey);
      if (ok && cached) {
        try {
          const out = JSON.parse(cached.toString("utf8"));
          if (limit > 0 && out.length > limit) {
            return out.slice(0, limit);
          }
          return out;
        } catch {}
      }

      const sitemapURL = "https://www.skills.sh/sitemap.xml";
      const res = yield* guardedHttpGet(sitemapURL, { "Accept-Encoding": "gzip" });
      if (res.status !== 200) {
        return [];
      }

      const xmlText = res.body.toString("utf8");
      const locRe = /<loc>([^<]+)<\/loc>/gi;
      let match;
      const sitemaps: string[] = [];
      while ((match = locRe.exec(xmlText)) !== null) {
        const loc = match[1].trim();
        if (loc.includes("sitemap-skills")) {
          sitemaps.push(loc);
        }
      }

      const results: SkillMeta[] = [];
      const seen = new Set<string>();
      const skillRe = /^https?:\/\/(?:www\.)?skills\.sh\/([^\/]+)\/([^\/]+)\/([^\/]+)\/?$/i;

      for (const smURL of sitemaps) {
        const smRes = yield* guardedHttpGet(smURL, { "Accept-Encoding": "gzip" });
        if (smRes.status !== 200) {
          continue;
        }
        const smXml = smRes.body.toString("utf8");
        let smMatch;
        locRe.lastIndex = 0;
        while ((smMatch = locRe.exec(smXml)) !== null) {
          const u = smMatch[1].trim();
          const smSub = u.match(skillRe);
          if (!smSub || smSub.length < 4) {
            continue;
          }
          const owner = smSub[1];
          const repoName = smSub[2];
          const skillName = smSub[3];
          const canonical = `${owner}/${repoName}/${skillName}`;

          if (seen.has(canonical)) {
            continue;
          }
          seen.add(canonical);
          const repo = owner + "/" + repoName;

          results.push({
            name: skillName,
            description: "Indexed by skills.sh from " + repo,
            source: "skills.sh",
            identifier: "skills-sh/" + canonical,
            trust_level: this.TrustLevelFor(canonical),
            repo,
            path: skillName,
            tags: [],
            extra: {
              detail_url: "https://skills.sh/" + canonical,
              repo_url: "https://github.com/" + repo
            }
          });
        }
      }

      if (results.length > 0) {
        try {
          writeIndexCache(cacheKey, Buffer.from(JSON.stringify(results)));
        } catch {}
      }

      if (limit > 0 && results.length > limit) {
        return results.slice(0, limit);
      }
      return results;
    });
  }

  Fetch(identifier: string): Effect.Effect<SkillBundle, Error> {
    return Effect.gen(this, function* () {
      const canonical = this.normalizeIdentifier(identifier);
      const detail = yield* this.fetchDetailPage(canonical);

      let repo = canonical.substring(0, canonical.lastIndexOf("/"));
      if (detail && detail.repo) {
        repo = detail.repo;
      }

      const skillName = canonical.substring(canonical.lastIndexOf("/") + 1);
      const candidates = [
        canonical,
        repo + "/skills/" + skillName,
        repo + "/.agents/skills/" + skillName,
        repo + "/.claude/skills/" + skillName
      ];

      for (const cand of candidates) {
        const bundleRes = yield* this.github.Fetch(cand).pipe(Effect.either);
        if (bundleRes._tag === "Right") {
          const bundle = bundleRes.right;
          bundle.source = "skills.sh";
          bundle.identifier = "skills-sh/" + canonical;
          if (detail) {
            bundle.metadata = detail;
          }
          return bundle;
        }
      }

      return yield* Effect.fail(new Error(`failed to fetch skills-sh bundle: ${identifier}`));
    });
  }

  Inspect(identifier: string): Effect.Effect<SkillMeta, Error> {
    return Effect.gen(this, function* () {
      const canonical = this.normalizeIdentifier(identifier);
      const detail = yield* this.fetchDetailPage(canonical);

      let repo = canonical.substring(0, canonical.lastIndexOf("/"));
      if (detail && detail.repo) {
        repo = detail.repo;
      }

      const skillName = canonical.substring(canonical.lastIndexOf("/") + 1);
      const candidates = [
        canonical,
        repo + "/skills/" + skillName,
        repo + "/.agents/skills/" + skillName,
        repo + "/.claude/skills/" + skillName
      ];

      let meta: SkillMeta | null = null;
      for (const cand of candidates) {
        const metaRes = yield* this.github.Inspect(cand).pipe(Effect.either);
        if (metaRes._tag === "Right") {
          meta = metaRes.right;
          break;
        }
      }

      if (!meta) {
        return yield* Effect.fail(new Error(`failed to inspect skills-sh: ${identifier}`));
      }

      meta.source = "skills.sh";
      meta.identifier = "skills-sh/" + canonical;
      meta.trust_level = this.TrustLevelFor(canonical);
      if (detail) {
        meta.extra = detail;
        if (detail.body_summary && detail.body_summary !== "") {
          meta.description = detail.body_summary;
        }
      }

      return meta;
    });
  }

  fetchDetailPage(identifier: string): Effect.Effect<Record<string, any>, Error> {
    return Effect.gen(this, function* () {
      const cacheKey = "skills_sh_detail_" + md5Hash(identifier);
      const [cached, ok] = readIndexCache(cacheKey);
      if (ok && cached) {
        try {
          return JSON.parse(cached.toString("utf8"));
        } catch {}
      }

      const detailURL = "https://skills.sh/" + identifier;
      const res = yield* guardedHttpGet(detailURL);
      if (res.status !== 200) {
        return {};
      }

      const html = res.body.toString("utf8");
      const detail: Record<string, any> = {};

      const titleRe = /<h1[^>]*>([\s\S]*?)<\/h1>/i;
      const titleMatch = html.match(titleRe);
      if (titleMatch) {
        detail.page_title = stripHTML(titleMatch[1]);
      }

      const proseRe = /<div[^>]*class=["']?[^"']*prose[^"']*["']?[^>]*>[\s\S]*?<p[^>]*>([\s\S]*?)<\/p>/i;
      const proseMatch = html.match(proseRe);
      if (proseMatch) {
        detail.body_summary = stripHTML(proseMatch[1]);
      }

      const installsRe = /Weekly Installs[\s\S]*?children\\":\\"([^"\\]+)\\"/i;
      const installsMatch = html.match(installsRe);
      if (installsMatch) {
        detail.weekly_installs = installsMatch[1];
      }

      const installCmdRe = /npx\s+skills\s+add\s+(https?:\/\/github\.com\/[^\s<]+|[^\s<]+)/i;
      const installMatch = html.match(installCmdRe);
      if (installMatch) {
        let repoVal = installMatch[1].trim();
        if (repoVal.startsWith("https://github.com/")) {
          repoVal = repoVal.substring(19);
        }
        repoVal = repoVal.replace(/\/+$/, "");
        const parts = repoVal.split("/");
        if (parts.length >= 2) {
          detail.repo = parts[0] + "/" + parts[1];
        }
      }

      detail.detail_url = detailURL;

      try {
        writeIndexCache(cacheKey, Buffer.from(JSON.stringify(detail)));
      } catch {}

      return detail;
    });
  }
}

// ---------------------------------------------------------------------------
// ClawHub Source Adapter
// ---------------------------------------------------------------------------

function unzipBuffer(buffer: Buffer): Record<string, Buffer> {
  const files: Record<string, Buffer> = {};
  let offset = 0;
  while (offset < buffer.length - 30) {
    const signature = buffer.readUInt32LE(offset);
    if (signature !== 0x04034b50) {
      break;
    }
    const compMethod = buffer.readUInt16LE(offset + 8);
    const compressedSize = buffer.readUInt32LE(offset + 18);
    const uncompressedSize = buffer.readUInt32LE(offset + 22);
    const fileNameLength = buffer.readUInt16LE(offset + 26);
    const extraFieldLength = buffer.readUInt16LE(offset + 28);
    const fileName = buffer.toString("utf8", offset + 30, offset + 30 + fileNameLength);
    
    const dataOffset = offset + 30 + fileNameLength + extraFieldLength;
    if (dataOffset + compressedSize > buffer.length) {
      break;
    }
    
    const compData = buffer.subarray(dataOffset, dataOffset + compressedSize);
    let decompressed: Buffer;
    if (compMethod === 0) {
      decompressed = Buffer.from(compData);
    } else if (compMethod === 8) {
      try {
        decompressed = zlib.inflateRawSync(compData);
      } catch {
        offset = dataOffset + compressedSize;
        continue;
      }
    } else {
      offset = dataOffset + compressedSize;
      continue;
    }
    
    files[fileName] = decompressed;
    offset = dataOffset + compressedSize;
  }
  return files;
}

export class ClawHubSource implements SkillSource {
  SourceID(): string {
    return "clawhub";
  }

  TrustLevelFor(identifier: string): string {
    return "community";
  }

  Search(query: string, limit: number): Effect.Effect<SkillMeta[], Error> {
    return Effect.gen(this, function* () {
      const q = query.trim();
      const cacheKey = "clawhub_search_" + md5Hash(`${q}|${limit}`);
      const [cached, ok] = readIndexCache(cacheKey);
      if (ok && cached) {
        try {
          return JSON.parse(cached.toString("utf8"));
        } catch {}
      }

      const searchURL = `https://clawhub.ai/api/v1/skills?search=${encodeURIComponent(q)}&limit=${limit}`;
      const res = yield* guardedHttpGet(searchURL);
      if (res.status !== 200) {
        return [];
      }

      let items: any[] = [];
      try {
        const data = JSON.parse(res.body.toString("utf8"));
        if (data.items) {
          items = data.items;
        } else if (Array.isArray(data)) {
          items = data;
        }
      } catch {
        return [];
      }

      const results: SkillMeta[] = [];
      for (const item of items) {
        let name = item.displayName || "";
        if (name === "") name = item.name || "";
        if (name === "") name = item.slug || "";
        let desc = item.summary || "";
        if (desc === "") desc = item.description || "";

        results.push({
          name,
          description: desc,
          source: "clawhub",
          identifier: item.slug || "",
          trust_level: "community",
          tags: item.tags || []
        });
      }

      try {
        writeIndexCache(cacheKey, Buffer.from(JSON.stringify(results)));
      } catch {}

      return results;
    });
  }

  Inspect(identifier: string): Effect.Effect<SkillMeta, Error> {
    return Effect.gen(this, function* () {
      let slug = identifier;
      const idx = slug.lastIndexOf("/");
      if (idx !== -1) {
        slug = slug.substring(idx + 1);
      }

      const detailURL = `https://clawhub.ai/api/v1/skills/${slug}`;
      const res = yield* guardedHttpGet(detailURL);
      if (res.status !== 200) {
        return yield* Effect.fail(new Error(`failed to inspect clawhub: ${identifier}`));
      }

      let data: any = {};
      try {
        const obj = JSON.parse(res.body.toString("utf8"));
        if (obj.skill && obj.skill.slug) {
          data = obj.skill;
        } else {
          data = obj;
        }
      } catch (err) {
        return yield* Effect.fail(err instanceof Error ? err : new Error(String(err)));
      }

      let name = data.displayName || "";
      if (name === "") name = data.name || "";
      if (name === "") name = data.slug || "";
      let desc = data.summary || "";
      if (desc === "") desc = data.description || "";

      return {
        name,
        description: desc,
        source: "clawhub",
        identifier: data.slug || "",
        trust_level: "community",
        tags: data.tags || []
      };
    });
  }

  Fetch(identifier: string): Effect.Effect<SkillBundle, Error> {
    return Effect.gen(this, function* () {
      let slug = identifier;
      const idx = slug.lastIndexOf("/");
      if (idx !== -1) {
        slug = slug.substring(idx + 1);
      }

      const detailURL = `https://clawhub.ai/api/v1/skills/${slug}`;
      const res = yield* guardedHttpGet(detailURL);
      if (res.status !== 200) {
        return yield* Effect.fail(new Error(`failed to inspect clawhub: ${identifier}`));
      }

      let v = "";
      try {
        const obj = JSON.parse(res.body.toString("utf8"));
        if (obj.skill && obj.skill.latestVersion) {
          v = obj.skill.latestVersion;
        } else if (obj.latestVersion) {
          v = obj.latestVersion;
        }
      } catch {}

      if (v === "") {
        const versionsURL = `https://clawhub.ai/api/v1/skills/${slug}/versions`;
        const vRes = yield* guardedHttpGet(versionsURL).pipe(Effect.either);
        if (vRes._tag === "Right" && vRes.right.status === 200) {
          try {
            const vList = JSON.parse(vRes.right.body.toString("utf8"));
            if (Array.isArray(vList) && vList.length > 0) {
              v = vList[0].version || "";
            }
          } catch {}
        }
      }

      if (v === "") {
        return yield* Effect.fail(new Error("failed to resolve latest version"));
      }

      const dlURL = `https://clawhub.ai/api/v1/download?slug=${slug}&version=${v}`;
      const zipRes = yield* guardedHttpGet(dlURL);
      if (zipRes.status !== 200) {
        return yield* Effect.fail(new Error(`failed to download ZIP, status: ${zipRes.status}`));
      }

      const files: Record<string, Uint8Array> = {};
      const unzipped = unzipBuffer(zipRes.body);
      for (const [fName, fContent] of Object.entries(unzipped)) {
        const safeRelRes = yield* normalizeBundlePath(fName, "zip member path", true).pipe(Effect.either);
        if (safeRelRes._tag === "Right") {
          if (fContent.length <= 500000) {
            files[safeRelRes.right] = fContent;
          }
        }
      }

      if (!files["SKILL.md"]) {
        const vURL = `https://clawhub.ai/api/v1/skills/${slug}/versions/${v}`;
        const vRes = yield* guardedHttpGet(vURL).pipe(Effect.either);
        if (vRes._tag === "Right" && vRes.right.status === 200) {
          try {
            const obj = JSON.parse(vRes.right.body.toString("utf8"));
            const fMap = obj.version?.files || obj.files || {};
            for (const [fPath, fText] of Object.entries(fMap)) {
              const safeRelRes = yield* normalizeBundlePath(fPath, "version metadata path", true).pipe(Effect.either);
              if (safeRelRes._tag === "Right") {
                files[safeRelRes.right] = Buffer.from(fText as string, "utf8");
              }
            }
          } catch {}
        }
      }

      if (!files["SKILL.md"]) {
        return yield* Effect.fail(new Error("failed to extract SKILL.md"));
      }

      return {
        name: slug,
        files,
        source: "clawhub",
        identifier: identifier,
        trust_level: "community"
      };
    });
  }
}

// ---------------------------------------------------------------------------
// Claude Code Marketplace Source Adapter
// ---------------------------------------------------------------------------

export class ClaudeMarketplaceSource implements SkillSource {
  auth: GitHubAuth;
  github: GitHubSource;

  constructor(auth: GitHubAuth) {
    this.auth = auth;
    this.github = new GitHubSource(auth);
  }

  SourceID(): string {
    return "claude-marketplace";
  }

  TrustLevelFor(identifier: string): string {
    return this.github.TrustLevelFor(identifier);
  }

  Search(query: string, limit: number): Effect.Effect<SkillMeta[], Error> {
    return Effect.succeed([]);
  }

  Inspect(identifier: string): Effect.Effect<SkillMeta, Error> {
    return Effect.gen(this, function* () {
      const meta = yield* this.github.Inspect(identifier);
      meta.source = "claude-marketplace";
      meta.trust_level = this.TrustLevelFor(identifier);
      return meta;
    });
  }

  Fetch(identifier: string): Effect.Effect<SkillBundle, Error> {
    return Effect.gen(this, function* () {
      const bundle = yield* this.github.Fetch(identifier);
      bundle.source = "claude-marketplace";
      return bundle;
    });
  }
}

// ---------------------------------------------------------------------------
// LobeHub Source Adapter
// ---------------------------------------------------------------------------

export class LobeHubSource implements SkillSource {
  SourceID(): string {
    return "lobehub";
  }

  TrustLevelFor(identifier: string): string {
    return "community";
  }

  Search(query: string, limit: number): Effect.Effect<SkillMeta[], Error> {
    return Effect.gen(this, function* () {
      const queryLower = query.toLowerCase();
      const index = yield* this.fetchIndex();

      const results: SkillMeta[] = [];
      for (const agent of index) {
        let title = agent.meta?.title || "";
        if (title === "") {
          title = agent.identifier;
        }
        const tags = agent.meta?.tags || [];
        const searchable = `${title} ${agent.meta?.description || ""} ${tags.join(" ")}`.toLowerCase();
        if (searchable.includes(queryLower)) {
          results.push({
            name: agent.identifier,
            description: agent.meta?.description || "",
            source: "lobehub",
            identifier: "lobehub/" + agent.identifier,
            trust_level: "community",
            tags
          });
          if (results.length >= limit) {
            break;
          }
        }
      }
      return results;
    });
  }

  fetchIndex(): Effect.Effect<any[], Error> {
    return Effect.gen(this, function* () {
      const cacheKey = "lobehub_index";
      const [cached, ok] = readIndexCache(cacheKey);
      if (ok && cached) {
        try {
          const data = JSON.parse(cached.toString("utf8"));
          if (Array.isArray(data)) return data;
          if (data.agents) return data.agents;
        } catch {}
      }

      const res = yield* guardedHttpGet("https://chat-agents.lobehub.com/index.json");
      if (res.status !== 200) {
        return yield* Effect.fail(new Error(`failed to fetch lobehub index: status ${res.status}`));
      }

      let idx: any[] = [];
      try {
        const obj = JSON.parse(res.body.toString("utf8"));
        if (obj.agents && Array.isArray(obj.agents)) {
          idx = obj.agents;
        } else if (Array.isArray(obj)) {
          idx = obj;
        }
      } catch (err) {
        return yield* Effect.fail(err instanceof Error ? err : new Error(String(err)));
      }

      writeIndexCache(cacheKey, res.body);
      return idx;
    });
  }

  Inspect(identifier: string): Effect.Effect<SkillMeta, Error> {
    return Effect.gen(this, function* () {
      let agentID = identifier;
      if (agentID.startsWith("lobehub/")) {
        agentID = agentID.substring(8);
      }

      const index = yield* this.fetchIndex();
      for (const agent of index) {
        if (agent.identifier === agentID) {
          return {
            name: agentID,
            description: agent.meta?.description || "",
            source: "lobehub",
            identifier,
            trust_level: "community",
            tags: agent.meta?.tags || []
          };
        }
      }

      return yield* Effect.fail(new Error(`lobehub agent not found: ${agentID}`));
    });
  }

  Fetch(identifier: string): Effect.Effect<SkillBundle, Error> {
    return Effect.gen(this, function* () {
      let agentID = identifier;
      if (agentID.startsWith("lobehub/")) {
        agentID = agentID.substring(8);
      }

      const urlStr = `https://chat-agents.lobehub.com/${agentID}.json`;
      const res = yield* guardedHttpGet(urlStr);
      if (res.status !== 200) {
        return yield* Effect.fail(new Error(`failed to fetch lobehub agent config: status ${res.status}`));
      }

      let data: any;
      try {
        data = JSON.parse(res.body.toString("utf8"));
      } catch (err) {
        return yield* Effect.fail(err instanceof Error ? err : new Error(String(err)));
      }

      const title = data.meta?.title || agentID;
      const tags = data.meta?.tags || [];
      const tagsStr = tags.length > 0 ? `    tags: [${tags.join(", ")}]` : "";

      const fmText = `---
name: ${data.identifier}
description: ${(data.meta?.description || "").replace(/\n/g, " ")}
metadata:
  hermes:
${tagsStr}
  lobehub:
    source: lobehub
---

# ${title}

${data.meta?.description || ""}

## Instructions

${data.config?.systemRole || ""}
`;

      const files: Record<string, Uint8Array> = {
        "SKILL.md": Buffer.from(fmText, "utf8")
      };

      return {
        name: agentID,
        files,
        source: "lobehub",
        identifier,
        trust_level: "community"
      };
    });
  }
}

// ---------------------------------------------------------------------------
// Browse.sh Source Adapter
// ---------------------------------------------------------------------------

export class BrowseShSource implements SkillSource {
  SourceID(): string {
    return "browse-sh";
  }

  TrustLevelFor(identifier: string): string {
    return "community";
  }

  fetchCatalog(): Effect.Effect<any[], Error> {
    return Effect.gen(this, function* () {
      const cacheKey = "browse_sh_catalog";
      const [cached, ok] = readIndexCache(cacheKey);
      if (ok && cached) {
        try {
          const data = JSON.parse(cached.toString("utf8"));
          if (Array.isArray(data)) return data;
          if (data.skills) return data.skills;
        } catch {}
      }

      const res = yield* guardedHttpGet("https://browse.sh/api/skills");
      if (res.status !== 200) {
        return yield* Effect.fail(new Error(`failed to fetch browse.sh catalog: status ${res.status}`));
      }

      let catalog: any[] = [];
      try {
        const obj = JSON.parse(res.body.toString("utf8"));
        if (obj.skills && Array.isArray(obj.skills)) {
          catalog = obj.skills;
        } else if (Array.isArray(obj)) {
          catalog = obj;
        }
      } catch (err) {
        return yield* Effect.fail(err instanceof Error ? err : new Error(String(err)));
      }

      writeIndexCache(cacheKey, res.body);
      return catalog;
    });
  }

  Search(query: string, limit: number): Effect.Effect<SkillMeta[], Error> {
    return Effect.gen(this, function* () {
      const catalog = yield* this.fetchCatalog();
      const queryLower = query.toLowerCase();
      const results: SkillMeta[] = [];

      for (const item of catalog) {
        const tags = item.tags || [];
        const text = `${item.name || ""} ${item.title || ""} ${item.description || ""} ${item.hostname || ""} ${item.category || ""} ${tags.join(" ")}`.toLowerCase();
        if (queryLower === "" || text.includes(queryLower)) {
          let desc = item.description || "";
          if (desc === "") {
            desc = item.title || "";
          }
          results.push({
            name: item.name || "",
            description: desc,
            source: "browse-sh",
            identifier: "browse-sh/" + (item.slug || ""),
            trust_level: "community",
            tags,
            extra: {
              slug: item.slug,
              hostname: item.hostname,
              category: item.category,
              source_url: item.sourceUrl,
              recommended_method: item.recommendedMethod,
              proxies: item.proxies,
              install_count: item.installCount
            }
          });
          if (results.length >= limit) {
            break;
          }
        }
      }
      return results;
    });
  }

  Inspect(identifier: string): Effect.Effect<SkillMeta, Error> {
    return Effect.gen(this, function* () {
      let slug = identifier;
      if (slug.startsWith("browse-sh/")) {
        slug = slug.substring(10);
      }

      const catalog = yield* this.fetchCatalog();
      for (const item of catalog) {
        if (item.slug === slug) {
          let desc = item.description || "";
          if (desc === "") {
            desc = item.title || "";
          }
          return {
            name: item.name || "",
            description: desc,
            source: "browse-sh",
            identifier,
            trust_level: "community",
            tags: item.tags || [],
            extra: {
              slug: item.slug,
              hostname: item.hostname,
              category: item.category,
              source_url: item.sourceUrl,
              recommended_method: item.recommendedMethod,
              proxies: item.proxies,
              install_count: item.installCount
            }
          };
        }
      }

      return yield* Effect.fail(new Error(`browse-sh skill not found: ${slug}`));
    });
  }

  Fetch(identifier: string): Effect.Effect<SkillBundle, Error> {
    return Effect.gen(this, function* () {
      const meta = yield* this.Inspect(identifier);
      const extra = meta.extra || {};
      let slug = meta.name;
      if (extra.slug) {
        slug = extra.slug;
      }

      const detailURL = `https://browse.sh/api/skills/${slug}`;
      const res = yield* guardedHttpGet(detailURL);
      if (res.status !== 200) {
        return yield* Effect.fail(new Error(`failed to fetch browse-sh detail: status ${res.status}`));
      }

      let detail: any;
      try {
        detail = JSON.parse(res.body.toString("utf8"));
      } catch (err) {
        return yield* Effect.fail(err instanceof Error ? err : new Error(String(err)));
      }

      let mdURL = detail.skillMdUrl || "";
      if (mdURL === "") {
        if (extra.source_url && extra.source_url.includes("raw.githubusercontent.com")) {
          mdURL = extra.source_url;
        }
      }

      if (mdURL === "") {
        return yield* Effect.fail(new Error("could not resolve SKILL.md URL for browse-sh"));
      }

      const mdRes = yield* guardedHttpGet(mdURL);
      if (mdRes.status !== 200) {
        return yield* Effect.fail(new Error(`failed to download SKILL.md from ${mdURL}: status ${mdRes.status}`));
      }

      const files: Record<string, Uint8Array> = {
        "SKILL.md": mdRes.body
      };

      return {
        name: meta.name,
        files,
        source: "browse-sh",
        identifier,
        trust_level: "community",
        metadata: extra
      };
    });
  }
}

// ---------------------------------------------------------------------------
// Centralized Hermes Index Source Adapter
// ---------------------------------------------------------------------------

export class HermesIndexSource implements SkillSource {
  auth: GitHubAuth;
  index: { skills: any[] } | null = null;
  loaded = false;
  github: GitHubSource | null = null;

  constructor(auth: GitHubAuth) {
    this.auth = auth;
  }

  SourceID(): string {
    return "hermes-index";
  }

  TrustLevelFor(identifier: string): string {
    if (!this.loaded) {
      const [cached, ok] = readIndexCache("hermes-index");
      if (ok && cached) {
        try {
          this.index = JSON.parse(cached.toString("utf8"));
          this.loaded = true;
        } catch {}
      }
    }
    if (this.index) {
      const skillsList = this.index.skills || [];
      for (const sk of skillsList) {
        if (sk.identifier === identifier) {
          return sk.trust_level || "community";
        }
      }
    }
    return "community";
  }

  Search(query: string, limit: number): Effect.Effect<SkillMeta[], Error> {
    return Effect.gen(this, function* () {
      yield* this.ensureLoaded();
      if (!this.index) {
        return [];
      }

      const queryLower = query.toLowerCase();
      const results: SkillMeta[] = [];
      const skillsList = this.index.skills || [];
      for (const sk of skillsList) {
        const tags = sk.tags || [];
        const searchable = `${sk.name || ""} ${sk.description || ""} ${tags.join(" ")}`.toLowerCase();
        if (queryLower === "" || searchable.includes(queryLower)) {
          results.push(this.toMeta(sk));
          if (limit > 0 && results.length >= limit) {
            break;
          }
        }
      }
      return results;
    });
  }

  Inspect(identifier: string): Effect.Effect<SkillMeta, Error> {
    return Effect.gen(this, function* () {
      yield* this.ensureLoaded();
      if (!this.index) {
        return yield* Effect.fail(new Error("hermes central index not loaded"));
      }

      const entry = this.findEntry(identifier);
      if (!entry) {
        return yield* Effect.fail(new Error(`skill not found in hermes index: ${identifier}`));
      }

      return this.toMeta(entry);
    });
  }

  Fetch(identifier: string): Effect.Effect<SkillBundle, Error> {
    return Effect.gen(this, function* () {
      yield* this.ensureLoaded();
      if (!this.index) {
        return yield* Effect.fail(new Error("hermes central index not loaded"));
      }

      const entry = this.findEntry(identifier);
      if (!entry) {
        return yield* Effect.fail(new Error(`skill not found in hermes index: ${identifier}`));
      }

      if (!this.github) {
        this.github = new GitHubSource(this.auth);
      }

      let target = entry.resolved_github_id || "";
      if (target === "" && entry.repo && entry.path) {
        target = entry.repo + "/" + entry.path;
      }

      if (target === "") {
        return yield* Effect.fail(new Error("could not resolve github ID for fetch"));
      }

      const bundle = yield* this.github.Fetch(target);
      bundle.source = entry.source || "hermes-index";
      bundle.identifier = identifier;
      return bundle;
    });
  }

  ensureLoaded(): Effect.Effect<void, Error> {
    return Effect.gen(this, function* () {
      if (this.loaded) {
        return;
      }

      const cacheKey = "hermes-index";
      const [cached, ok] = readIndexCache(cacheKey);
      if (ok && cached) {
        try {
          const idx = JSON.parse(cached.toString("utf8"));
          this.index = idx;
          this.loaded = true;
          return;
        } catch {}
      }

      const res = yield* guardedHttpGet("https://hermes-agent.nousresearch.com/docs/api/skills-index.json");
      if (res.status !== 200) {
        return yield* Effect.fail(new Error(`failed to fetch central index: status ${res.status}`));
      }

      try {
        const idx = JSON.parse(res.body.toString("utf8"));
        this.index = idx;
        this.loaded = true;
        writeIndexCache(cacheKey, res.body);
      } catch (err) {
        return yield* Effect.fail(err instanceof Error ? err : new Error(String(err)));
      }
    });
  }

  findEntry(identifier: string): any {
    if (!this.index) {
      return null;
    }
    const skillsList = this.index.skills || [];
    for (const sk of skillsList) {
      if (sk.identifier === identifier) {
        return sk;
      }
    }

    let normalized = identifier;
    const prefixes = ["skills-sh/", "skills.sh/", "official/", "github/", "clawhub/"];
    for (const prefix of prefixes) {
      if (identifier.startsWith(prefix)) {
        normalized = identifier.substring(prefix.length);
        break;
      }
    }

    for (const sk of skillsList) {
      let stored = sk.identifier || "";
      for (const prefix of prefixes) {
        if (stored.startsWith(prefix)) {
          stored = stored.substring(prefix.length);
          break;
        }
      }
      if (stored === normalized) {
        return sk;
      }
    }

    return null;
  }

  toMeta(entry: any): SkillMeta {
    return {
      name: entry.name || "",
      description: entry.description || "",
      source: entry.source || "",
      identifier: entry.identifier || "",
      trust_level: entry.trust_level || "community",
      repo: entry.repo,
      path: entry.path,
      tags: entry.tags || [],
      extra: entry.extra
    };
  }
}

// ---------------------------------------------------------------------------
// Helper: MD5 Hash
// ---------------------------------------------------------------------------

function stripHTML(val: string): string {
  return val.replace(/<[^>]+>/g, "").trim();
}

function md5Hash(val: string): string {
  return crypto.createHash("md5").update(val).digest("hex");
}

/*
PORT STATUS
source path: backend/skills/hub_adapters.go
source lines: 1961
draft lines: 1475
confidence: high
status: phase_b_compile
*/
