// PORT: backend/approval/write_approval.go

import fs from "node:fs";
import path from "node:path";
import crypto from "node:crypto";
import { Effect } from "effect";
import { home_dir } from "../hollowhome/home";
import type { runtime } from "../config/config";

export const subsystem_memory = "memory";
export const subsystem_skills = "skills";

export type approval_error = {
  readonly _tag: "ApprovalError";
  readonly reason: string;
  readonly cause: unknown;
};

export const approval_error = (reason: string, cause: unknown): approval_error => ({
  _tag: "ApprovalError",
  reason,
  cause,
});

export type pending_record = {
  id: string;
  subsystem: string;
  action: string;
  summary: string;
  origin: string;
  created_at: number;
  payload: Record<string, unknown>;
};

export const pending_dir = (subsystem: string): string =>
  path.join(home_dir(), "pending", subsystem);

const generate_id = (): string => crypto.randomBytes(4).toString("hex");

export const stage_write = (
  subsystem: string,
  payload: Record<string, unknown>,
  summary: string,
  origin: string,
): Effect.Effect<pending_record, approval_error> =>
  Effect.gen(function* () {
    const pid = generate_id();
    const record: pending_record = {
      id: pid,
      subsystem,
      action: get_string_field(payload, "action"),
      summary: summary.trim(),
      origin,
      created_at: Date.now() / 1000,
      payload,
    };

    const d = pending_dir(subsystem);
    yield* Effect.try({
      try: () => fs.mkdirSync(d, { recursive: true, mode: 0o700 }),
      catch: (cause) => approval_error("mkdir pending dir", cause),
    });

    const p = path.join(d, `${pid}.json`);
    const tmp_path = `${p}.tmp`;
    const data = yield* Effect.try({
      try: () => JSON.stringify(record, null, "  "),
      catch: (cause) => approval_error("marshal pending record", cause),
    });

    yield* Effect.try({
      try: () => fs.writeFileSync(tmp_path, data, { mode: 0o600 }),
      catch: (cause) => approval_error("write pending tmp", cause),
    });

    yield* Effect.try({
      try: () => fs.renameSync(tmp_path, p),
      catch: (cause) => {
        try { fs.rmSync(tmp_path); } catch {}
        return approval_error("rename pending record", cause);
      },
    });

    return record;
  });

export const list_pending = (
  subsystem: string,
): Effect.Effect<pending_record[], approval_error> =>
  Effect.gen(function* () {
    const d = pending_dir(subsystem);
    if (!fs.existsSync(d)) {
      return [];
    }

    const entries = yield* Effect.try({
      try: () => fs.readdirSync(d, { withFileTypes: true }),
      catch: (cause) => approval_error("read pending dir", cause),
    });

    const records: pending_record[] = [];
    for (const entry of entries) {
      if (entry.isDirectory() || !entry.name.endsWith(".json")) {
        continue;
      }
      try {
        const data = fs.readFileSync(path.join(d, entry.name), "utf8");
        records.push(JSON.parse(data) as pending_record);
      } catch {
        continue;
      }
    }

    records.sort((a, b) => a.created_at - b.created_at);
    return records;
  });

// PendingTotalCount returns staged skill + memory writes awaiting approval.
export const pending_total_count = (): number => {
  let total = 0;
  for (const sub of [subsystem_skills, subsystem_memory]) {
    try {
      total += fs
        .readdirSync(pending_dir(sub), { withFileTypes: true })
        .filter((entry) => !entry.isDirectory() && entry.name.endsWith(".json")).length;
    } catch {
      continue;
    }
  }
  return total;
};

export const get_pending = (
  subsystem: string,
  pending_id: string,
): Effect.Effect<pending_record | null, approval_error> =>
  Effect.gen(function* () {
    const p = path.join(pending_dir(subsystem), `${pending_id}.json`);
    if (!fs.existsSync(p)) {
      return null;
    }
    const data = yield* Effect.try({
      try: () => fs.readFileSync(p, "utf8"),
      catch: (cause) => approval_error("read pending record", cause),
    });
    return yield* Effect.try({
      try: () => JSON.parse(data) as pending_record,
      catch: (cause) => approval_error("decode pending record", cause),
    });
  });

export const discard_pending = (
  subsystem: string,
  pending_id: string,
): Effect.Effect<boolean, approval_error> =>
  Effect.gen(function* () {
    const p = path.join(pending_dir(subsystem), `${pending_id}.json`);
    if (!fs.existsSync(p)) {
      return false;
    }
    yield* Effect.try({
      try: () => fs.rmSync(p),
      catch: (cause) => approval_error("discard pending", cause),
    });
    return true;
  });

export const pending_count = (subsystem: string): number => {
  try {
    return fs
      .readdirSync(pending_dir(subsystem), { withFileTypes: true })
      .filter((entry) => !entry.isDirectory() && entry.name.endsWith(".json")).length;
  } catch {
    return 0;
  }
};

export const write_approval_enabled = (subsystem: string, cfg: runtime): boolean => {
  if (subsystem === subsystem_skills) {
    return cfg.skills.write_approval;
  }
  if (subsystem === subsystem_memory) {
    return cfg.memory.write_approval;
  }
  return false;
};

export type gate_result = {
  allow: boolean;
  blocked: boolean;
  stage: boolean;
  message: string;
};

export const evaluate_gate = (
  subsystem: string,
  is_background: boolean,
  cfg: runtime,
): gate_result => {
  if (!write_approval_enabled(subsystem, cfg)) {
    return { allow: true, blocked: false, stage: false, message: "" };
  }

  if (subsystem === subsystem_skills || is_background) {
    let where = "/skills pending";
    if (subsystem === subsystem_memory) {
      where = "/memory pending";
    }
    return {
      allow: false,
      blocked: false,
      stage: true,
      message: `Staged for approval (${subsystem}.write_approval is on). Not yet saved — review with ${where}.`,
    };
  }

  return {
    allow: false,
    blocked: false,
    stage: true,
    message:
      "Staged for approval (memory.write_approval is on). Not yet saved — review with /memory pending.",
  };
};

export const skill_gist = (
  action: string,
  name: string,
  content: string,
  file_path: string,
  old_string: string,
  new_string: string,
): string => {
  if ((action === "create" || action === "edit") && content !== "") {
    const desc = extract_description_quick(content);
    const size = content.length >= 1024 ? `${Math.floor(content.length / 1024) + 1} KB` : `${content.length} chars`;
    const verb = action === "edit" ? "rewrite" : "create";
    if (desc !== "") {
      return `${verb} '${name}' — ${desc} (${size})`;
    }
    return `${verb} '${name}' (${size})`;
  }
  if (action === "patch") {
    const target = file_path === "" ? "SKILL.md" : file_path;
    let removed = count_newlines(old_string);
    if (old_string !== "") removed++;
    let added = count_newlines(new_string);
    if (new_string !== "") added++;
    return `patch '${name}' ${target} (+${added}/-${removed} lines)`;
  }
  if (action === "write_file") return `write ${file_path} in '${name}'`;
  if (action === "remove_file") return `remove ${file_path} from '${name}'`;
  if (action === "delete") return `delete skill '${name}'`;
  return `${action} '${name}'`;
};

const extract_description_quick = (content: string): string => {
  const match = content.match(/^description:\s*(.+)$/m);
  if (match === null || match[1] === undefined) {
    return "";
  }
  let desc = match[1].trim().replace(/^["']|["']$/g, "");
  if (desc.length > 140) {
    desc = desc.slice(0, 137) + "...";
  }
  return desc;
};

export const memory_pending_diff = (record: pending_record): string => {
  const payload = record.payload;
  const action = get_string_field(payload, "action");
  let target = get_string_field(payload, "target");
  if (target === "") target = "memory";
  const content = get_string_field(payload, "content");
  const match = get_string_field(payload, "match");
  const replacement = get_string_field(payload, "replacement");

  switch (action) {
    case "add":
      return `add to ${target}:\n+${content}`;
    case "replace":
      return `replace in ${target} (match ${JSON.stringify(match)}):\n-${match}\n+${replacement}`;
    case "remove":
      return `remove from ${target} (match ${JSON.stringify(match)})`;
    default:
      if (record.summary.trim() !== "") return record.summary.trim();
      return `(${action} on ${target})`;
  }
};

export const skill_pending_diff = (record: pending_record): string => {
  const payload = record.payload;
  const action = get_string_field(payload, "action");
  const name = get_string_field(payload, "name");

  if (action === "create") {
    return get_string_field(payload, "content");
  }

  let current = "";
  let target_label = "SKILL.md";
  const skills_root = path.join(home_dir(), "skills");
  let skill_dir = "";

  walk_dirs(skills_root, (p, info) => {
    if (info.isDirectory() && path.basename(p) === name) {
      skill_dir = p;
      return false;
    }
    return true;
  });

  if (skill_dir !== "") {
    let p = path.join(skill_dir, "SKILL.md");
    if (action === "patch" || action === "write_file") {
      let rel = get_string_field(payload, "file_path");
      if (rel === "") rel = "SKILL.md";
      p = path.join(skill_dir, rel);
      target_label = rel;
    }
    try {
      current = fs.readFileSync(p, "utf8");
    } catch {
      current = "";
    }
  }

  let new_content = "";
  if (action === "edit") {
    new_content = get_string_field(payload, "content");
  } else if (action === "patch") {
    const old_s = get_string_field(payload, "old_string");
    const new_s = get_string_field(payload, "new_string");
    if (current !== "") new_content = current.split(old_s).join(new_s);
    else new_content = `(patch ${JSON.stringify(old_s)} → ${JSON.stringify(new_s)})`;
  } else if (action === "write_file") {
    new_content = get_string_field(payload, "file_content");
  } else if (action === "remove_file") {
    return `remove file: ${get_string_field(payload, "file_path")} from skill '${name}'`;
  } else if (action === "delete") {
    return `delete skill '${name}'`;
  } else {
    return `(${action} on '${name}')`;
  }

  return diff_lines(current, new_content, target_label);
};

export const diff_lines = (orig: string, new_text: string, label: string): string => {
  const orig_lines = orig.split("\n");
  const new_lines = new_text.split("\n");
  let out = `--- a/${label}\n+++ b/${label}\n`;
  const max_l = Math.max(orig_lines.length, new_lines.length);
  for (let i = 0; i < max_l; i++) {
    if (i < orig_lines.length && i < new_lines.length) {
      if (orig_lines[i] !== new_lines[i]) {
        out += `-${orig_lines[i]}\n+${new_lines[i]}\n`;
      }
    } else if (i < orig_lines.length) {
      out += `-${orig_lines[i]}\n`;
    } else if (i < new_lines.length) {
      out += `+${new_lines[i]}\n`;
    }
  }
  return out;
};

export const get_string_field = (m: Record<string, unknown> | null | undefined, key: string): string => {
  if (m === null || m === undefined) return "";
  const val = m[key];
  return typeof val === "string" ? val : "";
};

const count_newlines = (s: string): number => (s.match(/\n/g) ?? []).length;

const walk_dirs = (root: string, fn: (p: string, info: fs.Dirent) => boolean): void => {
  let entries: fs.Dirent[];
  try {
    entries = fs.readdirSync(root, { withFileTypes: true });
  } catch {
    return;
  }
  for (const entry of entries) {
    const p = path.join(root, entry.name);
    if (!fn(p, entry)) return;
    if (entry.isDirectory()) walk_dirs(p, fn);
  }
};

/*
PORT STATUS
source path: backend/approval/write_approval.go
source lines: 411
draft lines: 403
confidence: high
status: phase_a_draft
todos:
  - verify directory walk early-stop behavior matches filepath.Walk + io.EOF exactly
  - confirm JSON.stringify formatting is acceptable equivalent to json.MarshalIndent
notes:
  - Functions returning (T, error) use Effect.Effect<T, approval_error>.
  - Reuses existing hollowhome and config runtime types.
*/
