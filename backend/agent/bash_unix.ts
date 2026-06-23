// PORT: backend/agent/bash_unix.go

import { ChildProcess } from "node:child_process";
import process from "node:process";

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
    // Negative pid targets the process group
    process.kill(-cmd.pid, "SIGKILL");
    return null;
  } catch (err) {
    try {
      cmd.kill("SIGKILL");
    } catch {}
    return err instanceof Error ? err : new Error(String(err));
  }
}

/*
PORT STATUS
source path: backend/agent/bash_unix.go
source lines: 29
draft lines: 27
confidence: high
status: phase_b_compile
*/
