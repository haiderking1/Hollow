// PORT: backend/workflow/state.go

import fs from "node:fs";
import path from "node:path";
import { type Meta, type Snapshot, type AgentSnapshot, type AgentResult, cloneJSON } from "./types";

export interface State {
  version: number;
  id: string;
  scriptPath: string;
  args?: string;
  meta: Meta;
  status: string;
  pauseReason?: string;
  phase?: string;
  stageIndex?: number;
  completed: Record<string, AgentResult>;
  agents?: Record<string, AgentSnapshot>;
  startedAt: Date;
  updatedAt: Date;
}

export function StatePath(scriptPath: string): string {
  return path.join(path.dirname(scriptPath), "state.json");
}

export function LoadState(scriptPath: string): State {
  const filePath = StatePath(scriptPath);
  const data = fs.readFileSync(filePath, "utf8");
  const state: State = JSON.parse(data);
  if (!state.completed) {
    state.completed = {};
  }
  if (!state.agents) {
    state.agents = {};
  }
  if (state.startedAt) state.startedAt = new Date(state.startedAt);
  if (state.updatedAt) state.updatedAt = new Date(state.updatedAt);
  return state;
}

export function SaveState(state: State): void {
  if (!state || !state.scriptPath) {
    throw new Error("workflow state has no script path");
  }
  state.version = 1;
  state.updatedAt = new Date();
  if (!state.completed) {
    state.completed = {};
  }
  if (!state.agents) {
    state.agents = {};
  }
  const data = JSON.stringify(state, null, "  ");
  const filePath = StatePath(state.scriptPath);
  const dirPath = path.dirname(filePath);
  fs.mkdirSync(dirPath, { recursive: true, mode: 0o755 });
  const tmpPath = filePath + ".tmp";
  fs.writeFileSync(tmpPath, data, { mode: 0o600 });
  fs.renameSync(tmpPath, filePath);
}

export function FindResumable(workDir: string, id: string): string {
  const root = path.join(workDir, ".hollow", "workflows");
  if (id !== "") {
    const base = path.basename(id);
    if (base !== id || id === "." || id === "..") {
      throw new Error("invalid workflow id");
    }
    const wfPath = path.join(root, id, "workflow.js");
    const state = LoadState(wfPath);
    if (state.status !== "paused") {
      throw new Error("workflow is not paused");
    }
    return wfPath;
  }
  let entries: fs.Dirent[];
  try {
    entries = fs.readdirSync(root, { withFileTypes: true });
  } catch {
    throw new Error("no paused workflow found");
  }
  let newest = "";
  let newestAt = new Date(0);
  for (const entry of entries) {
    if (!entry.isDirectory() || entry.name === "saved") {
      continue;
    }
    const wfPath = path.join(root, entry.name, "workflow.js");
    try {
      const state = LoadState(wfPath);
      if (state.status === "paused" && state.updatedAt.getTime() > newestAt.getTime()) {
        newest = wfPath;
        newestAt = state.updatedAt;
      }
    } catch {}
  }
  if (newest === "") {
    throw new Error("no paused workflow found");
  }
  return newest;
}

export function ListStates(workDir: string): State[] {
  const root = path.join(workDir, ".hollow", "workflows");
  let entries: fs.Dirent[];
  try {
    entries = fs.readdirSync(root, { withFileTypes: true });
  } catch {
    return [];
  }
  const states: State[] = [];
  for (const entry of entries) {
    if (!entry.isDirectory() || entry.name === "saved") {
      continue;
    }
    const wfPath = path.join(root, entry.name, "workflow.js");
    try {
      const state = LoadState(wfPath);
      states.push(state);
    } catch {}
  }
  states.sort((a, b) => b.updatedAt.getTime() - a.updatedAt.getTime());
  return states;
}

export function SnapshotFromState(state: State): Snapshot {
  const s: Snapshot = {
    id: state.id,
    name: state.meta.name,
    description: state.meta.description,
    scriptPath: state.scriptPath,
    status: state.status,
    phase: state.phase ?? "",
    agents: cloneJSON(state.agents ?? {}),
    startedAt: state.startedAt,
    message: state.pauseReason,
    phases: [],
    queued: 0,
    running: 0,
    done: 0,
    failed: 0,
    tokens: 0,
  };

  const known = new Set<string>();
  if (state.meta.phases) {
    for (const name of state.meta.phases) {
      if (name) {
        s.phases.push({ name, total: 0, queued: 0, running: 0, done: 0, failed: 0, tokens: 0 });
        known.add(name);
      }
    }
  }

  for (const item of Object.values(s.agents)) {
    if (!known.has(item.phase)) {
      s.phases.push({ name: item.phase, total: 0, queued: 0, running: 0, done: 0, failed: 0, tokens: 0 });
      known.add(item.phase);
    }
  }

  const phaseByName: Record<string, typeof s.phases[number]> = {};
  for (const p of s.phases) {
    phaseByName[p.name] = p;
  }

  for (const item of Object.values(s.agents)) {
    const phase = phaseByName[item.phase];
    if (!phase) continue;
    phase.total++;
    phase.tokens += item.tokens ?? 0;
    s.tokens += item.tokens ?? 0;
    switch (item.status) {
      case "queued":
        phase.queued++;
        s.queued++;
        break;
      case "running":
        phase.running++;
        s.running++;
        break;
      case "done":
        phase.done++;
        s.done++;
        break;
      case "failed":
      case "stopped":
        phase.failed++;
        s.failed++;
        break;
    }
  }

  return s;
}

interface ApprovalFile {
  names: string[];
}

function projectApprovalPath(workDir: string): string {
  return path.join(workDir, ".hollow", "workflows", "approvals.json");
}

export function IsAlwaysApproved(workDir: string, name: string): boolean {
  try {
    const data = fs.readFileSync(projectApprovalPath(workDir), "utf8");
    const approvals: ApprovalFile = JSON.parse(data);
    if (!approvals || !Array.isArray(approvals.names)) {
      return false;
    }
    return approvals.names.includes(name);
  } catch {
    return false;
  }
}

export function SetAlwaysApproved(workDir: string, name: string): void {
  const filePath = projectApprovalPath(workDir);
  let approvals: ApprovalFile = { names: [] };
  try {
    const data = fs.readFileSync(filePath, "utf8");
    approvals = JSON.parse(data);
    if (!approvals || !Array.isArray(approvals.names)) {
      approvals = { names: [] };
    }
  } catch {}

  if (approvals.names.includes(name)) {
    return;
  }
  approvals.names.push(name);
  approvals.names.sort();

  const data = JSON.stringify(approvals, null, "  ");
  fs.mkdirSync(path.dirname(filePath), { recursive: true, mode: 0o755 });
  fs.writeFileSync(filePath, data, { mode: 0o600 });
}

/*
PORT STATUS
source path: backend/workflow/state.go
source lines: 231
draft lines: 219
confidence: high
status: phase_b_compile
*/
