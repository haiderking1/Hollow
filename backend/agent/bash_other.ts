// PORT: backend/agent/bash_other.go

import { ChildProcess } from "node:child_process";

// configureProcGroup is a no-op on Node.js after process is spawned,
// because process attributes must be passed at spawn time.
export function configureProcGroup(cmd: ChildProcess): void {
  // no-op
}

export function killProcessGroup(cmd: ChildProcess): Error | null {
  if (!cmd || !cmd.pid) {
    return null;
  }
  try {
    cmd.kill("SIGKILL");
    return null;
  } catch (err) {
    return err instanceof Error ? err : new Error(String(err));
  }
}

/*
PORT STATUS
source path: backend/agent/bash_other.go
source lines: 19
draft lines: 21
confidence: high
status: phase_b_compile
*/
