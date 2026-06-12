package skills

type SourceInfo struct {
	Source  string `json:"source"`
	Scope   string `json:"scope,omitempty"`
	BaseDir string `json:"baseDir"`
}

type SkillConditions struct {
	FallbackForToolsets []string `json:"fallbackForToolsets"`
	RequiresToolsets    []string `json:"requiresToolsets"`
	FallbackForTools    []string `json:"fallbackForTools"`
	RequiresTools       []string `json:"requiresTools"`
}

type Skill struct {
	Name                   string          `json:"name"`
	Description            string          `json:"description"`
	FilePath               string          `json:"filePath"`
	BaseDir                string          `json:"baseDir"`
	SourceInfo             SourceInfo      `json:"sourceInfo"`
	DisableModelInvocation bool            `json:"disableModelInvocation"`
	Category               string          `json:"category"`
	Platforms              []string        `json:"platforms,omitempty"`
	Tags                   []string        `json:"tags,omitempty"`
	RelatedSkills          []string        `json:"relatedSkills,omitempty"`
	Conditions             SkillConditions `json:"conditions"`
	DescriptionFull        string          `json:"descriptionFull"`
	Environments           []string        `json:"environments,omitempty"`
}

type SkillSnapshotEntry struct {
	SkillName              string          `json:"skill_name"`
	Category               string          `json:"category"`
	FrontmatterName        string          `json:"frontmatter_name"`
	Description            string          `json:"description"`
	Platforms              []string        `json:"platforms"`
	Conditions             SkillConditions `json:"conditions"`
	DisableModelInvocation bool            `json:"disable_model_invocation"`
	Environments           []string        `json:"environments"`
}

type SkillsPromptSnapshot struct {
	Version              int                           `json:"version"`
	Manifest             map[string][2]int64           `json:"manifest"` // absolute path -> [mtimeNanoseconds, size]
	Skills               []SkillSnapshotEntry          `json:"skills"`
	CategoryDescriptions map[string]string             `json:"category_descriptions"`
}

type SkillGuardFinding struct {
	PatternID   string `json:"patternId"`
	Severity    string `json:"severity"` // critical, high, medium, low
	Category    string `json:"category"`
	File        string `json:"file"`
	Line        int    `json:"line"`
	Match       string `json:"match"`
	Description string `json:"description"`
}

type SkillScanResult struct {
	SkillName  string              `json:"skillName"`
	Source     string              `json:"source"`
	ContextDir string              `json:"contextDir"`
	TrustLevel string              `json:"trustLevel"` // builtin, trusted, community, agent-created
	Verdict    string              `json:"verdict"`    // safe, caution, dangerous
	Findings   []SkillGuardFinding `json:"findings"`
	ScannedAt  string              `json:"scannedAt"`
	Summary    string              `json:"summary"`
}

type SkillManageResult struct {
	Success        bool                `json:"success"`
	Message        string              `json:"message,omitempty"`
	Error          string              `json:"error,omitempty"`
	Path           string              `json:"path,omitempty"`
	SkillMd        string              `json:"skill_md,omitempty"`
	Category       string              `json:"category,omitempty"`
	Hint           string              `json:"hint,omitempty"`
	FilePreview    string              `json:"file_preview,omitempty"`
	AvailableFiles []string            `json:"available_files,omitempty"`
	Staged         bool                `json:"staged,omitempty"`
	PendingID      string              `json:"pending_id,omitempty"`
	Gist           string              `json:"gist,omitempty"`
}
