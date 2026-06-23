// PORT: backend/agent/obligations/derive.go

import fs from "node:fs";
import path from "node:path";

// DetectVerifyCommand inspects the workspace root and returns the project's
// verification command, or "" when none can be derived (manual mode).
export const detect_verify_command = (work_dir: string): string => {
  const exists = (name: string): boolean => fs.existsSync(path.join(work_dir, name));

  if (exists("go.mod")) {
    return "go test ./...";
  }

  try {
    const data = fs.readFileSync(path.join(work_dir, "package.json"), "utf8");
    const pkg = JSON.parse(data) as { scripts?: Record<string, string> };
    if (pkg.scripts?.test !== undefined && pkg.scripts.test !== "") {
      return "npm test";
    }
  } catch {
    // ignore missing/unreadable package.json
  }

  if (exists("Cargo.toml")) {
    return "cargo test";
  }

  if (exists("pyproject.toml") || exists("pytest.ini")) {
    return "pytest";
  }

  return "";
};

/*
PORT STATUS
source path: backend/agent/obligations/derive.go
source lines: 35
draft lines: 47
confidence: high
status: phase_b_compile
todos:
  - verify JSON.parse type cast matches Go's json.Unmarshal behavior
notes:
  - No (T, error) returns; plain function port.
*/
