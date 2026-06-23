// PORT: backend/shell/cwd.go

import os from "node:os";
import path from "node:path";
import fs from "node:fs";

const msys_drive_regex1 = /^\/([a-zA-Z])(\/.*)?$/;
const msys_drive_regex2 = /^\/(?:cygdrive|mnt)\/([a-zA-Z])(\/.*)?$/;

// MsysToWindowsPath translates a Git Bash / MSYS-style POSIX path (/c/Users/x)
// to the native Windows form (C:\Users\x).
export const msys_to_windows_path = (cwd: string): string =>
  msys_to_windows_path_internal(cwd, process.platform === "win32");

export const msys_to_windows_path_internal = (cwd: string, is_windows: boolean): string => {
  if (!is_windows || cwd === "") return cwd;
  let m = cwd.match(msys_drive_regex1);
  if (m !== null) {
    const drive = m[1].toUpperCase();
    let tail = m[2] ?? "";
    tail = tail !== "" ? tail.replaceAll("/", "\\") : "\\";
    return `${drive}:${tail}`;
  }
  m = cwd.match(msys_drive_regex2);
  if (m !== null) {
    const drive = m[1].toUpperCase();
    let tail = m[2] ?? "";
    tail = tail !== "" ? tail.replaceAll("/", "\\") : "\\";
    return `${drive}:${tail}`;
  }
  return cwd;
};

// ResolveSafeCwd returns the cwd if it exists as a directory, else walks up to
// the nearest existing ancestor. Normalizes MSYS paths on Windows.
export const resolve_safe_cwd = (cwd: string): string => {
  if (process.platform === "win32") cwd = msys_to_windows_path(cwd);
  if (cwd !== "") {
    try {
      if (fs.statSync(cwd).isDirectory()) return cwd;
    } catch {}
  }
  let parent = path.dirname(cwd);
  while (parent !== "" && parent !== cwd) {
    try {
      if (fs.statSync(parent).isDirectory()) return parent;
    } catch {}
    const next_parent = path.dirname(parent);
    if (next_parent === parent) break;
    parent = next_parent;
  }
  return os.tmpdir();
};

/*
PORT STATUS
source path: backend/shell/cwd.go
source lines: 72
draft lines: 66
confidence: high
status: phase_a_draft
todos:
  - verify path.dirname behavior on Windows edge cases matches filepath.Dir
notes:
  - No (T, error) returns; plain function port.
*/
