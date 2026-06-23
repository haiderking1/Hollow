// PORT: backend/agent/goal_lock.go

import { runtime_notice_prefix } from "../core/events";

export function goalLockNotice(lockedGoal: string): string {
  return `${runtime_notice_prefix}GOAL LOCK — complete exactly this user task this turn.\n` +
    "Do not pivot scope, propose alternatives, or declare done without verification.\n" +
    "If blocked, try a different execution path on the SAME goal.\n\n" +
    lockedGoal;
}

export function parallelForkNotice(forkCount: number, lockedGoal: string, summary: string): string {
  return `${runtime_notice_prefix}PARALLEL FORKS — ${forkCount} same-model attempts ran on the locked goal after repeated verify failures.\n` +
    summary +
    "\n\n" +
    lockedGoal;
}

export function goalLockReminder(lockedGoal: string): string {
  if (lockedGoal.trim() === "") {
    return "";
  }
  return "\nGOAL LOCK: " + lockedGoal;
}

/*
PORT STATUS
source path: backend/agent/goal_lock.go
source lines: 36
draft lines: 25
confidence: high
status: phase_b_compile
*/
