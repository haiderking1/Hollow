// PORT: backend/skills/path_security.go

import path from "node:path";

export function hasTraversalComponent(filePath: string): boolean {
  // Strings.FieldsFunc splits on / and \
  const parts = filePath.split(/[/\\]/);
  for (const part of parts) {
    if (part === "..") {
      return true;
    }
  }
  return false;
}

export function isPathWithinDir(filePath: string, baseDir: string): boolean {
  const resolvedBase = path.resolve(baseDir);
  const resolvedTarget = path.resolve(filePath);

  if (resolvedTarget === resolvedBase) {
    return true;
  }

  // Add separator suffix to base to prevent matching partial directory names
  const sep = path.sep;
  let prefix = resolvedBase;
  if (!prefix.endsWith(sep)) {
    prefix += sep;
  }
  return resolvedTarget.startsWith(prefix);
}

export function validateWithinDir(targetPath: string, baseDir: string): string {
  try {
    const resolvedBase = path.resolve(baseDir);
    // path.join resolves relative paths
    const resolvedTarget = path.join(resolvedBase, targetPath);
    if (!isPathWithinDir(resolvedTarget, resolvedBase)) {
      return "Path escapes the skill directory.";
    }
    return "";
  } catch {
    return "Failed to resolve skill directory.";
  }
}

/*
PORT STATUS
source path: backend/skills/path_security.go
source lines: 50
draft lines: 45
confidence: high
status: phase_b_compile
*/
