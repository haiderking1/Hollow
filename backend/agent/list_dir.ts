// PORT: backend/agent/list_dir.go

import { Effect } from "effect";
import fs from "node:fs/promises";
import { type tool } from "../opencode/types";
import { Agent, type toolResult } from "./agent";

export function listDirTool(): tool {
  const schema = {
    type: "object",
    properties: {
      path: { type: "string", description: "Directory path, default ." },
    },
  };
  return {
    type: "function",
    function: {
      name: "list_dir",
      description: "List entries in a directory",
      parameters: new TextEncoder().encode(JSON.stringify(schema)),
    },
  };
}

Agent.prototype.toolListDir = function (
  this: Agent,
  argsJSON: string
): Effect.Effect<toolResult, Error> {
  return Effect.gen(this, function* () {
    let args: { path?: string };
    try {
      args = JSON.parse(argsJSON);
    } catch (err) {
      return { output: err instanceof Error ? err.message : String(err), isErr: true };
    }

    const dirPath = args.path || ".";
    const resolvedPath = yield* this.resolvePath(dirPath);

    const entries = yield* Effect.tryPromise({
      try: async () => {
        return await fs.readdir(resolvedPath, { withFileTypes: true });
      },
      catch: (cause) => (cause instanceof Error ? cause : new Error(String(cause))),
    });

    const lines: string[] = [];
    for (const entry of entries) {
      if (entry.isDirectory()) {
        lines.push(entry.name + "/");
      } else {
        lines.push(entry.name);
      }
    }

    return {
      output: lines.length > 0 ? lines.join("\n") + "\n" : "",
    };
  }).pipe(
    Effect.catchAll((err) =>
      Effect.succeed({ output: err.message, isErr: true })
    )
  );
};

/*
PORT STATUS
source path: backend/agent/list_dir.go
source lines: 62
draft lines: 62
confidence: high
status: phase_b_compile
*/
