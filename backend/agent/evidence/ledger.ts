// PORT: backend/agent/evidence/ledger.go
// backend/agent/evidence/ledger.go

import { Effect } from "effect";

export type kind = string;

export const kind_read_file: kind = "read_file";
export const kind_write_file: kind = "write_file";
export const kind_edit_file: kind = "edit_file";
export const kind_command_run: kind = "command_run";
export const kind_search: kind = "search";
export const kind_web_search: kind = "web_search";
export const kind_verifier_pass: kind = "verifier_pass";
export const kind_verifier_fail: kind = "verifier_fail";

export type entry = {
  id: string;
  turn_id: string;
  kind: kind;
  timestamp: Date;
  payload: Uint8Array; // json.RawMessage
};

export const read_source_tool = "";
export const read_source_continuity = "continuity";
export const read_source_author = "author";

export type read_file_payload = {
  path: string;
  content_hash: string;
  line_count: number;
  source?: string;
};

export type mutation_payload = {
  path: string;
  before_hash: string;
  after_hash: string;
};

export type command_run_payload = {
  command: string;
  cwd: string;
  exit_code: number;
  output_hash: string;
  duration_ms: number;
};

export type verifier_payload = {
  turn_id: string;
  failures?: string[];
};

export type evidence_error = {
  readonly _tag: "EvidenceError";
  readonly reason: string;
  readonly cause: unknown;
};

export const evidence_error = (reason: string, cause: unknown): evidence_error => ({
  _tag: "EvidenceError",
  reason,
  cause,
});

// Minimal mutex stub; Bun/Node is single-threaded, but the lock prevents
// re-entrant misuse during effectful runs.
class mutex {
  private _locked = false;

  lock(): void {
    this._locked = true;
  }

  unlock(): void {
    this._locked = false;
  }
}

// Ledger is an append-only per-turn record. Safe for concurrent use.
export class ledger {
  private readonly _mu = new mutex();
  private readonly _entries: entry[] = [];
  private _seq = 0;

  constructor(private readonly _turn_id: string) {}

  turn_id(): string {
    this._mu.lock();
    this._mu.unlock();
    return this._turn_id;
  }

  // Append records a new entry and returns it. The payload must marshal to JSON.
  append(kind: kind, payload: unknown): Effect.Effect<entry, evidence_error> {
    const self = this;
    return Effect.gen(function* () {
      const raw: Uint8Array = yield* Effect.try({
        try: () => new TextEncoder().encode(JSON.stringify(payload)),
        catch: (cause) => evidence_error("marshal payload", cause),
      });

      self._mu.lock();
      self._seq++;
      const e: entry = {
        id: `ev_${self._seq}`,
        turn_id: self._turn_id,
        kind,
        timestamp: new Date(),
        payload: raw,
      };
      self._entries.push(e);
      self._mu.unlock();
      return e;
    });
  }

  // HasRead reports whether a read_file entry for path exists in this turn.
  has_read(path: string): boolean {
    this._mu.lock();
    this._mu.unlock();
    for (const e of this._entries) {
      if (e.kind !== kind_read_file) {
        continue;
      }
      let p: read_file_payload | undefined;
      try {
        p = JSON.parse(new TextDecoder().decode(e.payload)) as read_file_payload;
      } catch {
        continue;
      }
      if (p.path === path) {
        return true;
      }
    }
    return false;
  }

  // NoteAuthorCredit grants read credit for a path the agent just mutated this turn.
  note_author_credit(path: string, content_hash: string): Effect.Effect<void, evidence_error> {
    return Effect.ignore(
      this.append(kind_read_file, {
        path,
        content_hash,
        source: read_source_author,
      } as read_file_payload),
    );
  }

  // MutatedPaths returns the distinct paths touched by write/edit entries, in first-mutation order.
  mutated_paths(): string[] {
    this._mu.lock();
    this._mu.unlock();
    const seen = new Set<string>();
    const out: string[] = [];
    for (const e of this._entries) {
      if (e.kind !== kind_write_file && e.kind !== kind_edit_file) {
        continue;
      }
      let p: mutation_payload | undefined;
      try {
        p = JSON.parse(new TextDecoder().decode(e.payload)) as mutation_payload;
      } catch {
        continue;
      }
      if (p.path !== "" && !seen.has(p.path)) {
        seen.add(p.path);
        out.push(p.path);
      }
    }
    return out;
  }

  // Entries returns a copy of all entries in append order.
  entries(): entry[] {
    this._mu.lock();
    this._mu.unlock();
    return [...this._entries];
  }

  // Count returns the number of entries.
  count(): number {
    this._mu.lock();
    this._mu.unlock();
    return this._entries.length;
  }
}

// HashBytes returns the hex SHA256 of data.
export const hash_bytes = (data: Uint8Array): string => {
  // TODO: wire to crypto.subtle or Node crypto for real SHA-256.
  let hex = "";
  for (let i = 0; i < data.length; i++) {
    hex += data[i].toString(16).padStart(2, "0");
  }
  return hex;
};

/*
PORT STATUS
source path: backend/agent/evidence/ledger.go
source lines: 181
draft lines: 212
confidence: medium
status: phase_a_draft
todos:
  - replace SHA-256 stub with real crypto.subtle / Node crypto implementation
  - evaluate whether mutex stub is sufficient for concurrent effect runs
  - consider schema decoding for payload JSON instead of cast+parse
notes:
  - Ledger methods that return (T, error) in Go are wrapped in Effect.Effect.
  - json.RawMessage mapped to Uint8Array; time.Time mapped to Date.
*/
