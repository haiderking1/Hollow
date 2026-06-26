import fs from "node:fs";
import path from "node:path";
import { home_dir } from "../hollowhome/home";
import { threatPatternIDs, ScanScope } from "./scan";
// SOUL.md — the agent's primary identity. When present, its content is the sole
// identity block in the stable prompt (no DEFAULT_AGENT_IDENTITY, HOLLOW_IDENTITY_RULE,
// or agent identity rule layered on top).
// Injected verbatim after scan/truncate (agent/prompt_builder.load_soul_md).

export const soulMaxChars = 24000;

const CONTEXT_TRUNCATE_HEAD_RATIO = 0.7;
const CONTEXT_TRUNCATE_TAIL_RATIO = 0.2;

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

/** Ensure HOLLOW_HOME exists and SOUL.md is present (empty on first install). */
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
    fs.writeFileSync(p, "", { mode: 0o600, encoding: "utf8" });
  } catch {
    // ignore
  }
}

function truncateSoulContent(content: string, maxChars: number): string {
  if (content.length <= maxChars) {
    return content;
  }
  const headChars = Math.floor(maxChars * CONTEXT_TRUNCATE_HEAD_RATIO);
  const tailChars = Math.floor(maxChars * CONTEXT_TRUNCATE_TAIL_RATIO);
  const readPath = SoulPath();
  const marker =
    `\n\n[...truncated SOUL.md: kept ${headChars}+${tailChars} of ${content.length} chars. ` +
    `The middle is omitted — if you need the full instructions, read the complete file with ` +
    `the read_file tool: ${readPath}]\n\n`;
  return content.slice(0, headChars) + marker + content.slice(content.length - tailChars);
}

/** Load SOUL.md from HOLLOW_HOME. Returns "" when missing/empty (caller uses DEFAULT_AGENT_IDENTITY). */
export function LoadSoul(): string {
  EnsureSoul();

  const soulPath = SoulPath();
  if (!fs.existsSync(soulPath)) {
    return "";
  }

  let content: string;
  try {
    content = fs.readFileSync(soulPath, "utf8").trim();
  } catch {
    return "";
  }

  if (content === "") {
    return "";
  }

  const ids = threatPatternIDs(content, ScanScope.ScopeContext);
  if (ids.length > 0) {
    return `[BLOCKED: SOUL.md contained potential prompt injection (${ids.join(", ")}). Content not loaded.]`;
  }

  return truncateSoulContent(content, soulMaxChars);
}

