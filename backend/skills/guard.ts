// PORT: backend/skills/guard.go

import fs from "node:fs";
import path from "node:path";
import { type SkillGuardFinding, type SkillScanResult } from "./types";
import { SkillGuardThreatPatterns } from "./guard_patterns";
import { SkillsDir } from "./paths";

export const InvisibleChars = [
  "\u200b", "\u200c", "\u200d", "\u2060", "\u2062", "\u2063", "\u2064", "\ufeff",
  "\u202a", "\u202b", "\u202c", "\u202d", "\u202e", "\u2066", "\u2067", "\u2068", "\u2069",
];

export const MaxFileCount = 50;
export const MaxTotalSizeKB = 1024;
export const MaxSingleFileKB = 256;

function unicodeCharName(char: string): string {
  const names: Record<string, string> = {
    "\u200b": "zero-width space",
    "\u200c": "zero-width non-joiner",
    "\u200d": "zero-width joiner",
    "\u2060": "word joiner",
    "\u2062": "invisible times",
    "\u2063": "invisible separator",
    "\u2064": "invisible plus",
    "\ufeff": "BOM/zero-width no-break space",
    "\u202a": "LTR embedding",
    "\u202b": "RTL embedding",
    "\u202c": "pop directional",
    "\u202d": "LTR override",
    "\u202e": "RTL override",
    "\u2066": "LTR isolate",
    "\u2067": "RTL isolate",
    "\u2068": "first strong isolate",
    "\u2069": "pop directional isolate",
  };
  if (names[char]) {
    return names[char];
  }
  if (char.length > 0) {
    const codePoint = char.codePointAt(0) ?? 0;
    return `U+${codePoint.toString(16).toUpperCase().padStart(4, "0")}`;
  }
  return "????";
}

function resolveTrustLevel(source: string): string {
  let normalized = source;
  if (normalized.startsWith("skills-sh/")) normalized = normalized.substring("skills-sh/".length);
  else if (normalized.startsWith("skills.sh/")) normalized = normalized.substring("skills.sh/".length);
  else if (normalized.startsWith("skils-sh/")) normalized = normalized.substring("skils-sh/".length);
  else if (normalized.startsWith("skils.sh/")) normalized = normalized.substring("skils.sh/".length);
  normalized = normalized.toLowerCase();

  if (normalized === "agent-created") {
    return "agent-created";
  }
  if (normalized === "official") {
    return "builtin";
  }
  const trusted = ["openai/skills", "anthropics/skills", "huggingface/skills"];
  for (const repo of trusted) {
    if (normalized === repo || normalized.startsWith(repo + "/")) {
      return "trusted";
    }
  }
  return "community";
}

function determineVerdict(findings: SkillGuardFinding[]): string {
  if (findings.length === 0) {
    return "safe";
  }
  for (const f of findings) {
    if (f.severity === "critical") {
      return "dangerous";
    }
  }
  for (const f of findings) {
    if (f.severity === "high") {
      return "caution";
    }
  }
  return "safe";
}

const scannableExtensions = new Set([
  ".md", ".txt", ".py", ".sh", ".bash",
  ".js", ".ts", ".rb", ".yaml", ".yml",
  ".json", ".toml", ".cfg", ".ini", ".conf",
  ".html", ".css", ".xml", ".tex", ".r",
  ".jl", ".pl", ".php",
]);

const suspiciousBinaryExtensions = new Set([
  ".exe", ".dll", ".so", ".dylib", ".bin",
  ".dat", ".com", ".msi", ".dmg", ".app",
  ".deb", ".rpm",
]);

export function ScanSkillFile(filePath: string, relPath: string): SkillGuardFinding[] {
  const baseName = path.basename(filePath);
  const ext = path.extname(baseName);
  if (!scannableExtensions.has(ext) && baseName !== "SKILL.md") {
    return [];
  }

  let data: string;
  try {
    data = fs.readFileSync(filePath, "utf8");
  } catch {
    return [];
  }

  const lines = data.split("\n");
  const findings: SkillGuardFinding[] = [];
  const seen = new Set<string>();

  for (const p of SkillGuardThreatPatterns) {
    for (let i = 0; i < lines.length; i++) {
      const line = lines[i];
      const lineLower = line.toLowerCase();
      const key = `${p.PatternID}:${i + 1}`;
      if (seen.has(key)) {
        continue;
      }

      let matched = false;
      if (p.PatternID === "python_os_environ") {
        if (p.Regex.test(line)) {
          if (!lineLower.includes("path")) {
            matched = true;
          }
        }
      } else if (p.PatternID === "unpinned_pip_install") {
        if (lineLower.includes("pip install") || lineLower.includes("pip3 install")) {
          if (!lineLower.includes("==") && !lineLower.includes("-r")) {
            matched = true;
          }
        }
      } else if (p.PatternID === "unpinned_npm_install") {
        if (lineLower.includes("npm install") || lineLower.includes("npm i ")) {
          if (!lineLower.includes("@")) {
            matched = true;
          }
        }
      } else {
        matched = p.Regex.test(line);
      }

      if (matched) {
        seen.add(key);
        let matchedText = line.trim();
        if (matchedText.length > 120) {
          matchedText = matchedText.substring(0, 117) + "...";
        }
        findings.push({
          patternId: p.PatternID,
          severity: p.Severity,
          category: p.Category,
          file: relPath,
          line: i + 1,
          match: matchedText,
          description: p.Description,
        });
      }
    }
  }

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    for (const char of InvisibleChars) {
      if (line.includes(char)) {
        const codePoint = char.codePointAt(0) ?? 0;
        findings.push({
          patternId: "invisible_unicode",
          severity: "high",
          category: "injection",
          file: relPath,
          line: i + 1,
          match: `U+${codePoint.toString(16).toUpperCase().padStart(4, "0")} (${unicodeCharName(char)})`,
          description: `invisible unicode character ${unicodeCharName(char)} (possible text hiding/injection)`,
        });
        break;
      }
    }
  }

  return findings;
}

function checkStructure(skillDir: string): SkillGuardFinding[] {
  const findings: SkillGuardFinding[] = [];
  let fileCount = 0;
  let totalSize = 0;

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
        walk(full);
      } else {
        fileCount++;
        totalSize += stat.size;
        let rel: string;
        try {
          rel = path.relative(skillDir, full);
        } catch {
          rel = path.basename(full);
        }
        const relSlash = rel.split(path.sep).join("/");
        if (stat.size > MaxSingleFileKB * 1024) {
          findings.push({
            patternId: "oversized_file",
            severity: "medium",
            category: "structural",
            file: relSlash,
            line: 0,
            match: `${Math.floor(stat.size / 1024)}KB`,
            description: `file is ${Math.floor(stat.size / 1024)}KB (limit: ${MaxSingleFileKB}KB)`,
          });
        }
        const ext = path.extname(entry).toLowerCase();
        if (suspiciousBinaryExtensions.has(ext)) {
          findings.push({
            patternId: "binary_file",
            severity: "critical",
            category: "structural",
            file: relSlash,
            line: 0,
            match: `binary: ${ext}`,
            description: `binary/executable file (${ext}) should not be in a skill`,
          });
        }
      }
    }
  };

  walk(skillDir);

  if (fileCount > MaxFileCount) {
    findings.push({
      patternId: "too_many_files",
      severity: "medium",
      category: "structural",
      file: "(directory)",
      line: 0,
      match: `${fileCount} files`,
      description: `skill has ${fileCount} files (limit: ${MaxFileCount})`,
    });
  }

  if (totalSize > MaxTotalSizeKB * 1024) {
    findings.push({
      patternId: "oversized_skill",
      severity: "high",
      category: "structural",
      file: "(directory)",
      line: 0,
      match: `${Math.floor(totalSize / 1024)}KB total`,
      description: `skill is ${Math.floor(totalSize / 1024)}KB total (limit: ${MaxTotalSizeKB}KB)`,
    });
  }

  return findings;
}

export function ScanSkill(skillPath: string, source: string): SkillScanResult {
  const skillName = path.basename(skillPath);
  const trustLevel = resolveTrustLevel(source);
  const allFindings = checkStructure(skillPath);

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
        walk(full);
      } else {
        let rel: string;
        try {
          rel = path.relative(skillPath, full);
        } catch {
          rel = path.basename(full);
        }
        allFindings.push(...ScanSkillFile(full, rel.split(path.sep).join("/")));
      }
    }
  };

  walk(skillPath);

  const verdict = determineVerdict(allFindings);

  const catMap = new Set<string>();
  for (const f of allFindings) {
    catMap.add(f.category);
  }
  const categories = Array.from(catMap).sort();

  let summary = "";
  if (allFindings.length === 0) {
    summary = `${skillName}: clean scan, no threats detected`;
  } else {
    summary = `${skillName}: ${verdict} — ${allFindings.length} finding(s) in ${categories.join(", ")}`;
  }

  return {
    skillName,
    source,
    contextDir: "",
    trustLevel,
    verdict,
    findings: allFindings,
    scannedAt: new Date().toISOString(),
    summary,
  };
}

export function shouldAllowInstall(result: SkillScanResult, force: boolean): [boolean, string] {
  const policy: Record<string, string[]> = {
    "builtin": ["allow", "allow", "allow"],
    "trusted": ["allow", "allow", "block"],
    "community": ["allow", "block", "block"],
    "agent-created": ["allow", "allow", "ask"],
  };

  let pList = policy[result.trustLevel];
  if (!pList) {
    pList = policy["community"];
  }

  let vi = 0;
  switch (result.verdict) {
    case "safe":
      vi = 0;
      break;
    case "caution":
      vi = 1;
      break;
    default:
      vi = 2;
  }

  const decision = pList[vi];

  if (decision === "allow") {
    return [true, `Allowed (${result.trustLevel} source, %s verdict)`.replace("%s", result.verdict)];
  }

  if (force && !(result.verdict === "dangerous" && (result.trustLevel === "community" || result.trustLevel === "trusted"))) {
    return [true, `Force-installed despite ${result.verdict} verdict (${result.findings.length} findings)`];
  }

  if (decision === "ask") {
    return [false, `Requires confirmation (${result.trustLevel} source + ${result.verdict} verdict, ${result.findings.length} findings)`];
  }

  if (result.verdict === "dangerous" && (result.trustLevel === "community" || result.trustLevel === "trusted")) {
    return [false, `Blocked (${result.trustLevel} source + dangerous verdict, ${result.findings.length} findings). --force does not override a dangerous verdict.`];
  }

  return [false, `Blocked (${result.trustLevel} source + ${result.verdict} verdict, ${result.findings.length} findings). Use --force to override.`];
}

export function FormatScanReport(result: SkillScanResult): string {
  let sb = `Scan: ${result.skillName} (${result.source}/${result.trustLevel})  Verdict: ${result.verdict.toUpperCase()}\n`;

  if (result.findings.length > 0) {
    const sevOrder: Record<string, number> = { "critical": 0, "high": 1, "medium": 2, "low": 3 };
    const findings = [...result.findings].sort((a, b) => {
      const aOrd = sevOrder[a.severity] ?? 99;
      const bOrd = sevOrder[b.severity] ?? 99;
      return aOrd - bOrd;
    });

    for (const f of findings) {
      let sev = f.severity.toUpperCase();
      if (sev.length < 8) {
        sev += " ".repeat(8 - sev.length);
      }
      let cat = f.category;
      if (cat.length < 14) {
        cat += " ".repeat(14 - cat.length);
      }
      let loc = `${f.file}:${f.line}`;
      if (loc.length < 30) {
        loc += " ".repeat(30 - loc.length);
      }
      let matchText = f.match;
      if (matchText.length > 60) {
        matchText = matchText.substring(0, 57) + "...";
      }
      sb += `  ${sev} ${cat} ${loc} "${matchText}"\n`;
    }
    sb += "\n";
  }

  const [allowed, reason] = shouldAllowInstall(result, false);
  const status = allowed ? "ALLOWED" : "BLOCKED";
  sb += `Decision: ${status} — ${reason}`;
  return sb;
}

export function SecurityScanSkillDir(skillDir: string, guardEnabled: boolean): string {
  if (!guardEnabled) {
    return "";
  }
  const result = ScanSkill(skillDir, "agent-created");
  const [allowed, reason] = shouldAllowInstall(result, false);
  if (!allowed) {
    const report = FormatScanReport(result);
    return `Security scan blocked this skill (${reason}):\n${report}`;
  }
  return "";
}

/*
PORT STATUS
source path: backend/skills/guard.go
source lines: 415
draft lines: 416
confidence: high
status: phase_b_compile
*/
