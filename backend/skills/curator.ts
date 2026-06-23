// PORT: backend/skills/curator.go

import fs from "node:fs";
import path from "node:path";
import { Effect } from "effect";
import { type curator_settings } from "../config/config";
import { SkillsDir } from "./paths";
import {
  AgentCreatedReport,
  ArchiveSkill,
  SetState,
  parseIso,
  atomicWrite,
  type UsageReportRow
} from "./usage";

// CuratorProtectedBuiltins are bundled skills the curator must never archive
// or consolidate, regardless of prune_builtins, pin state, or LLM judgment.
export const CuratorProtectedBuiltins: Record<string, boolean> = {
  "hollow-agent": true,
};

// CuratorState is the persistent scheduler + status record
// (~/.hollow/skills/.curator_state).
export interface CuratorState {
  last_run_at?: string;
  last_run_duration_seconds?: number;
  last_run_summary?: string;
  last_report_path?: string;
  paused: boolean;
  run_count: number;
}

export function CuratorStatePath(): string {
  return path.join(SkillsDir(), ".curator_state");
}

// CuratorSuppressedPath lists bundled skills the curator archived, so a
// future re-seed of bundled skills keeps them archived.
export function CuratorSuppressedPath(): string {
  return path.join(SkillsDir(), ".curator_suppressed");
}

export function LoadCuratorState(): CuratorState {
  const filePath = CuratorStatePath();
  try {
    const data = fs.readFileSync(filePath);
    return JSON.parse(data.toString()) as CuratorState;
  } catch {
    return { paused: false, run_count: 0 };
  }
}

export function SaveCuratorState(st: CuratorState): void {
  try {
    const data = Buffer.from(JSON.stringify(st, null, "  "));
    // Run atomicWrite synchronously using Effect.runSync
    Effect.runSync(atomicWrite(CuratorStatePath(), data));
  } catch {}
}

export function SetCuratorPaused(paused: boolean): void {
  const st = LoadCuratorState();
  st.paused = paused;
  SaveCuratorState(st);
}

// IsProtectedBuiltin reports whether the curator must never touch this skill.
export function IsProtectedBuiltin(name: string): boolean {
  return !!CuratorProtectedBuiltins[name];
}

// IsBundledSkillName reports whether the skill ships with Hollow.
export function IsBundledSkillName(name: string): boolean {
  return name === "hollow-agent";
}

// loadSuppressed returns the set of bundled skills previously archived by the curator.
function loadSuppressed(): Record<string, boolean> {
  const out: Record<string, boolean> = {};
  try {
    const data = fs.readFileSync(CuratorSuppressedPath(), "utf8");
    for (const line of data.split("\n")) {
      const trimmed = line.trim();
      if (trimmed !== "") {
        out[trimmed] = true;
      }
    }
  } catch {}
  return out;
}

// MarkSuppressed records a bundled skill the curator archived.
export function MarkSuppressed(name: string): void {
  const sup = loadSuppressed();
  if (sup[name]) {
    return;
  }
  sup[name] = true;
  const lines: string[] = Object.keys(sup);
  try {
    const data = Buffer.from(lines.join("\n") + "\n");
    Effect.runSync(atomicWrite(CuratorSuppressedPath(), data));
  } catch {}
}

// IsSuppressed reports whether the curator previously archived this bundled
// skill (so re-seeds keep it archived).
export function IsSuppressed(name: string): boolean {
  return !!loadSuppressed()[name];
}

// ShouldRunCurator evaluates the static gates: enabled, not paused, and
// last_run_at older than the interval. First-run behavior: when there is no
// last_run_at, the state is seeded to now and the first real pass is deferred
// by one full interval — the curator should never mutate the library on the
// first tick after install. Explicit /curator-run bypasses this gate.
export function ShouldRunCurator(cfg: curator_settings, now: Date): boolean {
  if (!cfg.enabled) {
    return false;
  }
  const st = LoadCuratorState();
  if (st.paused) {
    return false;
  }
  if (!st.last_run_at) {
    st.last_run_at = now.toISOString();
    st.last_run_summary = "deferred first run — curator seeded, will run after one interval; use /curator-run dry-run to preview now";
    SaveCuratorState(st);
    return false;
  }
  const lastTimeMs = Date.parse(st.last_run_at);
  if (isNaN(lastTimeMs)) {
    return false;
  }
  const intervalMs = cfg.interval_hours * 60 * 60 * 1000;
  return now.getTime() - lastTimeMs >= intervalMs;
}

// CuratorTransitionCounts reports what the deterministic pass changed.
export class CuratorTransitionCounts {
  Checked = 0;
  MarkedStale = 0;
  Archived = 0;
  Reactivated = 0;

  Summary(): string {
    const parts: string[] = [];
    if (this.MarkedStale > 0) {
      parts.push(`${this.MarkedStale} marked stale`);
    }
    if (this.Archived > 0) {
      parts.push(`${this.Archived} archived`);
    }
    if (this.Reactivated > 0) {
      parts.push(`${this.Reactivated} reactivated`);
    }
    if (parts.length === 0) {
      return "no changes";
    }
    return parts.join(", ");
  }
}

// ApplyAutomaticTransitions walks every curator-managed skill and moves
// active/stale/archived based on the latest real activity timestamp. Pinned
// skills are never touched; protected builtins are never candidates. Never
// deletes — archive only.
export function ApplyAutomaticTransitions(cfg: curator_settings, now: Date): CuratorTransitionCounts {
  const staleCutoff = new Date(now.getTime() - cfg.stale_after_days * 24 * 60 * 60 * 1000);
  const archiveCutoff = new Date(now.getTime() - cfg.archive_after_days * 24 * 60 * 60 * 1000);

  const counts = new CuratorTransitionCounts();

  for (const row of AgentCreatedReport()) {
    counts.Checked++;
    if (row.pinned || IsProtectedBuiltin(row.name)) {
      continue;
    }

    // If never active, anchor on created_at so new skills don't
    // immediately archive themselves.
    let [anchor, ok] = parseIso(row.last_activity_at);
    if (!ok) {
      const [t, cok] = parseIso(row.created_at);
      if (cok) {
        anchor = t;
      } else {
        anchor = now;
      }
    }

    const current = row.state;
    if (anchor.getTime() <= archiveCutoff.getTime() && current !== "archived") {
      try {
        const [archivedOk] = Effect.runSync(ArchiveSkill(row.name));
        if (archivedOk) {
          counts.Archived++;
        }
      } catch {}
    } else if (anchor.getTime() <= staleCutoff.getTime() && current === "active") {
      SetState(row.name, "stale");
      counts.MarkedStale++;
    } else if (anchor.getTime() > staleCutoff.getTime() && current === "stale") {
      // Skill got used again after being marked stale — reactivate.
      SetState(row.name, "active");
      counts.Reactivated++;
    }
  }

  return counts;
}

// RenderCuratorCandidateList builds the agent-readable list of agent-created
// skills with usage stats for the LLM review prompt.
export function RenderCuratorCandidateList(): string {
  const rows = AgentCreatedReport();
  const eligible: UsageReportRow[] = [];
  for (const r of rows) {
    if (IsProtectedBuiltin(r.name)) {
      continue;
    }
    eligible.push(r);
  }
  if (eligible.length === 0) {
    return "No agent-created skills to review.";
  }
  const lines = [`Agent-created skills (${eligible.length}):\n`];
  for (const r of eligible) {
    const pinned = r.pinned ? "yes" : "no";
    const lastActivity = r.last_activity_at && r.last_activity_at !== "" ? r.last_activity_at : "never";
    lines.push(
      `- ${r.name}  state=${r.state}  pinned=${pinned}  activity=${r.activity_count}  use=${r.use_count}  view=${r.view_count}  patches=${r.patch_count}  last_activity=${lastActivity}`
    );
  }
  return lines.join("\n");
}

// CuratorStatusString renders the /curator-status output.
export function CuratorStatusString(cfg: curator_settings): string {
  const st = LoadCuratorState();
  let b = "Curator status:\n";
  b += `- enabled: ${cfg.enabled}\n`;
  b += `- paused: ${st.paused}\n`;
  if (!st.last_run_at) {
    b += "- last run: never\n";
  } else {
    b += `- last run: ${st.last_run_at}\n`;
  }
  b += `- runs: ${st.run_count}\n`;
  b += `- interval: ${cfg.interval_hours}h, min idle: ${cfg.min_idle_hours.toFixed(1)}h, stale after: ${cfg.stale_after_days}d, archive after: ${cfg.archive_after_days}d, prune builtins: ${cfg.prune_builtins}\n`;
  if (st.last_run_summary) {
    b += `- last summary: ${st.last_run_summary}\n`;
  }
  if (st.last_report_path) {
    b += `- last report: ${st.last_report_path}\n`;
  }
  return b.trimEnd();
}

/*
PORT STATUS
source path: backend/skills/curator.go
source lines: 281
draft lines: 254
confidence: high
status: phase_b_compile
*/
