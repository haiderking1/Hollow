// PORT: backend/agent/step_score.go

import path from "node:path";
import { Effect } from "effect";
import { type toolResult } from "./agent";
import { Agent } from "./agent";
import { is_verify_command } from "./obligations/match";

const failurePathRE = /(?:(?:^|\s|\()([\w./-]+\.(?:go|py|rs|js|ts|tsx|jsx|java|c|h|cpp|hpp|rb|php|cs|swift|kt|scala|sql|yaml|yml|toml|json|md|sh))(?::\d+(?::\d+)?)?)|File "([^"]+)"/g;

export function scoreToolStep(
  this: Agent,
  name: string,
  argsJSON: string,
  result: toolResult
): toolResult | null {
  if (this.swarmDepth > 0 || !this.evidenceEnabled() || !this.cfg.evidence?.step_scorer) {
    this.recordStepOutcome(name, argsJSON, result);
    return null;
  }

  const tracker = this.step;

  switch (name) {
    case "bash": {
      const cmd = bashCommandArg(argsJSON);
      if (result.isErr && cmd !== "" && tracker.lastBashFailed && cmd === tracker.lastBashCommand) {
        return {
          output: "REJECTED: repeated failing command — change approach or fix the failure site before re-running",
          isErr: true,
        };
      }
      break;
    }
    case "write_file":
    case "edit_file": {
      if (!tracker.lastVerifyFailed || !tracker.failurePaths || tracker.failurePaths.length === 0) {
        break;
      }
      const [filePath, ok] = toolPathArg(argsJSON);
      if (!ok) {
        break;
      }
      // resolvePath returns an Effect. We run it synchronously using Effect.runSync.
      let abs = "";
      try {
        abs = Effect_runSync_resolvePath(this, filePath);
      } catch {
        break;
      }
      if (!editTouchesFailureSite(abs, tracker.failurePaths, this.evidenceLedger().mutated_paths())) {
        return {
          output: "REJECTED: edit does not touch the failure site — last verify failed on: " +
            tracker.failurePaths.join(", "),
          isErr: true,
        };
      }
      break;
    }
  }

  this.recordStepOutcome(name, argsJSON, result);
  return null;
}

Agent.prototype.scoreToolStep = scoreToolStep;

// Helper to run resolvePath synchronously
function Effect_runSync_resolvePath(agent: Agent, p: string): string {
  return Effect.runSync(agent.resolvePath(p));
}

export function recordStepOutcome(
  this: Agent,
  name: string,
  argsJSON: string,
  result: toolResult
): void {
  if (name !== "bash") {
    return;
  }
  const cmd = bashCommandArg(argsJSON);
  if (cmd === "") {
    return;
  }

  this.step.lastBashCommand = cmd;
  this.step.lastBashFailed = !!result.isErr;

  const reg = this.obligations;
  const isVerify = reg !== null && is_verify_command(cmd, reg.verify_command(), reg.extra_verify_commands());
  if (!isVerify) {
    return;
  }

  if (result.isErr) {
    this.step.lastVerifyFailed = true;
    this.step.lastVerifyOutput = truncateStepOutput(result.output);
    this.step.failurePaths = extractFailurePaths(result.output, this.evidenceLedger().mutated_paths());
  } else {
    this.step.lastVerifyFailed = false;
    this.step.lastVerifyOutput = "";
    this.step.failurePaths = null;
  }
}

Agent.prototype.recordStepOutcome = recordStepOutcome;

function bashCommandArg(argsJSON: string): string {
  let args: { command?: string };
  try {
    args = JSON.parse(argsJSON);
  } catch {
    return "";
  }
  return (args.command || "").trim();
}

function firstNonEmpty(a?: string, b?: string): string {
  if (a && a !== "") {
    return a;
  }
  return b || "";
}

function extractFailurePaths(output: string, mutated: string[]): string[] {
  const seen = new Set<string>();
  const paths: string[] = [];
  const add = (p: string) => {
    p = p.trim();
    if (p === "" || seen.has(p)) {
      return;
    }
    seen.add(p);
    paths.push(p);
  };

  failurePathRE.lastIndex = 0;
  const matches = [...output.matchAll(failurePathRE)];
  for (const m of matches) {
    add(firstNonEmpty(m[1], m[2]));
  }
  for (const p of mutated) {
    const toSlash = p.replaceAll("\\", "/");
    add(toSlash);
    add(path.basename(p));
  }
  return paths;
}

function editTouchesFailureSite(editPath: string, failurePaths: string[], mutated: string[]): boolean {
  const cleanEdit = path.normalize(editPath);
  for (const fp of failurePaths) {
    const cleanFp = path.normalize(fp);
    if (
      cleanFp === cleanEdit ||
      cleanEdit.endsWith(cleanFp) ||
      cleanFp.endsWith(path.basename(cleanEdit))
    ) {
      return true;
    }
    const base = path.basename(cleanFp);
    if (base !== "" && cleanEdit.includes(base)) {
      return true;
    }
  }
  for (const mp of mutated) {
    if (path.normalize(mp) === cleanEdit) {
      return true;
    }
  }
  return false;
}

function truncateStepOutput(s: string): string {
  const max = 4000;
  if (s.length <= max) {
    return s;
  }
  return s.slice(0, max) + "\n... truncated ...";
}

function toolPathArg(argsJSON: string): [string, boolean] {
  let args: { path?: string };
  try {
    args = JSON.parse(argsJSON);
  } catch {
    return ["", false];
  }
  if (!args.path || args.path === "") {
    return ["", false];
  }
  return [args.path, true];
}

/*
PORT STATUS
source path: backend/agent/step_score.go
source lines: 155
draft lines: 182
confidence: high
status: phase_b_compile
*/
