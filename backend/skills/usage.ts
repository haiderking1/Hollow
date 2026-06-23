// PORT: backend/skills/usage.go

import { Effect } from "effect";
import fs from "node:fs";
import path from "node:path";
import { SkillsDir, ArchiveDir } from "./paths";

export interface UsageRecord {
  created_by: string | null;
  use_count: number;
  view_count: number;
  last_used_at: string | null;
  last_viewed_at: string | null;
  patch_count: number;
  last_patched_at: string | null;
  created_at: string;
  state: string;
  pinned: boolean;
  archived_at: string | null;
}

export type UsageMap = Record<string, UsageRecord>;

export interface UsageReportRow {
  name: string;
  created_by: string | null;
  use_count: number;
  view_count: number;
  last_used_at: string | null;
  last_viewed_at: string | null;
  patch_count: number;
  last_patched_at: string | null;
  created_at: string;
  state: string;
  pinned: boolean;
  archived_at: string | null;
  last_activity_at: string | null;
  activity_count: number;
}

function usageFilePath(): string {
  return path.join(SkillsDir(), ".usage.json");
}

function nowIso(): string {
  return new Date().toISOString();
}

function emptyRecord(): UsageRecord {
  return {
    created_by: null,
    use_count: 0,
    view_count: 0,
    last_used_at: null,
    last_viewed_at: null,
    patch_count: 0,
    last_patched_at: null,
    created_at: nowIso(),
    state: "active",
    pinned: false,
    archived_at: null,
  };
}

export function parseIso(value: string | null): [Date, boolean] {
  if (!value) {
    return [new Date(0), false];
  }
  const t = Date.parse(value);
  if (isNaN(t)) {
    return [new Date(0), false];
  }
  return [new Date(t), true];
}

export function LatestActivityAt(record: UsageRecord): string | null {
  let latestTime = 0;
  let latestRaw: string | null = null;

  const fields = [record.last_used_at, record.last_viewed_at, record.last_patched_at];
  for (const raw of fields) {
    if (raw) {
      const [t, ok] = parseIso(raw);
      if (ok && t.getTime() > latestTime) {
        latestTime = t.getTime();
        latestRaw = raw;
      }
    }
  }
  return latestRaw;
}

export function ActivityCount(record: UsageRecord): number {
  return record.use_count + record.view_count + record.patch_count;
}

export function LoadUsage(): UsageMap {
  const filePath = usageFilePath();
  try {
    const dataBytes = fs.readFileSync(filePath);
    const parsed = JSON.parse(dataBytes.toString());
    const um: UsageMap = {};
    for (const [k, v] of Object.entries(parsed)) {
      um[k] = v as UsageRecord;
    }
    return um;
  } catch {
    return {};
  }
}

export function SaveUsage(um: UsageMap): void {
  const filePath = usageFilePath();
  const keys = Object.keys(um).sort();

  const sorted: UsageMap = {};
  for (const k of keys) {
    sorted[k] = um[k];
  }

  try {
    const dataBytes = Buffer.from(JSON.stringify(sorted, null, "  "));
    atomicWriteSync(filePath, dataBytes);
  } catch {}
}

export function atomicWrite(filename: string, data: Uint8Array): Effect.Effect<void, Error> {
  return Effect.try({
    try: () => {
      atomicWriteSync(filename, data);
    },
    catch: (cause) => (cause instanceof Error ? cause : new Error(String(cause))),
  });
}

function atomicWriteSync(filename: string, data: Uint8Array): void {
  const dir = path.dirname(filename);
  if (!fs.existsSync(dir)) {
    fs.mkdirSync(dir, { recursive: true, mode: 0o700 });
  }

  // Generate a random temporary filename in the same directory to guarantee atomicity on rename
  const tmpName = path.join(dir, `hollow-atomic-${Math.random().toString(36).substring(2, 10)}`);
  try {
    fs.writeFileSync(tmpName, data, { mode: 0o644 });
    fs.renameSync(tmpName, filename);
  } catch (err) {
    try {
      fs.unlinkSync(tmpName);
    } catch {}
    throw err;
  }
}

function mutateUsage(name: string, f: (rec: UsageRecord) => void): void {
  if (name === "") {
    return;
  }
  // Mutex lock is not needed in JS for sync I/O, but we serialize the mutation
  const um = LoadUsage();
  let rec = um[name];
  if (!rec) {
    rec = emptyRecord();
  }
  f(rec);
  um[name] = rec;
  SaveUsage(um);
}

export function BumpView(name: string): void {
  mutateUsage(name, (rec) => {
    rec.view_count++;
    rec.last_viewed_at = nowIso();
  });
}

export function BumpUse(name: string): void {
  mutateUsage(name, (rec) => {
    rec.use_count++;
    rec.last_used_at = nowIso();
  });
}

export function BumpPatch(name: string): void {
  mutateUsage(name, (rec) => {
    rec.patch_count++;
    rec.last_patched_at = nowIso();
  });
}

export function MarkAgentCreated(name: string): void {
  mutateUsage(name, (rec) => {
    rec.created_by = "agent";
  });
}

export function Forget(name: string): void {
  if (name === "") {
    return;
  }
  const um = LoadUsage();
  if (um[name]) {
    delete um[name];
    SaveUsage(um);
  }
}

export function SetState(name: string, state: string): void {
  const valid = new Set(["active", "stale", "archived"]);
  if (!valid.has(state)) {
    return;
  }
  mutateUsage(name, (rec) => {
    rec.state = state;
    if (state === "archived") {
      rec.archived_at = nowIso();
    } else if (state === "active") {
      rec.archived_at = null;
    }
  });
}

export function SetPinned(name: string, pinned: boolean): void {
  mutateUsage(name, (rec) => {
    rec.pinned = pinned;
  });
}

export function PinSkill(name: string): void {
  SetPinned(name, true);
}

export function UnpinSkill(name: string): void {
  SetPinned(name, false);
}

export function findSkillDir(name: string): string {
  const skillsRoot = SkillsDir();
  if (!fs.existsSync(skillsRoot)) {
    return "";
  }
  let foundDir = "";

  const walk = (dir: string): boolean => {
    let entries: string[];
    try {
      entries = fs.readdirSync(dir);
    } catch {
      return false;
    }

    for (const entry of entries) {
      const full = path.join(dir, entry);
      let stat: fs.Stats;
      try {
        stat = fs.statSync(full);
      } catch {
        continue;
      }

      if (stat.isDirectory()) {
        if (entry === ".archive") {
          continue;
        }
        if (entry === name) {
          if (fs.existsSync(path.join(full, "SKILL.md"))) {
            foundDir = full;
            return true;
          }
        }
        if (walk(full)) {
          return true;
        }
      }
    }
    return false;
  };

  walk(skillsRoot);
  return foundDir;
}

export function IsAgentCreated(name: string): boolean {
  const um = LoadUsage();
  const rec = um[name];
  if (!rec || rec.created_by !== "agent") {
    return false;
  }
  return findSkillDir(name) !== "";
}

export function ListAgentCreatedSkillNames(): string[] {
  const um = LoadUsage();
  const names: string[] = [];
  for (const [k, v] of Object.entries(um)) {
    if (v.created_by === "agent") {
      if (findSkillDir(k) !== "") {
        names.push(k);
      }
    }
  }
  names.sort();
  return names;
}

export function AgentCreatedReport(): UsageReportRow[] {
  const um = LoadUsage();
  const rows: UsageReportRow[] = [];
  for (const name of ListAgentCreatedSkillNames()) {
    const rec = um[name];
    rows.push({
      name,
      created_by: rec.created_by,
      use_count: rec.use_count,
      view_count: rec.view_count,
      last_used_at: rec.last_used_at,
      last_viewed_at: rec.last_viewed_at,
      patch_count: rec.patch_count,
      last_patched_at: rec.last_patched_at,
      created_at: rec.created_at,
      state: rec.state,
      pinned: rec.pinned,
      archived_at: rec.archived_at,
      last_activity_at: LatestActivityAt(rec),
      activity_count: ActivityCount(rec),
    });
  }
  return rows;
}

export function ListArchivedSkillNames(): string[] {
  const root = ArchiveDir();
  if (!fs.existsSync(root)) {
    return [];
  }
  try {
    const entries = fs.readdirSync(root, { withFileTypes: true });
    const names: string[] = [];
    for (const entry of entries) {
      if (entry.isDirectory()) {
        names.push(entry.name);
      }
    }
    names.sort();
    return names;
  } catch {
    return [];
  }
}

export function ArchiveSkill(name: string): Effect.Effect<[boolean, string], Error> {
  return Effect.try({
    try: () => {
      const skillDir = findSkillDir(name);
      if (skillDir === "") {
        return [false, `skill '${name}' not found`] as [boolean, string];
      }
      const root = ArchiveDir();
      if (!fs.existsSync(root)) {
        fs.mkdirSync(root, { recursive: true, mode: 0o700 });
      }
      let dest = path.join(root, name);
      if (fs.existsSync(dest)) {
        const ts = new Date().toISOString().replace(/[-:T]/g, "").split(".")[0];
        dest = path.join(root, `${name}-${ts}`);
      }
      fs.renameSync(skillDir, dest);
      SetState(name, "archived");
      return [true, `archived to ${dest}`] as [boolean, string];
    },
    catch: (cause) => (cause instanceof Error ? cause : new Error(String(cause))),
  });
}

export function RestoreSkill(name: string): Effect.Effect<[boolean, string], Error> {
  return Effect.try({
    try: () => {
      const root = ArchiveDir();
      if (!fs.existsSync(root)) {
        return [false, "no archive directory"] as [boolean, string];
      }
      let src = "";
      const exact = path.join(root, name);
      try {
        const fi = fs.statSync(exact);
        if (fi.isDirectory()) {
          src = exact;
        }
      } catch {}

      if (src === "") {
        const archived = ListArchivedSkillNames();
        let newest = "";
        for (const n of archived) {
          if (n.startsWith(name + "-")) {
            if (n > newest) {
              newest = n;
            }
          }
        }
        if (newest !== "") {
          src = path.join(root, newest);
        }
      }

      if (src === "") {
        return [false, `skill '${name}' not found in archive`] as [boolean, string];
      }
      const dest = path.join(SkillsDir(), name);
      if (fs.existsSync(dest)) {
        return [false, `destination already exists: ${dest}`] as [boolean, string];
      }
      fs.renameSync(src, dest);
      SetState(name, "active");
      return [true, `restored to ${dest}`] as [boolean, string];
    },
    catch: (cause) => (cause instanceof Error ? cause : new Error(String(cause))),
  });
}

/*
PORT STATUS
source path: backend/skills/usage.go
source lines: 411
draft lines: 409
confidence: high
status: phase_b_compile
*/
