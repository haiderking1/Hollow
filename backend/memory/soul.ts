// PORT: backend/memory/soul.go

import fs from "node:fs";
import path from "node:path";
import { home_dir } from "../hollowhome/home";
import { threatPatternIDs, ScanScope } from "./scan";

// SOUL.md — the agent's primary identity. When present, its content becomes
// the first stable block of the system prompt, replacing the default Hollow
// persona. disclosurePolicy (anti base-model disclosure) always follows SOUL;
// it does not override the user's chosen name or persona.

export const soulMaxChars = 24000;

// On fresh install SOUL.md is created empty — identity is opt-in. While it's
// empty, LoadSoul returns "" and the caller falls back to the built-in
// default persona (see agent/prompt.ts). The user edits this file to set a
// custom name/persona; edits take effect on the next session (/new).
export function SoulPath(): string {
  return path.join(home_dir(), "SOUL.md");
}

/** mtime of SOUL.md, or 0 when missing/unreadable. Used to invalidate cached prompts. */
export function soul_mtime_ms(): number {
  try {
    return fs.statSync(SoulPath()).mtimeMs;
  } catch {
    return 0;
  }
}

// EnsureSoul creates an empty SOUL.md if missing. Best-effort.
export function EnsureSoul(): void {
  const p = SoulPath();
  try {
    if (fs.existsSync(p)) {
      return;
    }
  } catch {
    // ignore
  }

  try {
    fs.mkdirSync(path.dirname(p), { recursive: true, mode: 0o700 });
  } catch {
    // ignore
  }

  try {
    fs.writeFileSync(p, "", { mode: 0o600 });
  } catch {
    // ignore
  }
}

// LoadSoul loads SOUL.md, seeding the default on first run. The content is
// threat-scanned before injection: a poisoned SOUL.md yields a blocked
// placeholder instead of the file content (the file on disk is untouched so
// the user can inspect and fix it). Returns "" when the file is missing or
// empty, in which case the caller falls back to the built-in identity.
export function LoadSoul(): string {
  EnsureSoul();

  let data: Buffer;
  try {
    data = fs.readFileSync(SoulPath());
  } catch {
    return "";
  }

  const content = data.toString("utf8").trim();
  if (content === "") {
    return "";
  }

  const ids = threatPatternIDs(content, ScanScope.ScopeContext);
  if (ids.length > 0) {
    return `[BLOCKED: SOUL.md contained threat pattern(s): ${ids.join(", ")}. Its content was removed from the system prompt. Inspect and fix ~/.hollow/SOUL.md, then start a new session.]`;
  }

  return formatSoulBlock(truncateMiddle(content, "persona", soulMaxChars));
}

/** Marker baked into soul-injected prompts so the agent cache can detect a stale persona. */
export const SOUL_PROMPT_MARKER = "<!-- hollow-soul-persona -->";

/** Turn informal SOUL lines (e.g. "you r shadow") into direct persona instructions. */
export function normalizeSoulPersona(content: string): string {
  const trimmed = content.trim();
  if (trimmed === "") {
    return "";
  }
  if (/^#\s/m.test(trimmed) || /^You are /im.test(trimmed)) {
    return trimmed;
  }
  const informal = trimmed.match(/^you\s+(?:are|r)\s+(.+)$/i);
  if (informal) {
    const raw = informal[1].trim();
    const name = raw.length > 0 ? raw.charAt(0).toUpperCase() + raw.slice(1) : raw;
    return `You are ${name}.`;
  }
  if (!/^you\b/i.test(trimmed)) {
    return `You are:\n${trimmed}`;
  }
  return trimmed;
}

/** Wrap persona text as binding identity — never framed as a file or external document. */
export function formatSoulBlock(content: string): string {
  const persona = normalizeSoulPersona(content);
  return (
    SOUL_PROMPT_MARKER +
    "\n" +
    persona +
    "\n\n" +
    "The lines above are you — your name, voice, morals, and behavior. Live them in every reply. " +
    "When asked who you are, answer in first person, naturally, as yourself."
  );
}

// truncateMiddle keeps the head and tail of oversized content with a marker
// in the middle, matching Hermes' context-file truncation.
export function truncateMiddle(content: string, filename: string, maxChars: number): string {
  if (content.length <= maxChars) {
    return content;
  }
  const headChars = Math.floor(maxChars * 7 / 10);
  const tailChars = Math.floor(maxChars * 2 / 10);
  const marker = `\n\n[...truncated ${filename}: kept ${headChars}+${tailChars} of ${content.length} chars. Use file tools to read the full file.]\n\n`;
  return content.slice(0, headChars) + marker + content.slice(content.length - tailChars);
}

/*
PORT STATUS
source path: backend/memory/soul.go
source lines: 92
draft lines: 104
confidence: high
status: phase_b_compile
*/
