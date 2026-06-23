// PORT: backend/agent/parallel_fork.go

import { spawnSync } from "node:child_process";
import path from "node:path";
import fs from "node:fs";
import os from "node:os";
import { type message } from "../opencode/types";
import { string_content } from "../opencode/types";
import { Effect } from "effect";
import { resolve_bash } from "../shell/resolve";
import { resolve_safe_cwd } from "../shell/cwd";
import { parallelForkNotice } from "./goal_lock";
import { Agent, type swarmWorkerResult } from "./agent";

const parallelForkMaxTurns = 20;

const forkAngles = [
  "Approach A: minimal diff — fix the root cause only.",
  "Approach B: re-read the failing code path and patch the smallest broken unit.",
  "Approach C: add a quick diagnostic, then fix with evidence from its output.",
  "Approach D: try an alternative implementation that still satisfies the goal.",
];

interface forkOutcome {
  index: number;
  workDir: string;
  exitCode: number;
  output: string;
  worker: swarmWorkerResult;
}

export async function runParallelForks(
  this: Agent,
  ctx: AbortSignal,
  lockedGoal: string,
  lastFailure: string,
  verifyCmd: string,
  count: number
): Promise<[string, boolean]> {
  let repoRoot = "";
  try {
    repoRoot = repoRootOf(this.workDir);
  } catch (err) {
    return [
      "No git repo — parallel forks skipped (use agent_swarm with isolate=worktree for manual parallel attempts).",
      false,
    ];
  }

  const runID = String(Date.now()) + String(Math.floor(Math.random() * 1000));
  const slots: { dir: string; branch: string }[] = [];

  const cleanup = () => {
    for (const s of slots) {
      try {
        git(repoRoot, "worktree", "remove", "--force", s.dir);
      } catch {}
      try {
        git(repoRoot, "branch", "-D", s.branch);
      } catch {}
    }
  };

  for (let i = 0; i < count; i++) {
    if (ctx.aborted) {
      cleanup();
      return ["Parallel forks aborted.", false];
    }
    const id = `fork-${i + 1}`;
    const branch = `hollow-fork/${runID}/${id}`;
    const base = path.join(os.tmpdir(), "hollow-fork-" + runID);
    const dir = path.join(base, id);
    try {
      fs.mkdirSync(base, { recursive: true, mode: 0o755 });
      git(repoRoot, "worktree", "add", "-b", branch, dir, "HEAD");
      slots.push({ dir, branch });
    } catch {
      continue;
    }
  }

  if (slots.length === 0) {
    cleanup();
    return ["Could not create git worktrees for parallel forks.", false];
  }

  const tasks = slots.map((s, i) => {
    const angle = forkAngles[i % forkAngles.length];
    return {
      ID: `fork-${i + 1}`,
      Prompt: buildForkPrompt(lockedGoal, lastFailure, verifyCmd, angle),
    };
  });

  const outcomes: forkOutcome[] = [];
  const promises = tasks.map(async (task, i) => {
    if (ctx.aborted) {
      return;
    }
    const dir = slots[i].dir;
    const worker = new Agent();
    worker.cfg = this.cfg;
    worker.client = this.client;
    worker.workDir = dir;
    worker.swarmDepth = 1;

    try {
      const result = await this.runSwarmWorkerInDir(
        ctx,
        task,
        i,
        1,
        "",
        0,
        parallelForkMaxTurns,
        dir
      );
      const [exitCode, verifyOut] = await runBashInDir(dir, verifyCmd);

      outcomes.push({
        index: i,
        workDir: dir,
        exitCode,
        output: verifyOut,
        worker: result,
      });
    } catch {}
  });

  await Promise.all(promises);

  let winner: forkOutcome | undefined;
  for (const o of outcomes) {
    if (o.exitCode === 0) {
      winner = o;
      break;
    }
  }

  let summary = `Workers: ${outcomes.length}.`;
  outcomes.forEach((o) => {
    const status = o.exitCode === 0 ? "pass" : "fail";
    summary += `\n- fork-${o.index + 1}: verify=${status} worker=${o.worker.Status}`;
  });

  if (!winner) {
    cleanup();
    return [summary, false];
  }

  try {
    syncWorktreeChanges(winner.workDir, this.workDir);
  } catch (err: any) {
    cleanup();
    return [summary + "\nWinner found but merge failed: " + (err.message || String(err)), false];
  }

  cleanup();
  this.noteMutation();
  return [summary + `\nApplied patch from fork-${winner.index + 1}.`, true];
}

Agent.prototype.runParallelForks = runParallelForks;

function buildForkPrompt(lockedGoal: string, lastFailure: string, verifyCmd: string, angle: string): string {
  let out = "GOAL LOCK — complete exactly this task:\n" + lockedGoal;
  out += "\n\nVerification has failed repeatedly. ";
  if (lastFailure !== "") {
    out += "Latest failure output:\n" + lastFailure + "\n\n";
  }
  if (verifyCmd !== "") {
    out += "Verification command: " + verifyCmd + "\n\n";
  }
  out += angle + "\nRun verification before finishing.";
  return out;
}

async function runBashInDir(workDir: string, command: string): Promise<[number, string]> {
  command = command.trim();
  if (command === "") {
    return [-1, "no verify command"];
  }

  let bashExe: string;
  try {
    bashExe = Effect.runSync(resolve_bash());
  } catch (err: any) {
    return [-1, err.message || String(err)];
  }

  const safeCwd = resolve_safe_cwd(workDir);
  const args = ["-c", command];

  const res = spawnSync(bashExe, args, {
    cwd: safeCwd,
    encoding: "utf8",
    env: {
      ...process.env,
      TERM: "dumb",
      NO_COLOR: "1",
      CLICOLOR: "0",
      FORCE_COLOR: "0",
    },
  });

  const exitCode = res.status ?? -1;
  const output = (res.stdout || "") + (res.stderr || "");
  return [exitCode, output];
}

function syncWorktreeChanges(srcDir: string, dstDir: string): void {
  const [status, err] = gitOutput(srcDir, "status", "--porcelain");
  if (err) {
    throw err;
  }
  if (status.trim() === "") {
    throw new Error("winning worktree has no changes");
  }
  const lines = status.split("\n");
  for (let line of lines) {
    line = line.trim();
    if (line === "") {
      continue;
    }
    const pathStr = line.slice(3).trim();
    if (pathStr === "") {
      continue;
    }
    copyFile(path.join(srcDir, pathStr), path.join(dstDir, pathStr));
  }
}

function copyFile(src: string, dst: string): void {
  const data = fs.readFileSync(src);
  fs.mkdirSync(path.dirname(dst), { recursive: true, mode: 0o755 });
  fs.writeFileSync(dst, data);
}

function git(cwd: string, ...args: string[]): void {
  const [_, err] = gitOutput(cwd, ...args);
  if (err) {
    throw err;
  }
}

function gitOutput(cwd: string, ...args: string[]): [string, Error | null] {
  const res = spawnSync("git", args, {
    cwd,
    encoding: "utf8",
  });
  if (res.status !== 0) {
    const errMsg = res.stderr ? res.stderr.trim() : (res.error ? res.error.message : "unknown git error");
    return ["", new Error(`git ${args.join(" ")}: ${errMsg}`)];
  }
  return [res.stdout ? res.stdout.trim() : "", null];
}

function repoRootOf(workDir: string): string {
  const [root, err] = gitOutput(workDir, "rev-parse", "--show-toplevel");
  if (err) {
    throw err;
  }
  return root;
}

/*
PORT STATUS
source path: backend/agent/parallel_fork.go
source lines: 238
draft lines: 232
confidence: high
status: phase_b_compile
*/
