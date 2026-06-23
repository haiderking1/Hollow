// PORT: backend/agent/write_file.go

import { Effect } from "effect";
import fs from "node:fs/promises";
import path from "node:path";
import { type tool } from "../opencode/types";
import { Agent, type toolResult } from "./agent";

export function writeFileTool(): tool {
  const schema = {
    type: "object",
    properties: {
      path: { type: "string" },
      content: { type: "string" },
    },
    required: ["path", "content"],
  };
  return {
    type: "function",
    function: {
      name: "write_file",
      description: "Write content to a file in the workspace",
      parameters: new TextEncoder().encode(JSON.stringify(schema)),
    },
  };
}

Agent.prototype.toolWriteFile = function (
  this: Agent,
  argsJSON: string
): Effect.Effect<toolResult, Error> {
  return Effect.gen(this, function* () {
    let args: { path: string; content: string };
    try {
      args = JSON.parse(argsJSON);
    } catch (err) {
      return { output: err instanceof Error ? err.message : String(err), isErr: true };
    }

    const resolvedPath = yield* this.resolvePath(args.path);

    yield* Effect.tryPromise({
      try: async () => {
        await fs.mkdir(path.dirname(resolvedPath), { recursive: true, mode: 0o755 });
        await fs.writeFile(resolvedPath, args.content, { mode: 0o644 });
      },
      catch: (cause) => (cause instanceof Error ? cause : new Error(String(cause))),
    });

    return {
      output: `wrote ${args.content.length} bytes to ${resolvedPath}`,
    };
  }).pipe(
    Effect.catchAll((err) =>
      Effect.succeed({ output: err.message, isErr: true })
    )
  );
};

/*
PORT STATUS
source path: backend/agent/write_file.go
source lines: 54
draft lines: 59
confidence: high
status: phase_b_compile
*/
