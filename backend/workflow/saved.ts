// PORT: backend/workflow/saved.go

import { Effect } from "effect";
import fs from "node:fs";
import path from "node:path";
import { home_dir } from "../hollowhome/home";
import { type Meta } from "./types";
import { Inspect } from "./runtime";

export interface SavedWorkflow {
  Name: string;
  Path: string;
  Project: boolean;
  Meta: Meta;
}

const savedNamePattern = /^[a-z0-9][a-z0-9-]{0,63}$/;

export function ScanSaved(workDir: string): SavedWorkflow[] {
  const byName: Record<string, SavedWorkflow> = {};
  const homeRoot = path.join(home_dir(), "workflows", "saved");
  scanSavedRoot(homeRoot, false, byName);
  scanSavedRoot(path.join(workDir, ".hollow", "workflows", "saved"), true, byName);

  const out: SavedWorkflow[] = Object.values(byName);
  out.sort((a, b) => a.Name.localeCompare(b.Name));
  return out;
}

function scanSavedRoot(root: string, project: boolean, out: Record<string, SavedWorkflow>): void {
  let entries: fs.Dirent[];
  try {
    entries = fs.readdirSync(root, { withFileTypes: true });
  } catch {
    return;
  }
  for (const entry of entries) {
    if (!entry.isDirectory()) {
      continue;
    }
    const name = entry.name;
    const wfPath = path.join(root, name, "workflow.js");
    try {
      if (!fs.statSync(wfPath).isFile()) {
        continue;
      }
    } catch {
      continue;
    }
    const item: SavedWorkflow = {
      Name: name,
      Path: wfPath,
      Project: project,
      Meta: { name: "", description: "" },
    };
    try {
      const metaData = fs.readFileSync(path.join(root, name, "meta.json"), "utf8");
      item.Meta = JSON.parse(metaData);
    } catch {}
    out[name] = item;
  }
}

export function SaveWorkflow(
  scriptPath: string,
  name: string,
  workDir: string,
  project: boolean
): Effect.Effect<SavedWorkflow, Error> {
  return Effect.tryPromise({
    try: async () => {
      const normalizedName = name.trim().toLowerCase();
      if (!savedNamePattern.test(normalizedName)) {
        throw new Error("workflow name must use lowercase letters, digits, and hyphens");
      }

      let data: Buffer;
      try {
        data = fs.readFileSync(scriptPath);
      } catch (err: any) {
        throw new Error(`read script ${scriptPath}: ${err.message}`);
      }

      const root = project
        ? path.join(workDir, ".hollow", "workflows", "saved")
        : path.join(home_dir(), "workflows", "saved");

      const dir = path.join(root, normalizedName);
      try {
        fs.mkdirSync(dir, { recursive: true, mode: 0o755 });
      } catch (err: any) {
        throw new Error(`mkdir ${dir}: ${err.message}`);
      }

      const dst = path.join(dir, "workflow.js");
      try {
        fs.writeFileSync(dst, data, { mode: 0o644 });
      } catch (err: any) {
        throw new Error(`write ${dst}: ${err.message}`);
      }

      let meta: Meta = { name: normalizedName, description: "" };
      try {
        const metaRes = await Effect.runPromise(Inspect(scriptPath));
        meta = { ...metaRes, name: normalizedName };
      } catch {}

      try {
        const metaData = JSON.stringify(meta, null, "  ");
        fs.writeFileSync(path.join(dir, "meta.json"), metaData, { mode: 0o644 });
      } catch (err: any) {
        throw new Error(`write meta.json: ${err.message}`);
      }

      return { Name: normalizedName, Path: dst, Project: project, Meta: meta };
    },
    catch: (cause) => (cause instanceof Error ? cause : new Error(String(cause))),
  });
}

/*
PORT STATUS
source path: backend/workflow/saved.go
source lines: 88
draft lines: 113
confidence: high
status: phase_b_compile
*/
