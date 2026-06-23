import { Agent } from "./agent";
import { new_client_for_runtime } from "../opencode/runtime_client";
import { type runtime } from "../config/config";
import * as opencode from "../opencode/types";
import { string_content, content_string, type message } from "../opencode/types";
import {
  LoadCuratorState,
  SaveCuratorState,
  ShouldRunCurator,
  ApplyAutomaticTransitions,
  RenderCuratorCandidateList,
  CuratorTransitionCounts,
  CuratorState
} from "../skills/curator";
import { AgentCreatedReport } from "../skills/usage";
import { HomeDir, SkillsDir } from "../skills/paths";
import fs from "node:fs";
import path from "node:path";
import { Effect } from "effect";

// curatorMaxIterations bounds the curator fork's model calls per pass.
export const curatorMaxIterations = 8;

// curatorToolWhitelist — read tools, skill management, and bash for archive moves.
export const curatorToolWhitelist: Record<string, boolean> = {
  "skills_list": true,
  "skill_view": true,
  "skill_manage": true,
  "bash": true,
};

export const curatorDryRunBanner =
  "═══════════════════════════════════════════════════════════════\n" +
  "DRY-RUN — REPORT ONLY. DO NOT MUTATE THE SKILL LIBRARY.\n" +
  "═══════════════════════════════════════════════════════════════\n" +
  "\n" +
  "This is a PREVIEW pass. Follow every instruction below EXCEPT:\n" +
  "\n" +
  "  • DO NOT call skill_manage with action=patch, create, delete, " +
  "write_file, or remove_file.\n" +
  "  • DO NOT call bash to mv skill directories into .archive/.\n" +
  "  • DO NOT call bash to mv, cp, rm, or rewrite any file under " +
  "~/.hollow/skills/.\n" +
  "  • skills_list and skill_view are FINE — read as much as you need.\n" +
  "\n" +
  "Your output IS the deliverable. Produce the exact same " +
  "human-readable summary and structured YAML block you would " +
  "produce on a live run — but describe the actions you WOULD take, " +
  "not actions you took. A downstream reviewer will read the report " +
  "and decide whether to approve a live run with /curator-run (no flag).\n" +
  "\n" +
  "If you accidentally take a mutating action, say so explicitly in " +
  "the summary so the reviewer can revert it.\n" +
  "═══════════════════════════════════════════════════════════════";

export const curatorReviewPrompt =
  "You are running as Hollow's background skill CURATOR. This is an " +
  "UMBRELLA-BUILDING consolidation pass, not a passive audit and not a " +
  "duplicate-finder.\n\n" +
  "The goal of the skill collection is a LIBRARY OF CLASS-LEVEL " +
  "INSTRUCTIONS AND EXPERIENTIAL KNOWLEDGE. A collection of hundreds of " +
  "narrow skills where each one captures one session's specific bug is " +
  "a FAILURE of the library — not a feature. An agent searching skills " +
  "matches on descriptions, not on exact names; one broad umbrella " +
  "skill with labeled subsections beats five narrow siblings for " +
  "discoverability, not the other way around.\n\n" +
  "The right target shape is CLASS-LEVEL skills with rich SKILL.md " +
  "bodies + `references/`, `templates/`, and `scripts/` subfiles for " +
  "session-specific detail — not one-session-one-skill micro-entries.\n\n" +
  "Hard rules — do not violate:\n" +
  "1. DO NOT touch bundled or externally-installed skills. The candidate " +
  "list below is already filtered to agent-created skills only.\n" +
  "2. DO NOT delete any skill. Archiving (moving the skill's directory " +
  "into ~/.hollow/skills/.archive/) is the maximum destructive action. " +
  "Archives are recoverable; deletion is not.\n" +
  "3. DO NOT touch skills shown as pinned=yes. Skip them entirely.\n" +
  "3b. DO NOT archive, delete, consolidate, move, or otherwise modify any " +
  "skill named in the protected built-ins list (currently: hollow-agent). These " +
  "back load-bearing UX and are filtered out of the candidate list below — " +
  "never resurrect one as an archive or absorb target.\n" +
  "4. DO NOT use usage counters as a reason to skip consolidation. The " +
  "counters are new and often mostly zero. Judge overlap on CONTENT, " +
  "not on use_count. 'use=0' is not evidence a skill is valuable; it's " +
  "absence of evidence either way.\n" +
  "5. DO NOT reject consolidation on the grounds that 'each skill has " +
  "a distinct trigger'. Pairwise distinctness is the wrong bar. The " +
  "right bar is: 'would a human maintainer write this as N separate " +
  "skills, or as one skill with N labeled subsections?' When the " +
  "answer is the latter, merge.\n\n" +
  "How to work — not optional:\n" +
  "1. Scan the full candidate list. Identify PREFIX CLUSTERS (skills " +
  "sharing a first word or domain keyword).\n" +
  "2. For each cluster with 2+ members, do NOT ask 'are these pairs " +
  "overlapping?' — ask 'what is the UMBRELLA CLASS these skills all " +
  "serve? Would a maintainer name that class and write one skill for " +
  "it?' If yes, pick (or create) the umbrella and absorb the siblings " +
  "into it.\n" +
  "3. Three ways to consolidate — use the right one per cluster:\n" +
  "   a. MERGE INTO EXISTING UMBRELLA — one skill in the cluster is " +
  "already broad enough to be the umbrella. Patch it to add a labeled " +
  "section for each sibling's unique insight, then archive the " +
  "siblings.\n" +
  "   b. CREATE A NEW UMBRELLA SKILL.md — no existing member is broad " +
  "enough. Use skill_manage action=create to write a new class-level " +
  "skill whose SKILL.md covers the shared workflow and has short " +
  "labeled subsections. Archive the now-absorbed narrow siblings.\n" +
  "   c. DEMOTE TO REFERENCES/TEMPLATES/SCRIPTS — a sibling has " +
  "narrow-but-valuable session-specific content. Move it into the " +
  "umbrella's appropriate support directory:\n" +
  "      • `references/<topic>.md` for session-specific detail OR " +
  "condensed knowledge banks (quoted research, API docs excerpts, " +
  "domain notes, provider quirks, reproduction recipes)\n" +
  "      • `templates/<name>.<ext>` for starter files meant to be " +
  "copied and modified\n" +
  "      • `scripts/<name>.<ext>` for statically re-runnable actions " +
  "(verification scripts, fixture generators, probes)\n" +
  "      Then archive the old sibling. Use `bash` with `mkdir -p " +
  "~/.hollow/skills/<umbrella>/references/ && mv ... <umbrella>/" +
  "references/<topic>.md` (or templates/ / scripts/).\n\n" +
  "Package integrity — not optional:\n" +
  "Before demoting or archiving a skill, inspect it as a COMPLETE " +
  "directory package, not just SKILL.md. A skill root may include " +
  "`references/`, `templates/`, `scripts/`, and `assets/`; `skill_view` " +
  "discovers those relative to the skill root. A reference markdown file " +
  "inside another skill is NOT a new skill root and does not get its own " +
  "linked-file discovery.\n" +
  "If the source skill has support files OR SKILL.md contains relative " +
  "links such as `references/...`, `templates/...`, `scripts/...`, or " +
  "`assets/...`, DO NOT flatten only SKILL.md into " +
  "`<umbrella>/references/<old>.md`. Choose one safe path instead:\n" +
  "   • keep it as a standalone skill, OR\n" +
  "   • fully merge it by re-homing every needed support file into the " +
  "umbrella's canonical `references/`, `templates/`, `scripts/`, or " +
  "`assets/` directories AND rewrite the destination instructions to " +
  "the new paths, OR\n" +
  "   • archive the entire original skill package unchanged.\n" +
  "Never leave archived/demoted instructions pointing at files that were " +
  "left behind under the old skill directory.\n" +
  "4. Also flag skills whose NAME is too narrow (contains a PR number, " +
  "a feature codename, a specific error string, an 'audit' / " +
  "'diagnosis' / 'salvage' session artifact). These almost always " +
  "belong as a subsection or support file under a class-level umbrella.\n" +
  "5. Iterate. After one consolidation round, scan the remaining set " +
  "and look for the NEXT umbrella opportunity.\n\n" +
  "Your toolset:\n" +
  "  - skills_list, skill_view        — read the current landscape\n" +
  "  - skill_manage action=patch      — add sections to the umbrella\n" +
  "  - skill_manage action=create     — create a new umbrella SKILL.md\n" +
  "  - skill_manage action=write_file — add a references/, templates/, " +
  "or scripts/ file under an existing skill (the skill must already " +
  "exist)\n" +
  "  - skill_manage action=delete     — archive a skill. MUST pass " +
  "`absorbed_into=<umbrella>` when you've merged its content into another " +
  "skill, or `absorbed_into=\"\"` when you're truly pruning with no " +
  "forwarding target.\n" +
  "  - bash                           — mv a sibling into the archive " +
  "OR move its content into a support subfile\n\n" +
  "'keep' is a legitimate decision ONLY when the skill is already a " +
  "class-level umbrella and none of the proposed merges would improve " +
  "discoverability. 'This is narrow but distinct from its siblings' " +
  "is NOT a reason to keep — it's a reason to move it under an " +
  "umbrella as a subsection or support file.\n\n" +
  "When done, write a human summary AND a structured machine-readable " +
  "block so downstream tooling can distinguish consolidation from " +
  "pruning. Format EXACTLY:\n\n" +
  "## Structured summary (required)\n" +
  "```yaml\n" +
  "consolidations:\n" +
  "  - from: <old-skill-name>\n" +
  "    into: <umbrella-skill-name>\n" +
  "    reason: <one short sentence — why merged, not just 'similar'>\n" +
  "prunings:\n" +
  "  - name: <skill-name>\n" +
  "    reason: <one short sentence — why archived with no merge target>\n" +
  "```\n\n" +
  "Every skill you moved to .archive/ MUST appear in exactly one of the " +
  "two lists. If you consolidated X into umbrella Y, X goes under " +
  "`consolidations` with `into: Y`. If you archived X with no absorption " +
  "— truly stale, irrelevant, or obsolete — X goes under `prunings`. " +
  "Leave a list empty (`consolidations: []`) if none. Do not omit the " +
  "block. The block comes AFTER your human-readable summary of clusters " +
  "processed, patches made, and decisions left alone.";

export const curatorPruneBuiltinsNote =
  "\n\nPRUNE-BUILTINS MODE IS ON: bundled built-in skills " +
  "MAY be archived for staleness/irrelevance, overriding hard rule #1 " +
  "for bundled skills ONLY — EXCEPT the protected built-ins (hollow-agent), " +
  "which remain strictly off-limits. Treat a stale built-in the same as " +
  "a stale agent-created skill: archive it (never delete).";

export interface CuratorRunResult {
  StartedAt: Date;
  AutoCounts: CuratorTransitionCounts;
  AutoSummary: string;
}

// MaybeRunCurator runs a curator pass when all gates pass.
export function MaybeRunCurator(cfg: runtime, idleForMs: number, notify: ((msg: string) => void) | null): boolean {
  try {
    if (!ShouldRunCurator(cfg.curator, new Date())) {
      return false;
    }
    if (idleForMs >= 0) {
      const minIdle = cfg.curator.min_idle_hours * 60 * 60 * 1000;
      if (idleForMs < minIdle) {
        return false;
      }
    }
    RunCuratorReview(cfg, false, false, notify);
    return true;
  } catch {
    return false;
  }
}

// RunCuratorReview executes a single curator pass.
export function RunCuratorReview(
  cfg: runtime,
  dryRun: boolean,
  synchronous: boolean,
  notify: ((msg: string) => void) | null
): CuratorRunResult {
  const start = new Date();

  let counts: CuratorTransitionCounts;
  if (dryRun) {
    counts = new CuratorTransitionCounts();
    counts.Checked = AgentCreatedReport().length;
  } else {
    counts = ApplyAutomaticTransitions(cfg.curator, start);
  }
  const autoSummary = counts.Summary();

  const prefix = dryRun ? "dry-run auto: " : "auto: ";
  const st = LoadCuratorState();
  if (!dryRun) {
    st.last_run_at = start.toISOString();
    st.run_count++;
  }
  st.last_run_summary = prefix + autoSummary;
  SaveCuratorState(st);

  const llmPass = async (): Promise<void> => {
    try {
      if (notify !== null) {
        notify("🧹 Curator review running…");
      }

      const finalSummary = await runCuratorLLMPass(cfg, dryRun, prefix, autoSummary, notify);

      const st2 = LoadCuratorState();
      st2.last_run_duration_seconds = (Date.now() - start.getTime()) / 1000;
      st2.last_run_summary = finalSummary;

      const reportPath = writeCuratorReport(start, finalSummary);
      if (reportPath !== "") {
        st2.last_report_path = reportPath;
      }
      SaveCuratorState(st2);

      if (notify !== null) {
        notify("🧹 Curator: " + finalSummary);
      }
    } catch (e) {
      // ignore background crash
    }
  };

  if (synchronous) {
    llmPass().catch(() => {});
  } else {
    // Run background promise
    llmPass().catch(() => {});
  }

  return { StartedAt: start, AutoCounts: counts, AutoSummary: autoSummary };
}

// runCuratorLLMPass spawns the curator fork and returns the run summary.
export async function runCuratorLLMPass(
  cfg: runtime,
  dryRun: boolean,
  prefix: string,
  autoSummary: string,
  notify: ((msg: string) => void) | null
): Promise<string> {
  const candidateList = RenderCuratorCandidateList();
  if (candidateList.includes("No agent-created skills")) {
    return prefix + autoSummary + "; llm: skipped (no candidates)";
  }

  let prompt = curatorReviewPrompt;
  if (cfg.curator.prune_builtins) {
    prompt += curatorPruneBuiltinsNote;
  }
  if (dryRun) {
    prompt = curatorDryRunBanner + "\n\n" + prompt;
  }
  prompt += "\n\n" + candidateList;

  const childCfg = { ...cfg };
  childCfg.compaction = { ...childCfg.compaction, enabled: false };
  childCfg.evidence = { ...childCfg.evidence, enabled: false };
  childCfg.memory = { ...childCfg.memory, nudge_interval: 0, skill_nudge_interval: 0 };

  const curator = new Agent();
  curator.cfg = childCfg;
  curator.client = new_client_for_runtime(childCfg);
  curator.workDir = SkillsDir();
  curator.session = null;
  curator.allowedTools = curatorToolWhitelist;
  curator.writeOrigin = "background_review";
  curator.maxIterations = curatorMaxIterations;
  curator.swarmDepth = 3;
  curator.notify = notify;
  curator.executeTool = curator.executeTool || Agent.prototype.executeTool;

  const cachedPrompt = "You are Hollow's background skill curator. You maintain the " +
    "skill library at ~/.hollow/skills/. Follow the task instructions exactly.";
  curator.cachedSystemPrompt = cachedPrompt;
  curator.messages = [
    { role: "system", content: string_content(cachedPrompt) },
    { role: "user", content: string_content(prompt) },
  ];

  try {
    const controller = new AbortController();
    await curator.runLoop(controller.signal);
  } catch (err: any) {
    return `${prefix}${autoSummary}; llm: error (${err.message || String(err)})`;
  }

  let final = "";
  for (let i = curator.messages.length - 1; i >= 0; i--) {
    const m = curator.messages[i];
    if (m.role === "assistant" && (!m.tool_calls || m.tool_calls.length === 0)) {
      final = content_string(m).trim();
      break;
    }
  }

  let llmSummary = final;
  if (llmSummary.length > 240) {
    llmSummary = llmSummary.slice(0, 240) + "…";
  }
  if (llmSummary === "") {
    llmSummary = "no change";
  }
  return prefix + autoSummary + "; llm: " + llmSummary;
}

// writeCuratorReport writes a per-run report under ~/.hollow/logs/curator/.
export function writeCuratorReport(start: Date, summary: string): string {
  const root = path.join(HomeDir(), "logs", "curator");
  try {
    fs.mkdirSync(root, { recursive: true, mode: 0o700 });
  } catch {
    return "";
  }
  const timestamp = start.toISOString().replace(/T/, "-").replace(/:/g, "").slice(0, 15);
  const filePath = path.join(root, `${timestamp}-REPORT.md`);
  const content = `# Curator run — ${start.toISOString()}\n\n${summary}\n\n## Recovery\n\n` +
    "- Restore an archived skill: /skill-restore <name>\n" +
    "- All archives live under ~/.hollow/skills/.archive/ and are recoverable by mv\n";
  try {
    fs.writeFileSync(filePath, content, { mode: 0o600 });
    return filePath;
  } catch {
    return "";
  }
}

/*
PORT STATUS
source path: backend/agent/curator.go
source lines: 370
draft lines: 250
confidence: high
status: phase_b_compile
*/
