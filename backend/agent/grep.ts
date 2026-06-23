// PORT: backend/agent/grep.go

import { Effect } from "effect";
import fs from "node:fs/promises";
import * as fsSync from "node:fs";
import readline from "node:readline";
import path from "node:path";
import { type tool } from "../opencode/types";
import { Agent, type toolResult } from "./agent";
import { shouldSkipDir, matchSegmentPattern } from "./glob";

export function grepTool(): tool {
  const schema = {
    type: "object",
    properties: {
      pattern: { type: "string", description: "Regular expression to search for" },
      path: { type: "string", description: "Directory to search in (default workspace root)" },
      include: { type: "string", description: "Glob to filter files, e.g. *.go" }
    },
    required: ["pattern"]
  };
  return {
    type: "function",
    function: {
      name: "grep",
      description: "Search file contents using a regular expression. Returns matching lines as path:line:text, workspace-relative. Use the optional include glob to limit the file types searched (e.g. \"*.go\"). Prefer this over bash grep for searching the codebase.",
      parameters: new TextEncoder().encode(JSON.stringify(schema)),
    },
  };
}

const maxGrepMatches = 200;
const maxGrepFileSize = 2_000_000;

Agent.prototype.toolGrep = function (
  this: Agent,
  ctx: AbortSignal,
  argsJSON: string
): Effect.Effect<toolResult, Error> {
  return Effect.gen(this, function* () {
    let args: { pattern: string; path?: string; include?: string };
    try {
      args = JSON.parse(argsJSON);
    } catch (err) {
      return { output: err instanceof Error ? err.message : String(err), isErr: true };
    }

    if (!args.pattern || args.pattern.trim() === "") {
      return { output: "pattern is required", isErr: true };
    }

    let re: RegExp;
    try {
      re = new RegExp(args.pattern);
    } catch (err) {
      return { output: `invalid pattern: ${err instanceof Error ? err.message : String(err)}`, isErr: true };
    }

    const root = args.path || ".";
    const rootAbs = yield* this.resolvePath(root);

    const matches: string[] = [];
    const state = { truncated: false };

    yield* Effect.tryPromise({
      try: async () => {
        await walkGrep(ctx, rootAbs, rootAbs, this.workDir, re, args.include || "", matches, state);
      },
      catch: (cause) => (cause instanceof Error ? cause : new Error(String(cause))),
    });

    if (ctx.aborted) {
      return { output: "[interrupted]", isErr: true };
    }

    if (matches.length === 0) {
      return { output: "no matches" };
    }

    let out = matches.join("\n");
    if (state.truncated) {
      out += `\n... truncated at ${maxGrepMatches} ...`;
    }

    return {
      output: out,
    };
  }).pipe(
    Effect.catchAll((err) =>
      Effect.succeed({ output: err.message, isErr: true })
    )
  );
};

async function walkGrep(
  ctx: AbortSignal,
  dir: string,
  rootAbs: string,
  workDir: string,
  re: RegExp,
  include: string,
  matches: string[],
  state: { truncated: boolean }
): Promise<void> {
  if (ctx.aborted || matches.length >= maxGrepMatches) {
    return;
  }

  let entries: fsSync.Dirent[];
  try {
    entries = await fs.readdir(dir, { withFileTypes: true });
  } catch {
    return;
  }

  for (const entry of entries) {
    if (ctx.aborted || matches.length >= maxGrepMatches) {
      break;
    }
    const fullPath = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      if (shouldSkipDir(entry.name)) {
        continue;
      }
      await walkGrep(ctx, fullPath, rootAbs, workDir, re, include, matches, state);
    } else {
      if (include !== "") {
        if (!matchSegmentPattern(include, entry.name)) {
          continue;
        }
      }

      let stat: fsSync.Stats;
      try {
        stat = await fs.stat(fullPath);
      } catch {
        continue;
      }
      if (stat.size > maxGrepFileSize) {
        continue;
      }

      let rel = path.relative(workDir, fullPath);
      rel = rel.split(path.sep).join("/");

      const fileStream = fsSync.createReadStream(fullPath);
      const rl = readline.createInterface({
        input: fileStream,
        crlfDelay: Infinity,
      });

      let lineNo = 0;
      try {
        for await (const line of rl) {
          if (ctx.aborted) {
            break;
          }
          lineNo++;
          if (re.test(line)) {
            matches.push(`${rel}:${lineNo}:${line.trim()}`);
            if (matches.length >= maxGrepMatches) {
              state.truncated = true;
              break;
            }
          }
        }
      } catch {
        // ignore
      } finally {
        rl.close();
        fileStream.destroy();
      }
    }
  }
}

/*
PORT STATUS
source path: backend/agent/grep.go
source lines: 142
draft lines: 172
confidence: high
status: phase_b_compile
*/
