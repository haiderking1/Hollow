// PORT: backend/agent/bash_windows.go

import { ChildProcess, spawnSync } from "node:child_process";

// configureProcGroup is a no-op on Node.js after process is spawned,
// because process attributes must be passed at spawn time.
export function configureProcGroup(cmd: ChildProcess): void {
  // no-op
}

export function killProcessGroup(cmd: ChildProcess): Error | null {
  if (!cmd || !cmd.pid) {
    return null;
  }
  const pid = cmd.pid;
  try {
    const res = spawnSync("taskkill", ["/PID", String(pid), "/T", "/F"], {
      windowsHide: true,
      timeout: 10000,
    });
    if (res.error) {
      cmd.kill("SIGKILL");
      return res.error;
    }
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
source path: backend/agent/bash_windows.go
source lines: 47
draft lines: 32
confidence: high
status: phase_b_compile
*/
