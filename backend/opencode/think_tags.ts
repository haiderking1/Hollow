// PORT: backend/opencode/think_tags.go

import { get_reasoning, string_content, content_string, type message } from "./types";

const think_open_tags = [
  "<think>",
  "<redacted_thinking>"
];

const think_close_tags = [
  "</think>",
  "</redacted_thinking>"
];

export const max_think_tag_len = (): number => {
  let n = 0;
  for (const t of think_open_tags) {
    if (t.length > n) {
      n = t.length;
    }
  }
  for (const t of think_close_tags) {
    if (t.length > n) {
      n = t.length;
    }
  }
  return n;
};

export const find_think_open = (s: string, from: number): [number, number] => {
  const lower = s.slice(from).toLowerCase();
  let best = -1;
  let best_len = 0;
  for (const tag of think_open_tags) {
    const i = lower.indexOf(tag.toLowerCase());
    if (i >= 0 && (best < 0 || i < best)) {
      best = i;
      best_len = tag.length;
    }
  }
  if (best < 0) {
    return [-1, 0];
  }
  return [from + best, best_len];
};

export const close_tag_for_open = (open_tag: string): string => {
  const lower_open = open_tag.toLowerCase();
  for (let i = 0; i < think_open_tags.length; i++) {
    if (think_open_tags[i].toLowerCase() === lower_open) {
      return think_close_tags[i];
    }
  }
  return think_close_tags[0];
};

export const find_think_close_from = (s: string, from: number, open_tag: string): [number, number] => {
  const close_tag = close_tag_for_open(open_tag);
  const lower = s.slice(from).toLowerCase();
  const first_idx = lower.indexOf(close_tag.toLowerCase());
  if (first_idx >= 0) {
    return [from + first_idx, close_tag.length];
  }
  let best = -1;
  let best_len = 0;
  for (const tag of think_close_tags) {
    const i = lower.indexOf(tag.toLowerCase());
    if (i >= 0 && (best < 0 || i < best)) {
      best = i;
      best_len = tag.length;
    }
  }
  if (best < 0) {
    return [-1, 0];
  }
  return [from + best, best_len];
};

export const skip_space = (s: string, from: number): number => {
  while (from < s.length && /\s/.test(s[from])) {
    from++;
  }
  return from;
};

export const split_embedded_thinking = (s: string): [string, string] => {
  const text_parts: string[] = [];
  const think_parts: string[] = [];
  let i = 0;
  while (i < s.length) {
    const [open_idx, open_len] = find_think_open(s, i);
    if (open_idx < 0) {
      text_parts.push(s.slice(i));
      break;
    }
    text_parts.push(s.slice(i, open_idx));
    const think_start = open_idx + open_len;
    const open_tag = s.slice(open_idx, open_idx + open_len);
    const [close_idx, close_len] = find_think_close_from(s, think_start, open_tag);
    if (close_idx < 0) {
      think_parts.push(s.slice(think_start));
      break;
    }
    think_parts.push(s.slice(think_start, close_idx));
    i = skip_space(s, close_idx + close_len);
  }
  return [text_parts.join(""), think_parts.join("")];
};

export const sanitize_embedded_thinking = (msg: message | null | undefined): void => {
  if (!msg || msg.role !== "assistant") {
    return;
  }
  const raw = content_string(msg);
  if (raw === "") {
    return;
  }
  const [text, embedded] = split_embedded_thinking(raw);
  if (embedded === "" && text === raw) {
    return;
  }
  if (text !== "") {
    msg.content = string_content(text);
  } else {
    msg.content = new Uint8Array();
  }
  let existing = get_reasoning(msg);
  if (embedded !== "") {
    if (existing !== "") {
      existing += embedded;
    } else {
      existing = embedded;
    }
  }
  if (existing !== "") {
    msg.reasoning_content = existing;
    delete msg.reasoning_details;
    delete msg.reasoning;
  }
};

export class think_stream_splitter {
  in_think = false;
  open_tag = "";
  carry = "";

  feed(chunk: string, emit_text: (text: string) => void, emit_think: (text: string) => void): void {
    const data = this.carry + chunk;
    this.carry = "";
    if (data === "") {
      return;
    }

    const max_hold = max_think_tag_len() - 1;
    let i = 0;
    while (i < data.length) {
      if (!this.in_think) {
        const [open_idx, open_len] = find_think_open(data, i);
        if (open_idx < 0) {
          let safe_end = data.length;
          if (safe_end - i > max_hold) {
            safe_end = data.length - max_hold;
          }
          if (safe_end > i) {
            emit_text(data.slice(i, safe_end));
          }
          if (safe_end < data.length) {
            this.carry += data.slice(safe_end);
          }
          return;
        }
        if (open_idx > i) {
          emit_text(data.slice(i, open_idx));
        }
        this.in_think = true;
        this.open_tag = data.slice(open_idx, open_idx + open_len);
        i = open_idx + open_len;
        continue;
      }

      const [close_idx, close_len] = find_think_close_from(data, i, this.open_tag);
      if (close_idx < 0) {
        let safe_end = data.length;
        if (safe_end - i > max_hold) {
          safe_end = data.length - max_hold;
        }
        if (safe_end > i) {
          emit_think(data.slice(i, safe_end));
        }
        if (safe_end < data.length) {
          this.carry += data.slice(safe_end);
        }
        return;
      }
      if (close_idx > i) {
        emit_think(data.slice(i, close_idx));
      }
      this.in_think = false;
      this.open_tag = "";
      i = skip_space(data, close_idx + close_len);
    }
  }

  flush(emit_text: (text: string) => void, emit_think: (text: string) => void): void {
    if (this.carry.length === 0) {
      return;
    }
    const rest = this.carry;
    this.carry = "";
    if (this.in_think) {
      emit_think(rest);
    } else {
      emit_text(rest);
    }
  }
}

/*
PORT STATUS
source path: backend/opencode/think_tags.go
source lines: 216
draft lines: 182
confidence: high
status: phase_b_compile
*/
