// PORT: backend/shell/resolve.go

import fs from "node:fs";
import path from "node:path";
import { Effect } from "effect";
import { load } from "../config/config";
import { portable_git_dir } from "../hollowhome/portable_git";

export type shell_error = {
  readonly _tag: "ShellError";
  readonly reason: string;
  readonly cause: unknown;
};

export const shell_error = (reason: string, cause: unknown): shell_error => ({
  _tag: "ShellError",
  reason,
  cause,
});

const is_file = (p: string): boolean => {
  try { return fs.statSync(p).isFile(); } catch { return false; }
};

const look_path = (name: string): string | null => {
  const paths = (process.env.PATH ?? "").split(path.delimiter);
  for (const dir of paths) {
    const candidate = path.join(dir, name);
    if (is_file(candidate)) return candidate;
    if (process.platform === "win32") {
      const exe = candidate.endsWith(".exe") ? candidate : `${candidate}.exe`;
      if (is_file(exe)) return exe;
    }
  }
  return null;
};

// ResolveBash returns the path to the resolved bash executable.
// It follows the resolution order specified in B1 of the handoff spec.
export const resolve_bash = (): Effect.Effect<string, shell_error> =>
  Effect.gen(function* () {
    if (process.platform !== "win32") {
      const found = look_path("bash");
      if (found !== null) return found;
      for (const candidate of ["/usr/bin/bash", "/bin/bash"]) {
        if (is_file(candidate)) return candidate;
      }
      const shell_env = process.env.SHELL ?? "";
      if (shell_env !== "" && is_file(shell_env)) return shell_env;
      return "/bin/sh";
    }

    const local_app_data = process.env.LOCALAPPDATA ?? "";

    const cfg_result = yield* Effect.either(load());
    if (cfg_result._tag === "Right") {
      const shell_path = cfg_result.right.shell_path ?? "";
      if (shell_path !== "" && is_file(shell_path)) return shell_path;
    }

    const custom = process.env.HOLLOW_GIT_BASH_PATH ?? "";
    if (custom !== "" && is_file(custom)) return custom;

    const p_git_dir = portable_git_dir();
    for (const c of [
      path.join(p_git_dir, "bin", "bash.exe"),
      path.join(p_git_dir, "usr", "bin", "bash.exe"),
    ]) {
      if (is_file(c)) return c;
    }

    const bash_path = look_path("bash");
    if (bash_path !== null && is_file(bash_path)) return bash_path;

    const system_candidates: string[] = [];
    const pf = process.env.ProgramFiles ?? "";
    system_candidates.push(pf !== "" ? path.join(pf, "Git", "bin", "bash.exe") : "C:\\Program Files\\Git\\bin\\bash.exe");
    const pf86 = process.env["ProgramFiles(x86)"] ?? "";
    system_candidates.push(pf86 !== "" ? path.join(pf86, "Git", "bin", "bash.exe") : "C:\\Program Files (x86)\\Git\\bin\\bash.exe");
    if (local_app_data !== "") system_candidates.push(path.join(local_app_data, "Programs", "Git", "bin", "bash.exe"));

    for (const c of system_candidates) {
      if (is_file(c)) return c;
    }

    const git_path = look_path("git");
    if (git_path !== null) {
      const abs_git_path = path.resolve(git_path);
      const parent = path.dirname(abs_git_path);
      const git_root = path.dirname(parent);
      for (const c of [path.join(git_root, "bin", "bash.exe"), path.join(git_root, "usr", "bin", "bash.exe")]) {
        if (is_file(c)) return c;
      }
    }

    return yield* Effect.fail(shell_error(
      "Git Bash not found. Hollow requires Git for Windows on Windows.\nInstall it using the following command in PowerShell:\n  irm https://raw.githubusercontent.com/haiderking1/Hollow/main/scripts/install-windows.ps1 | iex\nOr set the HOLLOW_GIT_BASH_PATH environment variable.",
      null,
    ));
  });

/*
PORT STATUS
source path: backend/shell/resolve.go
source lines: 118
draft lines: 114
confidence: high
status: phase_a_draft
todos:
  - verify Windows PATH executable lookup semantics match exec.LookPath exactly
notes:
  - ResolveBash returns (string, error), modeled as Effect.Effect<string, shell_error>.
  - Reuses config.load and hollowhome portable_git_dir ports.
*/
