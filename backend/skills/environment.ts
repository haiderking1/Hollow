// PORT: backend/skills/environment.go

import fs from "node:fs";

const knownEnvironments: Record<string, boolean> = {
  kanban: true,
  docker: true,
  s6: true,
};

const envDetectCache = new Map<string, boolean>();

function detectEnvironment(env: string): boolean {
  if (envDetectCache.has(env)) {
    return envDetectCache.get(env)!;
  }

  let result = true;
  switch (env) {
    case "kanban":
      if (
        (process.env.HOLLOW_KANBAN_TASK !== undefined && process.env.HOLLOW_KANBAN_TASK !== "") ||
        (process.env.HOLLOW_KANBAN_BOARD !== undefined && process.env.HOLLOW_KANBAN_BOARD !== "") ||
        (process.env.HERMES_KANBAN_TASK !== undefined && process.env.HERMES_KANBAN_TASK !== "") ||
        (process.env.HERMES_KANBAN_BOARD !== undefined && process.env.HERMES_KANBAN_BOARD !== "")
      ) {
        result = true;
      } else {
        result = false;
      }
      break;
    case "docker":
      result = isContainer();
      break;
    case "s6":
      try {
        const fi = fs.statSync("/run/s6");
        if (fi.isDirectory()) {
          result = true;
        } else {
          result = false;
        }
      } catch {
        try {
          const fi = fs.statSync("/package/admin/s6-overlay");
          if (fi.isDirectory()) {
            result = true;
          } else {
            result = false;
          }
        } catch {
          result = false;
        }
      }
      break;
  }

  envDetectCache.set(env, result);
  return result;
}

function isContainer(): boolean {
  try {
    fs.statSync("/.dockerenv");
    return true;
  } catch {}
  try {
    fs.statSync("/run/.containerenv");
    return true;
  } catch {}
  try {
    const data = fs.readFileSync("/proc/1/cgroup", "utf8");
    if (data.includes("docker") || data.includes("podman") || data.includes("/lxc/")) {
      return true;
    }
  } catch {}
  return false;
}

export function SkillMatchesEnvironment(fm: Record<string, any>): boolean {
  const envVal = fm["environments"];
  if (envVal === undefined || envVal === null) {
    return true;
  }

  let list: string[] = [];
  if (typeof envVal === "string") {
    if (envVal !== "") {
      list = [envVal];
    }
  } else if (Array.isArray(envVal)) {
    for (const item of envVal) {
      if (typeof item === "string") {
        list.push(item);
      }
    }
  }

  if (list.length === 0) {
    return true;
  }

  for (const env of list) {
    const normalized = env.trim().toLowerCase();
    if (normalized === "") {
      continue;
    }
    if (!knownEnvironments[normalized]) {
      return true;
    }
    if (detectEnvironment(normalized)) {
      return true;
    }
  }

  return false;
}

/*
PORT STATUS
source path: backend/skills/environment.go
source lines: 104
draft lines: 105
confidence: high
status: phase_b_compile
*/
