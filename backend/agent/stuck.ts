// PORT: backend/agent/stuck.go

import { Agent } from "./agent";
import { type message, string_content } from "../opencode/types";
import { parallel_forks_enabled, stuck_threshold, fork_count } from "../config/config";
import { parallelForkNotice } from "./goal_lock";

Agent.prototype.noteVerifyFailure = function (this: Agent): void {
  this.verifyFailures++;
  const failures = this.verifyFailures;
  const shouldFork =
    !this.parallelForksAttempted &&
    parallel_forks_enabled(this.cfg.evidence) &&
    this.swarmDepth === 0 &&
    failures >= stuck_threshold(this.cfg.evidence);

  if (shouldFork) {
    this.maybeParallelForks();
  }
};

Agent.prototype.noteVerifySuccess = function (this: Agent): void {
  this.verifyFailures = 0;
  this.step.lastVerifyFailed = false;
  this.step.failurePaths = null;
};

Agent.prototype.maybeParallelForks = async function (this: Agent): Promise<void> {
  if (this.parallelForksAttempted || !parallel_forks_enabled(this.cfg.evidence) || this.swarmDepth > 0) {
    return;
  }
  if (this.verifyFailures < stuck_threshold(this.cfg.evidence)) {
    return;
  }
  this.parallelForksAttempted = true;
  const lockedGoal = this.lockedGoal;
  const lastOutput = this.step.lastVerifyOutput;
  let verifyCmd = "";
  if (this.obligations) {
    try {
      verifyCmd = this.obligations.verify_command();
    } catch {}
  }
  const fCount = fork_count(this.cfg.evidence);
  const ctx = this.turnCtx;
  if (!ctx) {
    return;
  }

  const [summary, merged] = await this.runParallelForks(ctx, lockedGoal, lastOutput, verifyCmd, fCount);

  if (this.emit) {
    let msg = `parallel forks: ${fCount} workers`;
    if (merged) {
      msg += " — winning patch applied";
    } else {
      msg += " — no passing patch yet";
    }
    this.emit({ kind: "system", data: msg });
  }

  const notice = parallelForkNotice(fCount, lockedGoal, summary);
  const inject: message = {
    role: "user",
    content: string_content(notice),
  };
  this.messages.push(inject);
  this.persist(inject);
};

/*
PORT STATUS
source path: backend/agent/stuck.go
source lines: 77
draft lines: 77
confidence: high
status: phase_b_compile
*/
