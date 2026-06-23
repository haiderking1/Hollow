// PORT: backend/workflow/discover.go

import { Effect } from "effect";
import fs from "node:fs";
import path from "node:path";

interface ScriptCandidate {
  path: string;
  modTime: number;
}

export function FindLatestScript(workDir: string): Effect.Effect<string, Error> {
  return Effect.try({
    try: () => {
      const root = path.join(workDir, ".hollow", "workflows");
      if (!fs.existsSync(root)) {
        throw new Error(`no workflow scripts found under ${root}`);
      }

      const entries = fs.readdirSync(root, { withFileTypes: true });
      const candidates: ScriptCandidate[] = [];

      for (const entry of entries) {
        if (entry.isDirectory()) {
          const wfPath = path.join(root, entry.name, "workflow.js");
          try {
            const stat = fs.statSync(wfPath);
            if (stat.isFile()) {
              candidates.push({
                path: wfPath,
                modTime: stat.mtime.getTime(),
              });
            }
          } catch {}
        }
      }

      if (candidates.length === 0) {
        throw new Error(`no workflow scripts found under ${root}`);
      }

      candidates.sort((a, b) => b.modTime - a.modTime);
      return candidates[0].path;
    },
    catch: (cause) => (cause instanceof Error ? cause : new Error(String(cause))),
  });
}

export function ResolveScriptPath(workDir: string, arg: string): Effect.Effect<string, Error> {
  return Effect.try({
    try: () => {
      const trimmed = arg.trim();
      if (trimmed === "") {
        throw new Error("empty workflow path");
      }

      let scriptPath = trimmed;
      if (!path.isAbsolute(scriptPath)) {
        scriptPath = path.join(workDir, scriptPath);
      }
      scriptPath = path.normalize(scriptPath);

      let stat = fs.statSync(scriptPath);
      if (stat.isDirectory()) {
        scriptPath = path.join(scriptPath, "workflow.js");
        stat = fs.statSync(scriptPath);
      }

      if (stat.isDirectory()) {
        throw new Error(`workflow path is a directory: ${scriptPath}`);
      }

      return scriptPath;
    },
    catch: (cause) => (cause instanceof Error ? cause : new Error(String(cause))),
  });
}

/*
PORT STATUS
source path: backend/workflow/discover.go
source lines: 67
draft lines: 83
confidence: high
status: phase_b_compile
*/
