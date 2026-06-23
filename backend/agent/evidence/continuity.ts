// PORT: backend/agent/evidence/continuity.go
// backend/agent/evidence/continuity.go

import {
  ledger,
  hash_bytes,
  read_file_payload,
  read_source_continuity,
  kind_read_file,
} from "./ledger";
import { Effect } from "effect";
import fs from "node:fs";

// Fingerprint is the cross-turn record of an agent mutation: the content
// hash a path had after this agent last successfully wrote or edited it.
export type fingerprint = {
  path: string; // absolute path
  after_hash: string; // hex SHA256 after the mutation
};

// SeedContinuityReads grants read credit for agent-authored paths whose
// on-disk content still matches the last recorded mutation fingerprint. The
// guard stays hostile to everything else: a missing file, a hash mismatch
// (external edit), or a path the agent never wrote gets no credit and still
// requires a real read_file this turn. Returns the number of seeded entries.
export const seed_continuity_reads = (ledger: ledger | null, fps: fingerprint[]): number => {
  if (ledger === null) {
    return 0;
  }

  let n = 0;
  for (const fp of fps) {
    if (fp.path === "" || fp.after_hash === "") {
      continue;
    }

    let data: Uint8Array;
    try {
      data = read_file_sync(fp.path);
    } catch {
      continue; // deleted or unreadable: no credit
    }

    if (hash_bytes(data) !== fp.after_hash) {
      continue; // external edit: forces a real read
    }

    const text = new TextDecoder().decode(data);
    let lines = 0;
    for (const c of text) {
      if (c === "\n") {
        lines++;
      }
    }
    if (data.length > 0 && !text.endsWith("\n")) {
      lines++;
    }

    const append_effect = ledger.append(kind_read_file, {
      path: fp.path,
      content_hash: fp.after_hash,
      line_count: lines,
      source: read_source_continuity,
    } as read_file_payload);

    try {
      Effect.runSync(Effect.ignore(append_effect));
      n++;
    } catch {
      // ignore append failures, matching Go's err == nil guard
    }
  }

  return n;
};

// Real synchronous file read using node:fs readFileSync.
const read_file_sync = (p: string): Uint8Array => {
  return fs.readFileSync(p);
};

/*
PORT STATUS
source path: backend/agent/evidence/continuity.go
source lines: 50
draft lines: 95
confidence: high
status: phase_b_compile
*/
