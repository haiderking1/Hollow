// PORT: backend/skills/types.go

export interface SourceInfo {
  source: string;
  scope?: string;
  baseDir: string;
}

export interface SkillConditions {
  fallbackForToolsets: string[];
  requiresToolsets: string[];
  fallbackForTools: string[];
  requiresTools: string[];
}

export interface Skill {
  Name: string;
  Description: string;
  FilePath: string;
  BaseDir: string;
  SourceInfo: SourceInfo;
  DisableModelInvocation: boolean;
  Category: string;
  Platforms?: string[];
  Tags?: string[];
  RelatedSkills?: string[];
  Conditions: SkillConditions;
  DescriptionFull: string;
  Environments?: string[];
}

export interface SkillSnapshotEntry {
  skill_name: string;
  category: string;
  frontmatter_name: string;
  description: string;
  platforms: string[];
  conditions: SkillConditions;
  disable_model_invocation: boolean;
  environments: string[];
}

export interface SkillsPromptSnapshot {
  version: number;
  manifest: Record<string, [number, number]>; // absolute path -> [mtimeMs, size]
  skills: SkillSnapshotEntry[];
  category_descriptions: Record<string, string>;
}

export interface SkillGuardFinding {
  patternId: string;
  severity: string; // critical, high, medium, low
  category: string;
  file: string;
  line: number;
  match: string;
  description: string;
}

export interface SkillScanResult {
  skillName: string;
  source: string;
  contextDir: string;
  trustLevel: string; // builtin, trusted, community, agent-created
  verdict: string; // safe, caution, dangerous
  findings: SkillGuardFinding[];
  scannedAt: string;
  summary: string;
}

export interface SkillManageResult {
  success: boolean;
  message?: string;
  error?: string;
  path?: string;
  skill_md?: string;
  category?: string;
  hint?: string;
  file_preview?: string;
  available_files?: string[];
  staged?: boolean;
  pending_id?: string;
  gist?: string;
}

/*
PORT STATUS
source path: backend/skills/types.go
source lines: 85
draft lines: 88
confidence: high
status: phase_b_compile
*/
