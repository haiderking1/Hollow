// PORT: backend/skills/constants.go

export const MaxSkillNameLength = 64;
export const MaxSkillDescriptionLength = 1024;
export const PromptIndexDescriptionMax = 60;
export const SkillsPromptCacheMax = 8;
export const SkillsSnapshotVersion = 1;
export const InlineShellMaxOutput = 4000;
export const MaxSkillContentChars = 100000;
export const MaxSkillFileBytes = 1048576;

export const ExcludedSkillDirs: Record<string, boolean> = {
  ".git": true,
  ".github": true,
  ".hub": true,
  ".archive": true,
  ".venv": true,
  "venv": true,
  "node_modules": true,
  "site-packages": true,
  "__pycache__": true,
  ".tox": true,
  ".nox": true,
  ".pytest_cache": true,
  ".mypy_cache": true,
  ".ruff_cache": true,
};

export const PlatformMap: Record<string, string> = {
  macos: "darwin",
  linux: "linux",
  windows: "windows",
};

export const InjectionPatterns = [
  "ignore previous instructions",
  "ignore all previous",
  "you are now",
  "disregard your",
  "forget your instructions",
  "new instructions:",
  "system prompt:",
  "<system>",
  "]]>",
];

export const AllowedSkillSubdirs: Record<string, boolean> = {
  references: true,
  templates: true,
  scripts: true,
  assets: true,
};

export const SkillManageNameRe = /^[a-z0-9][a-z0-9._-]*$/;
export const SkillNameValidRe = /^[a-z0-9-]+$/;

/*
PORT STATUS
source path: backend/skills/constants.go
source lines: 60
draft lines: 55
confidence: high
status: phase_b_compile
*/
