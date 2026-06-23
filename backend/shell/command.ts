// PORT: backend/shell/command.go

import path from "node:path";
import { spawn, type ChildProcess } from "node:child_process";
import { Effect } from "effect";
import { resolve_bash, type shell_error } from "./resolve";

export type command_options = {
  command: string;
  login: boolean;
  env: NodeJS.ProcessEnv;
  windows_hide: boolean;
};

// CommandContext creates a child process to run Git Bash on Windows or system bash on Unix.
// It prepends the Git paths to the child env PATH on Windows, and hides the console window.
export const command_context = (
  _ctx: AbortSignal,
  command: string,
  login: boolean,
  cwd?: string,
): Effect.Effect<ChildProcess, shell_error> =>
  Effect.gen(function* () {
    const bash_exe = yield* resolve_bash();
    const args = login ? ["-l", "-c", command] : ["-c", command];

    const env: NodeJS.ProcessEnv = {
      ...process.env,
      TERM: "dumb",
      NO_COLOR: "1",
      CLICOLOR: "0",
      FORCE_COLOR: "0",
    };

    if (process.platform === "win32") {
      const bash_abs = path.resolve(bash_exe);
      const git_root = git_root_from_bash_exe(bash_abs);
      const new_entries = [
        path.join(git_root, "cmd"),
        path.join(git_root, "bin"),
        path.join(git_root, "usr", "bin"),
      ];
      const path_key = Object.keys(env).find((k) => k.toLowerCase() === "path") ?? "PATH";
      const path_val = env[path_key] ?? "";
      env[path_key] = path_val !== "" ? `${new_entries.join(";")};${path_val}` : new_entries.join(";");
    }

    return spawn(bash_exe, args, {
      env,
      cwd,
      // detached: child becomes its own process-group leader so the whole tree
      // can be killed via process.kill(-pid) on abort/timeout. Without this the
      // shell's children orphan and run forever (the "stuck bash" bug).
      detached: process.platform !== "win32",
      // stdin ignored: commands like `read`/`cat` hit EOF instead of hanging.
      stdio: ["ignore", "pipe", "pipe"],
      windowsHide: process.platform === "win32",
    });
  });

// gitRootFromBashExe returns the Git for Windows install root containing cmd/, bin/, usr/.
export const git_root_from_bash_exe = (bash_abs: string): string => {
  const parent = path.dirname(bash_abs); // .../bin or .../usr/bin
  const grandparent = path.dirname(parent); // .../git or .../usr
  const great = path.dirname(grandparent); // .../git or drive root

  const base_parent = path.basename(parent).toLowerCase();
  const base_grandparent = path.basename(grandparent).toLowerCase();

  if (base_parent === "bin" && base_grandparent === "usr") {
    return great;
  }
  if (base_parent === "bin") {
    return grandparent;
  }
  return great;
};

/*
PORT STATUS
source path: backend/shell/command.go
source lines: 93
draft lines: 82
confidence: high
status: phase_a_draft
todos:
  - wire AbortSignal cancellation to spawned child lifecycle if needed
notes:
  - CommandContext returns (*exec.Cmd, error), modeled as Effect.Effect<ChildProcess, shell_error>.
*/
