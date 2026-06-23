// PORT: backend/agent/tool_path.go

import { Effect } from "effect";
import path from "node:path";
import { Agent } from "./agent";

Agent.prototype.resolvePath = function (this: Agent, p: string): Effect.Effect<string, Error> {
  if (p === "") {
    return Effect.fail(new Error("path is required"));
  }

  let abs: string;
  if (path.isAbsolute(p)) {
    abs = path.normalize(p);
  } else {
    abs = path.normalize(path.join(this.workDir, p));
  }

  return Effect.try({
    try: () => {
      return path.resolve(abs);
    },
    catch: (cause) => (cause instanceof Error ? cause : new Error(String(cause))),
  });
};

/*
PORT STATUS
source path: backend/agent/tool_path.go
source lines: 31
draft lines: 31
confidence: high
status: phase_b_compile
*/
