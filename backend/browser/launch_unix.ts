// PORT: backend/browser/launch_unix.go

import { type SpawnOptions } from "node:child_process";

export function detachProcess(opts: SpawnOptions): void {
  opts.detached = true;
}

/*
PORT STATUS
source path: backend/browser/launch_unix.go
source lines: 16
draft lines: 10
confidence: high
status: phase_b_compile
*/
