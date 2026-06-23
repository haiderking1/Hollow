// PORT: backend/session/list.go

import { Effect } from "effect";
import fs from "node:fs/promises";
import fs_sync from "node:fs";
import path from "node:path";
import readline from "node:readline";
import { home_agent_dir, session_dir, sessions_subdir } from "./paths";
import { home_dir } from "../hollowhome/home";
import { manager } from "./manager";
import {
  type info,
  type header,
  type message_entry,
} from "./types";
import { content_string } from "../opencode/types";

// Open loads a specific session file.
export const open_session = (session_path: string): Effect.Effect<manager, Error> => {
  return Effect.tryPromise({
    try: async () => {
      const clean_path = path.resolve(session_path);
      const m = new manager();
      m.set_session_dir(path.dirname(clean_path));
      await Effect.runPromise(m.open_file(clean_path));
      if (m.get_cwd() === "") {
        try {
          const h = JSON.parse(m.get_entries()[0]) as header;
          m.set_cwd(h.cwd || "");
        } catch {}
      }
      return m;
    },
    catch: (cause) => cause instanceof Error ? cause : new Error(String(cause)),
  });
};

// ListForCWD returns sessions for a project directory, newest first.
export const list_for_cwd = (cwd: string): Effect.Effect<info[], Error> => {
  return Effect.gen(function* () {
    let abs_cwd = cwd;
    if (abs_cwd === "") {
      abs_cwd = process.cwd();
    }
    abs_cwd = path.resolve(abs_cwd);
    const dir = yield* session_dir(abs_cwd);
    return yield* list_dir(dir);
  });
};

// ListAll returns sessions across every project directory, newest first.
export const list_all = (): Effect.Effect<info[], Error> => {
  return Effect.gen(function* () {
    const agent_dir = yield* home_agent_dir();
    const root = path.join(agent_dir, sessions_subdir);

    let entries: fs_sync.Dirent[] = [];
    const read_res = yield* Effect.either(
      Effect.tryPromise({
        try: () => fs.readdir(root, { withFileTypes: true }),
        catch: (cause) => cause instanceof Error ? cause : new Error(String(cause)),
      })
    );
    if (read_res._tag === "Left") {
      const err = read_res.left as any;
      if (err.code === "ENOENT") {
        return [];
      }
      return yield* Effect.fail(err instanceof Error ? err : new Error(String(err)));
    }
    entries = read_res.right;

    const out: info[] = [];
    for (const e of entries) {
      if (!e.isDirectory()) {
        continue;
      }
      const sub_dir = path.join(root, e.name);
      const res = yield* Effect.either(list_dir(sub_dir));
      if (res._tag === "Right") {
        out.push(...res.right);
      }
    }

    // Sort newest first
    out.sort((a, b) => b.modified.getTime() - a.modified.getTime());
    return out;
  });
};

// ListDir lists valid session files in a directory.
export const list_dir = (dir: string): Effect.Effect<info[], Error> => {
  return Effect.tryPromise({
    try: async () => {
      let entries: fs_sync.Dirent[] = [];
      try {
        entries = await fs.readdir(dir, { withFileTypes: true });
      } catch (err: any) {
        if (err.code === "ENOENT") {
          return [];
        }
        throw err;
      }

      const out: info[] = [];
      for (const e of entries) {
        if (e.isDirectory() || !e.name.endsWith(".jsonl")) {
          continue;
        }
        const file_path = path.join(dir, e.name);
        try {
          const scan_res = await Effect.runPromise(scan_info(file_path));
          if (scan_res !== null) {
            out.push(scan_res);
          }
        } catch {}
      }

      // Sort newest first
      out.sort((a, b) => b.modified.getTime() - a.modified.getTime());
      return out;
    },
    catch: (cause) => cause instanceof Error ? cause : new Error(String(cause)),
  });
};

// ScanInfo reads session metadata from a JSONL file without loading all messages.
export const scan_info = (session_path: string): Effect.Effect<info, Error> => {
  return Effect.tryPromise({
    try: async () => {
      const clean_path = path.resolve(session_path);
      if (!valid_session_file(clean_path)) {
        throw new Error("invalid session file");
      }

      const stat = await fs.stat(clean_path);

      const file_stream = fs_sync.createReadStream(clean_path);
      const rl = readline.createInterface({
        input: file_stream,
        crlfDelay: Infinity,
      });

      let header_val: header | null = null;
      let header_ok = false;
      let message_count = 0;
      let first_message = "";

      for await (const line of rl) {
        const trimmed = line.trim();
        if (trimmed === "") continue;

        const line_type = peek_line_type(trimmed);
        if (line_type === "session") {
          if (!header_ok) {
            try {
              const h = JSON.parse(trimmed) as header;
              if (h.type === "session" && h.id !== "") {
                header_val = h;
                header_ok = true;
              }
            } catch {}
          }
        } else if (line_type === "message") {
          message_count++;
          if (first_message === "" && (trimmed.includes('"role":"user"') || trimmed.includes('"role": "user"'))) {
            try {
              const entry = JSON.parse(trimmed) as message_entry;
              const text = content_string(entry.message).trim();
              if (text !== "") {
                first_message = text;
              }
            } catch {}
          }
        }

        if (header_ok && first_message !== "") {
          rl.close();
          file_stream.destroy();
          break;
        }
      }

      if (!header_ok || !header_val) {
        throw new Error("missing session header");
      }

      let created = stat.mtime;
      if (header_val.timestamp) {
        const parsed_time = new Date(header_val.timestamp);
        if (!isNaN(parsed_time.getTime())) {
          created = parsed_time;
        }
      }

      if (first_message === "") {
        first_message = "(no messages)";
      }

      return {
        path: clean_path,
        id: header_val.id,
        cwd: header_val.cwd,
        modified: stat.mtime,
        created: created,
        message_count: message_count,
        first_message: first_message,
      };
    },
    catch: (cause) => cause instanceof Error ? cause : new Error(String(cause)),
  });
};

export const peek_line_type = (line: string): string => {
  if (line.includes('"type":"session"') || line.includes('"type": "session"')) {
    return "session";
  }
  if (line.includes('"type":"message"') || line.includes('"type": "message"')) {
    return "message";
  }
  return "";
};

export const valid_session_file = (session_path: string): boolean => {
  try {
    const fd = fs_sync.openSync(session_path, "r");
    const buffer = Buffer.alloc(4096);
    const bytes_read = fs_sync.readSync(fd, buffer, 0, 4096, 0);
    fs_sync.closeSync(fd);

    if (bytes_read === 0) return false;

    const content = buffer.toString("utf8", 0, bytes_read);
    const first_line = content.split("\n")[0].trim();
    if (first_line === "") return false;

    const h = JSON.parse(first_line) as header;
    return h.type === "session" && h.id !== "";
  } catch {
    return false;
  }
};

export const format_relative = (t: Date): string => {
  const diff_ms = Date.now() - t.getTime();
  const diff_sec = diff_ms / 1000;
  if (diff_sec < 60) {
    return "now";
  }
  const diff_min = diff_sec / 60;
  if (diff_min < 60) {
    return `${Math.floor(diff_min)}m`;
  }
  const diff_hour = diff_min / 60;
  if (diff_hour < 24) {
    return `${Math.floor(diff_hour)}h`;
  }
  const diff_day = diff_hour / 24;
  if (diff_day < 7) {
    return `${Math.floor(diff_day)}d`;
  }
  if (diff_day < 30) {
    return `${Math.floor(diff_day / 7)}w`;
  }
  if (diff_day < 365) {
    return `${Math.floor(diff_day / 30)}mo`;
  }
  return `${Math.floor(diff_day / 365)}y`;
};

export const shorten_path = (file_path: string): string => {
  if (file_path === "") {
    return "";
  }
  const home = home_dir();
  if (file_path.startsWith(home)) {
    return "~" + file_path.substring(home.length);
  }
  return file_path;
};

/*
PORT STATUS
source path: backend/session/list.go
source lines: 249
draft lines: 257
confidence: high
status: phase_b_compile
*/
