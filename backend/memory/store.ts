// PORT: backend/memory/store.go

import { Effect } from "effect";
import fs from "node:fs";
import path from "node:path";
import { home_dir } from "../hollowhome/home";
import * as fslockUnix from "../fslock/lock_unix";
import * as fslockWindows from "../fslock/lock_windows";
import { atomicWrite } from "../skills/usage";
import { threatPatternIDs, ScanScope, FirstThreatMessage } from "./scan";

const fslock = process.platform === "win32" ? fslockWindows : fslockUnix;

export const EntryDelimiter = "\n§\n";

export const TargetMemory = "memory";
export const TargetUser = "user";

// Dir returns the memories directory (resolved dynamically so HOLLOW_HOME
// overrides are always respected).
export function Dir(): string {
  return path.join(home_dir(), "memories");
}

// PathFor maps a target to its on-disk file.
export function PathFor(target: string): string {
  if (target === TargetUser) {
    return path.join(Dir(), "USER.md");
  }
  return path.join(Dir(), "MEMORY.md");
}

// Result is the structured outcome of a memory operation, serialized into the
// tool response.
export interface Result {
  success: boolean;
  target?: string;
  entries?: string[];
  usage?: string;
  entry_count?: number;
  message?: string;
  error?: string;
  current_entries?: string[];
  matches?: string[];
  drift_backup?: string;
  remediation?: string;
}

// Store is bounded curated memory with file persistence. One instance per
// Agent (shared with background-review forks).
//
// It maintains two parallel states:
//   - snapshot: frozen at LoadFromDisk(), used for system prompt injection.
//     Never mutated mid-session. Keeps the prefix cache stable.
//   - memoryEntries / userEntries: live state, mutated by tool calls and
//     persisted to disk immediately. Tool responses always reflect this.
export class Store {
  memoryEntries: string[] = [];
  userEntries: string[] = [];

  memoryCharLimit: number;
  userCharLimit: number;

  // snapshot holds the rendered system-prompt blocks frozen at load time.
  snapshot: Record<string, string>;

  constructor(memoryCharLimit: number, userCharLimit: number) {
    this.memoryCharLimit = memoryCharLimit <= 0 ? 2200 : memoryCharLimit;
    this.userCharLimit = userCharLimit <= 0 ? 1375 : userCharLimit;
    this.snapshot = { [TargetMemory]: "", [TargetUser]: "" };
  }

  // LoadFromDisk loads entries from MEMORY.md and USER.md and captures the
  // frozen system-prompt snapshot.
  //
  // Each entry is threat-scanned at snapshot-build time — any hit replaces the
  // entry text in the snapshot with a "[BLOCKED: …]" placeholder, so a
  // poisoned-on-disk memory file cannot inject into the system prompt. The live
  // entry lists keep the original text so the user can still SEE poisoned
  // entries via memory(action=read) and remove them — silently dropping them
  // would hide the attack from the user.
  loadFromDisk(): void {
    try {
      fs.mkdirSync(Dir(), { recursive: true, mode: 0o700 });
    } catch {
      // ignore
    }

    this.memoryEntries = dedupe(readEntries(PathFor(TargetMemory)));
    this.userEntries = dedupe(readEntries(PathFor(TargetUser)));

    this.snapshot = {
      [TargetMemory]: this.renderBlock(TargetMemory, sanitizeForSnapshot(this.memoryEntries, "MEMORY.md")),
      [TargetUser]: this.renderBlock(TargetUser, sanitizeForSnapshot(this.userEntries, "USER.md")),
    };
  }

  // FormatForSystemPrompt returns the frozen snapshot block for target.
  // This is the state captured at LoadFromDisk() time, NOT the live state —
  // mid-session writes do not affect it. Empty string when there were no
  // entries at load time.
  formatForSystemPrompt(target: string): string {
    return this.snapshot[target] ?? "";
  }

  // Add appends a new entry. Errors if it would exceed the char limit.
  add(target: string, content: string): Effect.Effect<Result, Error> {
    const trimmed = content.trim();
    if (trimmed === "") {
      return Effect.succeed({ success: false, error: "Content cannot be empty." });
    }
    const msg = FirstThreatMessage(trimmed);
    if (msg !== "") {
      return Effect.succeed({ success: false, error: msg });
    }

    return this.withFileLock(target, () => {
      const bak = this.reloadTarget(target);
      if (bak !== "") {
        return Effect.succeed(driftError(target, bak));
      }

      const entries = this.entriesFor(target);
      const limit = this.charLimit(target);

      for (const e of entries) {
        if (e === trimmed) {
          return Effect.succeed(this.successResponse(target, "Entry already exists (no duplicate added)."));
        }
      }

      const newEntries = [...entries, trimmed];
      const newTotal = newEntries.join(EntryDelimiter).length;
      if (newTotal > limit) {
        const current = this.charCount(target);
        return Effect.succeed({
          success: false,
          error: `Memory at ${formatThousands(current)}/${formatThousands(limit)} chars. Adding this entry (${trimmed.length} chars) would exceed the limit. Consolidate now: use 'replace' to merge overlapping entries into shorter ones or 'remove' stale or less important entries (see current_entries below), then retry this add — all in this turn.`,
          current_entries: entries,
          usage: `${formatThousands(current)}/${formatThousands(limit)}`,
        });
      }

      this.setEntries(target, newEntries);
      return this.saveToDisk(target).pipe(
        Effect.map(() => this.successResponse(target, "Entry added.")),
        Effect.catchAll((err) => Effect.succeed({ success: false, error: err.message }))
      );
    });
  }

  // Replace finds the entry containing oldText as a substring and replaces it
  // with newContent.
  replace(target: string, oldText: string, newContent: string): Effect.Effect<Result, Error> {
    const trimmedOld = oldText.trim();
    const trimmedNew = newContent.trim();
    if (trimmedOld === "") {
      return Effect.succeed({ success: false, error: "match text cannot be empty." });
    }
    if (trimmedNew === "") {
      return Effect.succeed({ success: false, error: "replacement cannot be empty. Use 'remove' to delete entries." });
    }
    const msg = FirstThreatMessage(trimmedNew);
    if (msg !== "") {
      return Effect.succeed({ success: false, error: msg });
    }

    return this.withFileLock(target, () => {
      const bak = this.reloadTarget(target);
      if (bak !== "") {
        return Effect.succeed(driftError(target, bak));
      }

      const entries = this.entriesFor(target);
      const [idx, errRes] = matchSingle(entries, trimmedOld);
      if (errRes !== null) {
        return Effect.succeed(errRes);
      }

      const limit = this.charLimit(target);
      const test = [...entries];
      test[idx] = trimmedNew;
      const newTotal = test.join(EntryDelimiter).length;
      if (newTotal > limit) {
        const current = this.charCount(target);
        return Effect.succeed({
          success: false,
          error: `Replacement would put memory at ${formatThousands(newTotal)}/${formatThousands(limit)} chars. Shorten the new content, or 'remove' other stale or less important entries to make room (see current_entries below), then retry — all in this turn.`,
          current_entries: entries,
          usage: `${formatThousands(current)}/${formatThousands(limit)}`,
        });
      }

      this.setEntries(target, test);
      return this.saveToDisk(target).pipe(
        Effect.map(() => this.successResponse(target, "Entry replaced.")),
        Effect.catchAll((err) => Effect.succeed({ success: false, error: err.message }))
      );
    });
  }

  // Remove deletes the entry containing oldText as a substring.
  remove(target: string, oldText: string): Effect.Effect<Result, Error> {
    const trimmedOld = oldText.trim();
    if (trimmedOld === "") {
      return Effect.succeed({ success: false, error: "match text cannot be empty." });
    }

    return this.withFileLock(target, () => {
      const bak = this.reloadTarget(target);
      if (bak !== "") {
        return Effect.succeed(driftError(target, bak));
      }

      const entries = this.entriesFor(target);
      const [idx, errRes] = matchSingle(entries, trimmedOld);
      if (errRes !== null) {
        return Effect.succeed(errRes);
      }

      const test = [...entries];
      test.splice(idx, 1);
      this.setEntries(target, test);
      return this.saveToDisk(target).pipe(
        Effect.map(() => this.successResponse(target, "Entry removed.")),
        Effect.catchAll((err) => Effect.succeed({ success: false, error: err.message }))
      );
    });
  }

  // Read returns the live state for the target (not the frozen snapshot).
  read(target: string): Result {
    return this.successResponse(target, "");
  }

  // -- internal helpers --

  entriesFor(target: string): string[] {
    if (target === TargetUser) {
      return this.userEntries;
    }
    return this.memoryEntries;
  }

  setEntries(target: string, entries: string[]): void {
    if (target === TargetUser) {
      this.userEntries = entries;
    } else {
      this.memoryEntries = entries;
    }
  }

  charLimit(target: string): number {
    if (target === TargetUser) {
      return this.userCharLimit;
    }
    return this.memoryCharLimit;
  }

  charCount(target: string): number {
    const entries = this.entriesFor(target);
    if (entries.length === 0) {
      return 0;
    }
    return entries.join(EntryDelimiter).length;
  }

  successResponse(target: string, message: string): Result {
    const entries = this.entriesFor(target);
    const current = this.charCount(target);
    const limit = this.charLimit(target);
    let pct = 0;
    if (limit > 0) {
      pct = Math.floor((current * 100) / limit);
      if (pct > 100) {
        pct = 100;
      }
    }
    return {
      success: true,
      target,
      entries: [...entries],
      usage: `${pct}% — ${formatThousands(current)}/${formatThousands(limit)} chars`,
      entry_count: entries.length,
      message,
    };
  }

  // renderBlock renders a system-prompt block with header and usage indicator.
  renderBlock(target: string, entries: string[]): string {
    if (entries.length === 0) {
      return "";
    }
    const limit = this.charLimit(target);
    const content = entries.join(EntryDelimiter);
    const current = content.length;
    let pct = 0;
    if (limit > 0) {
      pct = Math.floor((current * 100) / limit);
      if (pct > 100) {
        pct = 100;
      }
    }
    let header = "";
    if (target === TargetUser) {
      header = `USER PROFILE (who the user is) [${pct}% — ${formatThousands(current)}/${formatThousands(limit)} chars]`;
    } else {
      header = `MEMORY (your personal notes) [${pct}% — ${formatThousands(current)}/${formatThousands(limit)} chars]`;
    }
    const separator = "═".repeat(46);
    return `${separator}\n${header}\n${separator}\n${content}`;
  }

  // reloadTarget re-reads entries from disk into the live state (under the file
  // lock, so other-session writes are picked up before mutating). Returns the
  // backup path when external drift was detected — the caller must abort the
  // mutation, since flushing would discard the un-roundtrippable content.
  reloadTarget(target: string): string {
    const bak = this.detectExternalDrift(target);
    const fresh = dedupe(readEntries(PathFor(target)));
    this.setEntries(target, fresh);
    return bak;
  }

  saveToDisk(target: string): Effect.Effect<void, Error> {
    const entries = this.entriesFor(target);
    const content = entries.join(EntryDelimiter);
    const contentBytes = Buffer.from(content, "utf8");
    return atomicWrite(PathFor(target), contentBytes).pipe(
      Effect.mapError((err) => new Error(`failed to write memory file ${PathFor(target)}: ${err.message}`))
    );
  }

  // detectExternalDrift returns a backup-path string when the on-disk content
  // shows external drift: either a round-trip mismatch (re-parsing and
  // re-serializing doesn't reproduce the bytes) or a single parsed entry
  // exceeding the store's whole-file char limit (an external writer appended
  // free-form content). The file is snapshotted to .bak.<ts> so the operator
  // can recover whatever the external writer added.
  detectExternalDrift(target: string): string {
    const p = PathFor(target);
    let data: string;
    try {
      data = fs.readFileSync(p, "utf8");
    } catch {
      return "";
    }
    if (data.trim() === "") {
      return "";
    }

    const parsed = readEntriesFromString(data);
    const roundtrip = parsed.join(EntryDelimiter);

    const limit = this.charLimit(target);
    let maxEntryLen = 0;
    for (const e of parsed) {
      if (e.length > maxEntryLen) {
        maxEntryLen = e.length;
      }
    }

    if (data.trim() === roundtrip && maxEntryLen <= limit) {
      return "";
    }

    const unixTime = Math.floor(Date.now() / 1000);
    const bakPath = `${p}.bak.${unixTime}`;
    try {
      fs.writeFileSync(bakPath, data, { mode: 0o600 });
      return bakPath;
    } catch {
      return bakPath + " (BACKUP FAILED — file unchanged on disk)";
    }
  }

  // withFileLock acquires an exclusive lock on a sidecar .lock file for the
  // duration of a read-modify-write so concurrent sessions don't clobber each
  // other. The memory file itself is replaced atomically.
  withFileLock(
    target: string,
    f: () => Effect.Effect<Result, Error>
  ): Effect.Effect<Result, Error> {
    const p = PathFor(target);
    const lockPath = p + ".lock";
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
      ({ fd }) => {
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
}

export function readEntries(filePath: string): string[] {
  try {
    const data = fs.readFileSync(filePath, "utf8");
    return readEntriesFromString(data);
  } catch {
    return [];
  }
}

export function readEntriesFromString(raw: string): string[] {
  if (raw.trim() === "") {
    return [];
  }
  const out: string[] = [];
  for (let e of raw.split(EntryDelimiter)) {
    e = e.trim();
    if (e !== "") {
      out.push(e);
    }
  }
  return out;
}

export function dedupe(entries: string[]): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const e of entries) {
    if (!seen.has(e)) {
      seen.add(e);
      out.push(e);
    }
  }
  return out;
}

export function formatThousands(n: number): string {
  const s = String(n);
  if (s.length <= 3) {
    return s;
  }
  const parts: string[] = [];
  let current = s;
  while (current.length > 3) {
    parts.unshift(current.slice(-3));
    current = current.slice(0, -3);
  }
  parts.unshift(current);
  return parts.join(",");
}

export function sanitizeForSnapshot(entries: string[], filename: string): string[] {
  const out: string[] = [];
  for (const entry of entries) {
    if (entry === "" || entry.startsWith("[BLOCKED:")) {
      out.push(entry);
      continue;
    }
    const ids = threatPatternIDs(entry, ScanScope.ScopeStrict);
    if (ids.length > 0) {
      out.push(
        `[BLOCKED: ${filename} entry contained threat pattern(s): ${ids.join(", ")}. Removed from system prompt; use memory(action=read) to inspect and memory(action=remove) to delete the original.]`
      );
    } else {
      out.push(entry);
    }
  }
  return out;
}

export function driftError(target: string, bakPath: string): Result {
  const name = path.basename(PathFor(target));
  return {
    success: false,
    error: `Refusing to write ${name}: file on disk has content that wouldn't round-trip through the memory tool (likely added by a manual edit, a shell append, or a concurrent session). A snapshot was saved to ${bakPath}. Resolve the drift first — either rewrite the file as a clean §-delimited list of entries, or move the extra content out — then retry. This guard exists to prevent silent data loss.`,
    drift_backup: bakPath,
    remediation: "Open the .bak file, integrate the missing entries into the memory tool one at a time via memory(action=add, content=...), then remove or rewrite the original file to a clean state.",
  };
}

export function matchSingle(entries: string[], needle: string): [number, Result | null] {
  const idxs: number[] = [];
  for (let i = 0; i < entries.length; i++) {
    if (entries[i].includes(needle)) {
      idxs.push(i);
    }
  }
  if (idxs.length === 0) {
    return [0, { success: false, error: `No entry matched '${needle}'.` }];
  }
  if (idxs.length > 1) {
    const unique = new Set<string>();
    for (const i of idxs) {
      unique.add(entries[i]);
    }
    if (unique.size > 1) {
      const previews: string[] = [];
      for (const i of idxs) {
        let e = entries[i];
        if (e.length > 80) {
          e = e.slice(0, 80) + "...";
        }
        previews.push(e);
      }
      return [
        0,
        {
          success: false,
          error: `Multiple entries matched '${needle}'. Be more specific.`,
          matches: previews,
        },
      ];
    }
  }
  return [idxs[0], null];
}

/*
PORT STATUS
source path: backend/memory/store.go
source lines: 592
draft lines: 521
confidence: high
status: phase_b_compile
*/
