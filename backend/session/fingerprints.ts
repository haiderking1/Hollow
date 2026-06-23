// PORT: backend/session/fingerprints.go

import fs from "node:fs";

// FileFingerprint records the content hash a path had after the agent's last
// successful mutation of it. Paths and hashes only — never file contents.
export type file_fingerprint = {
  path: string; // absolute path
  after_hash: string; // hex SHA256 after the mutation
  kind: string; // write_file | edit_file
  turn_id: string;
  timestamp: Date;
};

// FingerprintStore holds the session-wide latest fingerprint per path (last
// mutation wins, across branches) and persists it to a sidecar file next to
// the session JSONL. The sidecar is independent of the message log, so
// compaction never touches it; loading a session loads its sidecar.
export class fingerprint_store {
  private path_fn: () => string; // current sidecar path; "" until session flushes
  private by_path = new Map<string, file_fingerprint>();
  private loaded_from = "";

  constructor(path_fn: () => string) {
    this.path_fn = path_fn;
  }

  private sync_locked() {
    const p = sidecar_path(this.path_fn());
    if (p === this.loaded_from) {
      return;
    }
    const pending = new Map(this.by_path);
    const carry_over = this.loaded_from === "" && pending.size > 0;

    this.loaded_from = p;
    this.by_path.clear();
    if (p === "") {
      return;
    }

    try {
      if (fs.existsSync(p)) {
        const data = fs.readFileSync(p, "utf8");
        const parsed = JSON.parse(data) as Record<string, any>;
        for (const [k, v] of Object.entries(parsed)) {
          this.by_path.set(k, {
            path: v.path ?? "",
            after_hash: v.after_hash ?? "",
            kind: v.kind ?? "",
            turn_id: v.turn_id ?? "",
            timestamp: v.timestamp ? new Date(v.timestamp) : new Date(),
          });
        }
      }
    } catch {}

    if (carry_over) {
      for (const [k, v] of pending.entries()) {
        this.by_path.set(k, v);
      }
    }
  }

  upsert(fp: file_fingerprint) {
    if (fp.path === "" || fp.after_hash === "") {
      return;
    }

    this.sync_locked();
    this.by_path.set(fp.path, fp);

    if (this.loaded_from === "") {
      // Session not flushed yet; retry persisting on a later upsert/list.
      return;
    }

    try {
      const obj: Record<string, any> = {};
      for (const [k, v] of this.by_path.entries()) {
        obj[k] = {
          ...v,
          timestamp: v.timestamp.toISOString(),
        };
      }
      const data = JSON.stringify(obj, null, " ");
      fs.writeFileSync(this.loaded_from, data, { mode: 0o600 });
    } catch {}
  }

  list(): file_fingerprint[] {
    this.sync_locked();
    return Array.from(this.by_path.values());
  }
}

// NewFingerprintStore creates a store whose sidecar location is resolved
// lazily via pathFn (the session file may not exist yet).
export const new_fingerprint_store = (path_fn: () => string): fingerprint_store => {
  return new fingerprint_store(path_fn);
};

// sidecarPath derives the fingerprint file from a session JSONL path.
export const sidecar_path = (session_file: string): string => {
  if (session_file === "") {
    return "";
  }
  if (session_file.endsWith(".jsonl")) {
    return session_file.substring(0, session_file.length - ".jsonl".length) + ".fingerprints.json";
  }
  return session_file + ".fingerprints.json";
};

/*
PORT STATUS
source path: backend/session/fingerprints.go
source lines: 112
draft lines: 115
confidence: high
status: phase_b_compile
*/
