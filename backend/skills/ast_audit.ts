// PORT: backend/skills/ast_audit.go

import { Effect } from "effect";
import fs from "node:fs";
import path from "node:path";

export interface ASTFinding {
  File: string;
  Line: number;
  PatternID: string;
  Description: string;
}

const rxDynamicImport = /import_module\s*\(/;
const rxComputedImport = /__import__\s*\(\s*[^'"]/;
const rxDynamicGetattr = /getattr\s*\(\s*[^,]+,\s*[^'"]/;
const rxDictAccess = /__dict__\s*\[\s*[^'"]/;
const rxImportlibImport = /(?:^|\n)\s*(?:import\s+importlib|from\s+importlib\s+import)/;

function scanSource(content: string, relPath: string): ASTFinding[] {
  const findings: ASTFinding[] = [];
  const lines = content.split("\n");

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const lineNum = i + 1;
    const trimmed = line.trim();
    if (trimmed === "" || trimmed.startsWith("#")) {
      continue;
    }

    if (rxDynamicImport.test(line)) {
      findings.push({
        File: relPath,
        Line: lineNum,
        PatternID: "dynamic_import",
        Description: "importlib.import_module() — loads arbitrary modules at runtime",
      });
    }
    if (rxComputedImport.test(line)) {
      findings.push({
        File: relPath,
        Line: lineNum,
        PatternID: "dynamic_import_computed",
        Description: "__import__ with non-literal module name",
      });
    }
    if (rxDynamicGetattr.test(line)) {
      findings.push({
        File: relPath,
        Line: lineNum,
        PatternID: "dynamic_getattr",
        Description: "getattr with non-literal attribute name",
      });
    }
    if (rxDictAccess.test(line)) {
      findings.push({
        File: relPath,
        Line: lineNum,
        PatternID: "dict_access",
        Description: "__dict__[<computed>] — dynamic attribute access",
      });
    }
    if (rxImportlibImport.test(line)) {
      findings.push({
        File: relPath,
        Line: lineNum,
        PatternID: "importlib_import",
        Description: "import importlib — enables dynamic module loading",
      });
    }
  }

  return findings;
}

export function ASTScanPath(filePath: string): Effect.Effect<ASTFinding[], Error> {
  return Effect.try({
    try: () => {
      const fi = fs.statSync(filePath);
      if (!fi.isDirectory()) {
        if (path.extname(filePath).toLowerCase() !== ".py") {
          return [];
        }
        const data = fs.readFileSync(filePath, "utf8");
        return scanSource(data, path.basename(filePath));
      }

      const findings: ASTFinding[] = [];
      const ignoredDirs = new Set(["__pycache__", ".venv", "venv", "node_modules", ".git"]);

      const walk = (dir: string) => {
        let entries: string[];
        try {
          entries = fs.readdirSync(dir);
        } catch {
          return;
        }

        for (const entry of entries) {
          const full = path.join(dir, entry);
          let stat: fs.Stats;
          try {
            stat = fs.statSync(full);
          } catch {
            continue;
          }

          if (stat.isDirectory()) {
            if (ignoredDirs.has(entry)) {
              continue;
            }
            walk(full);
          } else {
            if (path.extname(full).toLowerCase() !== ".py") {
              continue;
            }
            let rel = path.relative(filePath, full);
            if (!rel) {
              rel = path.basename(full);
            }
            // Normalize separator to slash like Go's filepath.ToSlash
            const relSlash = rel.split(path.sep).join("/");
            try {
              const data = fs.readFileSync(full, "utf8");
              findings.push(...scanSource(data, relSlash));
            } catch {}
          }
        }
      };

      walk(filePath);
      return findings;
    },
    catch: (cause) => (cause instanceof Error ? cause : new Error(String(cause))),
  });
}

export function FormatASTReport(findings: ASTFinding[], skillName: string): string {
  let header = "AST deep scan";
  if (skillName !== "") {
    header = `AST deep scan: ${skillName}`;
  }

  if (findings.length === 0) {
    return `${header}\n  No dynamic import/access patterns detected.`;
  }

  const sorted = [...findings].sort((a, b) => {
    if (a.File !== b.File) {
      return a.File.localeCompare(b.File);
    }
    return a.Line - b.Line;
  });

  const lines: string[] = [];
  lines.push(header, `  ${sorted.length} finding(s):`);

  let currentFile = "";
  for (const f of sorted) {
    if (f.File !== currentFile) {
      currentFile = f.File;
      lines.push(`  ${f.File}`);
    }
    lines.push(`    L${f.Line}  ${f.PatternID}  — ${f.Description}`);
  }
  lines.push("", "  Note: diagnostic hints for human review, not security verdicts.");

  return lines.join("\n");
}

/*
PORT STATUS
source path: backend/skills/ast_audit.go
source lines: 168
draft lines: 167
confidence: high
status: phase_b_compile
*/
