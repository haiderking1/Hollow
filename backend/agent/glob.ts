// PORT: backend/agent/glob.go

import { Effect } from "effect";
import fs from "node:fs/promises";
import * as fsSync from "node:fs";
import path from "node:path";
import { type tool } from "../opencode/types";
import { Agent, type toolResult } from "./agent";

export function globTool(): tool {
  const schema = {
    type: "object",
    properties: {
      pattern: { type: "string", description: "Glob pattern, e.g. **/*.go" },
      path: { type: "string", description: "Directory to search in (default workspace root)" }
    },
    required: ["pattern"]
  };
  return {
    type: "function",
    function: {
      name: "glob",
      description: "Fast file pattern matching. Returns workspace-relative paths matching a glob pattern, sorted alphabetically. Supports ** for recursive matching (e.g. \"**/*.go\", \"src/**/*.ts\"). Use this when you know part of a filename or extension.",
      parameters: new TextEncoder().encode(JSON.stringify(schema)),
    },
  };
}

const maxGlobResults = 200;

Agent.prototype.toolGlob = function (
  this: Agent,
  argsJSON: string
): Effect.Effect<toolResult, Error> {
  return Effect.gen(this, function* () {
    let args: { pattern: string; path?: string };
    try {
      args = JSON.parse(argsJSON);
    } catch (err) {
      return { output: err instanceof Error ? err.message : String(err), isErr: true };
    }

    if (!args.pattern || args.pattern.trim() === "") {
      return { output: "pattern is required", isErr: true };
    }

    const root = args.path || ".";
    const rootAbs = yield* this.resolvePath(root);

    const matches: string[] = [];
    yield* Effect.tryPromise({
      try: async () => {
        await walk(rootAbs, rootAbs, this.workDir, args.pattern, matches);
      },
      catch: (cause) => (cause instanceof Error ? cause : new Error(String(cause))),
    });

    if (matches.length === 0) {
      return { output: "no matches" };
    }

    matches.sort();
    let out = matches.join("\n");
    if (matches.length >= maxGlobResults) {
      out += `\n... truncated at ${maxGlobResults} matches ...`;
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

export function shouldSkipDir(name: string): boolean {
  switch (name) {
    case ".git":
    case "node_modules":
    case "vendor":
    case ".idea":
    case ".vscode":
    case "dist":
    case "build":
      return true;
  }
  return false;
}

export function matchSegmentPattern(pattern: string, segment: string): boolean {
  const rxStr = "^" + pattern
    .replace(/[-\/\\^$*+?.()|[\]{}]/g, (m) => {
      if (m === "*") return ".*";
      if (m === "?") return ".";
      if (m === "[" || m === "]") return m;
      return "\\" + m;
    }) + "$";
  try {
    const rx = new RegExp(rxStr);
    return rx.test(segment);
  } catch {
    return false;
  }
}

export function matchSegments(pat: string[], name: string[]): boolean {
  let pIdx = 0;
  let nIdx = 0;
  while (pIdx < pat.length) {
    if (pat[pIdx] === "**") {
      const rest = pat.slice(pIdx + 1);
      if (rest.length === 0) {
        return true;
      }
      for (let i = nIdx; i <= name.length; i++) {
        if (matchSegments(rest, name.slice(i))) {
          return true;
        }
      }
      return false;
    }
    if (nIdx >= name.length) {
      return false;
    }
    if (!matchSegmentPattern(pat[pIdx], name[nIdx])) {
      return false;
    }
    pIdx++;
    nIdx++;
  }
  return nIdx === name.length;
}

export function matchGlob(pattern: string, filepathStr: string): boolean {
  if (!pattern.includes("/") && !pattern.includes("**")) {
    const base = path.basename(filepathStr);
    if (matchSegmentPattern(pattern, base)) {
      return true;
    }
  }
  return matchSegments(pattern.split("/"), filepathStr.split("/"));
}

async function walk(
  dir: string,
  rootAbs: string,
  workDir: string,
  pattern: string,
  matches: string[]
): Promise<void> {
  if (matches.length >= maxGlobResults) {
    return;
  }
  let entries: fsSync.Dirent[];
  try {
    entries = await fs.readdir(dir, { withFileTypes: true });
  } catch {
    return;
  }

  for (const entry of entries) {
    if (matches.length >= maxGlobResults) {
      break;
    }
    const fullPath = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      if (shouldSkipDir(entry.name)) {
        continue;
      }
      await walk(fullPath, rootAbs, workDir, pattern, matches);
    } else {
      let rel = path.relative(rootAbs, fullPath);
      rel = rel.split(path.sep).join("/");
      if (matchGlob(pattern, rel)) {
        let workRel = path.relative(workDir, fullPath);
        workRel = workRel.split(path.sep).join("/");
        matches.push(workRel);
      }
    }
  }
}

/*
PORT STATUS
source path: backend/agent/glob.go
source lines: 146
draft lines: 181
confidence: high
status: phase_b_compile
*/
