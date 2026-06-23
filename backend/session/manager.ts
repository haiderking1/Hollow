// PORT: backend/session/manager.go

import { fingerprint_store, new_fingerprint_store } from "./fingerprints";
import { Effect } from "effect";
import fs from "node:fs/promises";
import path from "node:path";
import crypto from "node:crypto";
import type { message } from "../opencode/types";
import { content_string, string_content, content_blocks } from "../opencode/types";
import { session_dir } from "./paths";
import { valid_session_file } from "./list";
import {
  type_session,
  type_system_prompt,
  type_label,
  type_session_info,
  type_compaction,
  type_branch_summary,
  type_message,
  type_custom_message,
  type header,
  type file_entry,
  type chat_line,
  type chat_image,
  file_timestamp,
} from "./types";
import { get_branch, build_session_context, type session_context } from "./context";

export class manager {
  private _cwd = "";
  private _session_dir = "";
  private _session_file = "";
  private _session_id = "";
  private _entries: string[] = [];
  private _leaf_id: string | null = null;
  private _flushed = false;
  private _stored_system_prompt = "";
  private _labels_by_id = new Map<string, string>();
  private _label_timestamps_by_id = new Map<string, string>();
  private _fingerprints: fingerprint_store | null = null;

  constructor() {}

  cwd(): string {
    return this._cwd;
  }

  session_id(): string {
    return this._session_id;
  }

  session_file(): string {
    return this._session_file;
  }

  leaf_id(): string | null {
    return this._leaf_id;
  }

  get_cwd(): string {
    return this._cwd;
  }

  set_cwd(cwd: string) {
    this._cwd = cwd;
  }

  get_session_dir(): string {
    return this._session_dir;
  }

  set_session_dir(dir: string) {
    this._session_dir = dir;
  }

  get_entries(): string[] {
    return this._entries;
  }

  fingerprints(): fingerprint_store {
    if (!this._fingerprints) {
      this._fingerprints = new_fingerprint_store(() => this.session_file());
    }
    return this._fingerprints;
  }

  open_file(file_path: string): Effect.Effect<void, Error> {
    return Effect.tryPromise({
      try: async () => {
        const content = await fs.readFile(file_path, "utf8");
        const lines = content.split("\n");
        const entries: string[] = [];
        for (let line of lines) {
          line = line.trim();
          if (line !== "") {
            entries.push(line);
          }
        }
        if (entries.length === 0) {
          await Effect.runPromise(this.new_session_internal());
          return;
        }

        try {
          const h = JSON.parse(entries[0]) as header;
          if (h.type !== "session" || !h.id) {
            await Effect.runPromise(this.new_session_internal());
            return;
          }
          this._session_file = file_path;
          this._session_id = h.id;
          this._entries = entries;
          this._leaf_id = null;
          this._flushed = true;

          this._labels_by_id.clear();
          this._label_timestamps_by_id.clear();

          for (const raw of entries.slice(1)) {
            const entry = JSON.parse(raw) as file_entry;
            if (entry.type !== type_session && entry.id) {
              if (
                entry.type !== type_label &&
                entry.type !== type_session_info &&
                entry.type !== type_system_prompt
              ) {
                this._leaf_id = entry.id;
              }
              if (entry.type === type_system_prompt) {
                if (typeof entry.content === "string") {
                  this._stored_system_prompt = entry.content;
                }
              }
              if (entry.type === type_label) {
                if (entry.label) {
                  this._labels_by_id.set(entry.targetId || "", entry.label);
                  this._label_timestamps_by_id.set(entry.targetId || "", entry.timestamp);
                } else {
                  this._labels_by_id.delete(entry.targetId || "");
                  this._label_timestamps_by_id.delete(entry.targetId || "");
                }
              }
            }
          }
        } catch {
          await Effect.runPromise(this.new_session_internal());
        }
      },
      catch: (cause) => cause instanceof Error ? cause : new Error(String(cause)),
    });
  }

  private new_session_internal(): Effect.Effect<void, Error> {
    return Effect.tryPromise({
      try: async () => {
        this._session_id = crypto.randomBytes(16).toString("hex");
        const ts = new Date();
        const h: header = {
          type: "session",
          version: 1,
          id: this._session_id,
          timestamp: ts.toISOString(),
          cwd: this._cwd,
        };

        const raw = JSON.stringify(h);
        this._entries = [raw];
        this._leaf_id = null;
        this._flushed = false;
        this._session_file = path.join(
          this._session_dir,
          `${file_timestamp(ts)}_${this._session_id}.jsonl`,
        );
        this._stored_system_prompt = "";
        this._labels_by_id.clear();
        this._label_timestamps_by_id.clear();
      },
      catch: (cause) => cause instanceof Error ? cause : new Error(String(cause)),
    });
  }

  new_session(): Effect.Effect<void, Error> {
    return this.new_session_internal();
  }

  parsed_entries(): file_entry[] {
    const out: file_entry[] = [];
    for (const raw of this._entries) {
      try {
        out.push(JSON.parse(raw) as file_entry);
      } catch {}
    }
    return out;
  }

  get_branch(leaf_id: string | null): file_entry[] {
    return get_branch(this.parsed_entries(), leaf_id);
  }

  build_session_context(): session_context {
    return build_session_context(this.parsed_entries(), this._leaf_id);
  }

  messages(): message[] {
    return this.build_session_context().messages ?? [];
  }

  chat_lines(): chat_line[] {
    const branch = this.get_branch(this._leaf_id);
    const out: chat_line[] = [];

    for (let i = 0; i < branch.length; i++) {
      const entry = branch[i];

      if (entry.type === type_compaction) {
        out.push({
          role: "compactionSummary",
          text: entry.summary || "",
          thinking: "",
          tool_name: "",
          tool_args: "",
          tool_result: "",
          tool_details: "",
          tool_error: false,
          tokens_before: entry.tokensBefore || 0,
          images: [],
        });
        continue;
      }

      if (entry.type === type_branch_summary) {
        out.push({
          role: "branchSummary",
          text: entry.summary || "",
          thinking: "",
          tool_name: "",
          tool_args: "",
          tool_result: "",
          tool_details: "",
          tool_error: false,
          tokens_before: 0,
          images: [],
        });
        continue;
      }

      if (entry.type === type_custom_message) {
        let text = "";
        if (typeof entry.content === "string") {
          text = entry.content;
        }
        let display = true;
        if (entry.display !== undefined && entry.display !== null) {
          display = entry.display;
        }
        if (display) {
          out.push({
            role: "user",
            text: text,
            thinking: "",
            tool_name: "",
            tool_args: "",
            tool_result: "",
            tool_details: "",
            tool_error: false,
            tokens_before: 0,
            images: [],
          });
        }
        continue;
      }

      if (entry.type !== type_message || !entry.message) {
        continue;
      }

      const msg = entry.message;

      if (msg.role === "assistant" && msg.tool_calls && msg.tool_calls.length > 0) {
        let thinking = "";
        if (msg.reasoning_content !== undefined) {
          thinking = msg.reasoning_content.trim();
        }
        const text = content_string(msg).trim();
        if (text !== "" || thinking !== "") {
          out.push({
            role: "assistant",
            text: text,
            thinking: thinking,
            tool_name: "",
            tool_args: "",
            tool_result: "",
            tool_details: "",
            tool_error: false,
            tokens_before: 0,
            images: [],
          });
        }
        for (const tc of msg.tool_calls) {
          const line: chat_line = {
            role: "tool",
            text: "",
            thinking: "",
            tool_name: tc.function.name,
            tool_args: tc.function.arguments,
            tool_result: "",
            tool_details: "",
            tool_error: false,
            tokens_before: 0,
            images: [],
          };
          for (let j = i + 1; j < branch.length; j++) {
            const tm = branch[j];
            if (
              tm.type === type_message &&
              tm.message &&
              tm.message.role === "tool" &&
              tm.message.tool_call_id === tc.id
            ) {
              line.tool_result = content_string(tm.message).trim();
              line.tool_details = tm.toolDetails || "";
              break;
            }
          }
          out.push(line);
        }
        continue;
      }

      if (msg.role === "tool") {
        continue;
      }

      const lineRes = message_to_chat_line(msg);
      if (lineRes) {
        out.push(lineRes);
      }
    }
    return out;
  }

  append_message_with_details(msg: message, toolDetails: string): Effect.Effect<void, Error> {
    if (msg.role === "system") {
      return Effect.void;
    }

    const parent = this._leaf_id;
    const id = new_id();
    const entry: file_entry = {
      type: type_message,
      id: id,
      parentId: parent,
      timestamp: new Date().toISOString(),
      message: msg,
      toolDetails: toolDetails,
    };

    try {
      const raw = JSON.stringify(entry);
      this._entries.push(raw);
      this._leaf_id = id;
      return this.persist_entry(raw, msg.role === "assistant");
    } catch (err) {
      return Effect.fail(err instanceof Error ? err : new Error(String(err)));
    }
  }

  append_message(msg: message): Effect.Effect<void, Error> {
    return this.append_message_with_details(msg, "");
  }

  append_compaction(
    summary: string,
    firstKeptEntryId: string,
    tokensBefore: number,
    details: any,
    fromHook: boolean,
  ): Effect.Effect<void, Error> {
    const parent = this._leaf_id;
    const id = new_id();
    const entry: file_entry = {
      type: type_compaction,
      id: id,
      parentId: parent,
      timestamp: new Date().toISOString(),
      summary,
      firstKeptEntryId,
      tokensBefore,
      details,
      fromHook,
    };

    try {
      const raw = JSON.stringify(entry);
      this._entries.push(raw);
      this._leaf_id = id;
      return this.persist_entry(raw, true);
    } catch (err) {
      return Effect.fail(err instanceof Error ? err : new Error(String(err)));
    }
  }

  branch_with_summary(
    branchFromId: string | null,
    summary: string,
    details: any,
    fromHook: boolean,
  ): Effect.Effect<string, Error> {
    this._leaf_id = branchFromId;
    const id = new_id();
    const fromId = branchFromId !== null ? branchFromId : "root";
    const entry: file_entry = {
      type: type_branch_summary,
      id: id,
      parentId: branchFromId,
      timestamp: new Date().toISOString(),
      fromId: fromId,
      summary: summary,
      details: details,
      fromHook: fromHook,
    };

    try {
      const raw = JSON.stringify(entry);
      this._entries.push(raw);
      this._leaf_id = id;
      return this.persist_entry(raw, true).pipe(Effect.map(() => id));
    } catch (err) {
      return Effect.fail(err instanceof Error ? err : new Error(String(err)));
    }
  }

  stored_system_prompt(): string {
    return this._stored_system_prompt;
  }

  set_system_prompt(prompt: string): Effect.Effect<void, Error> {
    if (prompt === this._stored_system_prompt) {
      return Effect.void;
    }
    const id = new_id();
    const entry: file_entry = {
      type: type_system_prompt,
      id: id,
      parentId: this._leaf_id,
      timestamp: new Date().toISOString(),
      content: prompt,
    };

    try {
      const raw = JSON.stringify(entry);
      this._entries.push(raw);
      this._stored_system_prompt = prompt;
      return this.persist_entry(raw, false);
    } catch (err) {
      return Effect.fail(err instanceof Error ? err : new Error(String(err)));
    }
  }

  private persist_entry(entry: string, isAssistant: boolean): Effect.Effect<void, Error> {
    return Effect.tryPromise({
      try: async () => {
        if (this._session_file === "") {
          throw new Error("session file not initialized");
        }

        if (!this.has_assistant() && !isAssistant) {
          return;
        }

        if (!this._flushed) {
          await Effect.runPromise(this.rewrite_file());
          return;
        }

        const fh = await fs.open(this._session_file, "a", 0o600);
        try {
          await fh.write(entry + "\n");
          this._flushed = true;
        } finally {
          await fh.close();
        }
      },
      catch: (cause) => cause instanceof Error ? cause : new Error(String(cause)),
    });
  }

  private has_assistant(): boolean {
    for (const raw of this._entries) {
      try {
        const entry = JSON.parse(raw) as file_entry;
        if (
          entry.type === type_message &&
          entry.message &&
          entry.message.role === "assistant"
        ) {
          return true;
        }
      } catch {}
    }
    return false;
  }

  private rewrite_file(): Effect.Effect<void, Error> {
    return Effect.tryPromise({
      try: async () => {
        if (this._session_file === "") {
          throw new Error("session file not initialized");
        }

        await fs.mkdir(path.dirname(this._session_file), { recursive: true, mode: 0o700 });

        const lines = this._entries.join("\n") + (this._entries.length > 0 ? "\n" : "");
        await fs.writeFile(this._session_file, lines, { encoding: "utf8", mode: 0o600 });
        this._flushed = true;
      },
      catch: (cause) => cause instanceof Error ? cause : new Error(String(cause)),
    });
  }

  get_tree(): session_tree_node[] {
    const entries = this.parsed_entries();
    const nodeMap = new Map<string, session_tree_node>();
    const roots: session_tree_node[] = [];

    for (const entry of entries) {
      if (entry.id === "" || entry.type === type_session) {
        continue;
      }
      const label = this._labels_by_id.get(entry.id) || "";
      const labelTS = this._label_timestamps_by_id.get(entry.id) || "";
      nodeMap.set(entry.id, {
        entry,
        children: [],
        label,
        labelTimestamp: labelTS,
      });
    }

    for (const entry of entries) {
      if (entry.id === "" || entry.type === type_session) {
        continue;
      }
      const node = nodeMap.get(entry.id);
      if (!node) continue;

      if (!entry.parentId || entry.parentId === "" || entry.parentId === entry.id) {
        roots.push(node);
      } else {
        const parent = nodeMap.get(entry.parentId);
        if (parent) {
          parent.children.push(node);
        } else {
          roots.push(node);
        }
      }
    }

    const sortTree = (node: session_tree_node) => {
      node.children.sort((a, b) => {
        const ta = new Date(a.entry.timestamp).getTime();
        const tb = new Date(b.entry.timestamp).getTime();
        return ta - tb;
      });
      for (const child of node.children) {
        sortTree(child);
      }
    };

    roots.sort((a, b) => {
      const ta = new Date(a.entry.timestamp).getTime();
      const tb = new Date(b.entry.timestamp).getTime();
      return ta - tb;
    });

    for (const root of roots) {
      sortTree(root);
    }

    return roots;
  }

  append_label_change(targetID: string, label: string): Effect.Effect<string, Error> {
    const id = new_id();
    const entry: file_entry = {
      type: type_label,
      id: id,
      parentId: this._leaf_id,
      timestamp: new Date().toISOString(),
      targetId: targetID,
      label: label,
    };

    try {
      const raw = JSON.stringify(entry);
      this._entries.push(raw);
      return this.persist_entry(raw, false).pipe(
        Effect.map(() => {
          if (label !== "") {
            this._labels_by_id.set(targetID, label);
            this._label_timestamps_by_id.set(targetID, entry.timestamp);
          } else {
            this._labels_by_id.delete(targetID);
            this._label_timestamps_by_id.delete(targetID);
          }
          return id;
        }),
      );
    } catch (err) {
      return Effect.fail(err instanceof Error ? err : new Error(String(err)));
    }
  }

  branch(branchFromId: string): void {
    this._leaf_id = branchFromId;
  }

  reset_leaf(): void {
    this._leaf_id = null;
  }
}

export type session_tree_node = {
  entry: file_entry;
  children: session_tree_node[];
  label: string;
  labelTimestamp: string;
};

const message_to_chat_line = (msg: message): chat_line | null => {
  switch (msg.role) {
    case "user": {
      const text = content_string(msg).trim();
      const chatImages: chat_image[] = [];
      const blocks = content_blocks(msg);
      if (blocks) {
        for (const b of blocks) {
          if (b.type === "image_url" && b.image_url) {
            chatImages.push({
              url: b.image_url.url,
              mime_type: "",
              width: 0,
              height: 0,
            });
          }
        }
      }
      if (text === "" && chatImages.length === 0) {
        return null;
      }
      return {
        role: "user",
        text: text,
        thinking: "",
        tool_name: "",
        tool_args: "",
        tool_result: "",
        tool_details: "",
        tool_error: false,
        tokens_before: 0,
        images: chatImages,
      };
    }
    case "assistant": {
      let thinking = "";
      if (msg.reasoning_content !== undefined) {
        thinking = msg.reasoning_content.trim();
      }
      if (msg.tool_calls && msg.tool_calls.length > 0) {
        return null;
      }
      const text = content_string(msg).trim();
      if (text === "" && thinking === "") {
        return null;
      }
      return {
        role: "assistant",
        text: text,
        thinking: thinking,
        tool_name: "",
        tool_args: "",
        tool_result: "",
        tool_details: "",
        tool_error: false,
        tokens_before: 0,
        images: [],
      };
    }
    case "tool":
      return null;
    default:
      return null;
  }
};

const new_id = (): string => {
  return crypto.randomBytes(16).toString("hex");
};

const find_most_recent = async (dir: string): Promise<string> => {
  try {
    const entries = await fs.readdir(dir, { withFileTypes: true });
    let bestPath = "";
    let bestTime = 0;

    for (const e of entries) {
      if (e.isDirectory() || !e.name.endsWith(".jsonl")) {
        continue;
      }
      const file_path = path.join(dir, e.name);
      if (!valid_session_file(file_path)) {
        continue;
      }
      try {
        const stat = await fs.stat(file_path);
        const mtime = stat.mtimeMs;
        if (bestPath === "" || mtime > bestTime) {
          bestPath = file_path;
          bestTime = mtime;
        }
      } catch {}
    }
    return bestPath;
  } catch {
    return "";
  }
};

export const continue_recent = (cwd: string): Effect.Effect<manager, Error> => {
  return Effect.tryPromise({
    try: async () => {
      let cleanCwd = cwd;
      if (cleanCwd === "") {
        cleanCwd = process.cwd();
      }
      cleanCwd = path.resolve(cleanCwd);

      const dir = await Effect.runPromise(session_dir(cleanCwd));
      const m = new manager();
      m.set_cwd(cleanCwd);
      m.set_session_dir(dir);

      const recent = await find_most_recent(dir);
      if (recent !== "") {
        await Effect.runPromise(m.open_file(recent));
        return m;
      }

      await Effect.runPromise(m.new_session());
      return m;
    },
    catch: (cause) => cause instanceof Error ? cause : new Error(String(cause)),
  });
};

export const start_new = (cwd: string): Effect.Effect<manager, Error> => {
  return Effect.tryPromise({
    try: async () => {
      let cleanCwd = cwd;
      if (cleanCwd === "") {
        cleanCwd = process.cwd();
      }
      cleanCwd = path.resolve(cleanCwd);

      const dir = await Effect.runPromise(session_dir(cleanCwd));
      const m = new manager();
      m.set_cwd(cleanCwd);
      m.set_session_dir(dir);

      await Effect.runPromise(m.new_session());
      return m;
    },
    catch: (cause) => cause instanceof Error ? cause : new Error(String(cause)),
  });
};

/*
PORT STATUS
source path: backend/session/manager.go
source lines: 709
confidence: high
status: phase_b_compile
*/
