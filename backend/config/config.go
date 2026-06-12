package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/enough/enough/backend/auth"
	"github.com/enough/enough/backend/enoughhome"
	"github.com/enough/enough/backend/secrets"
)

const (
	DefaultEndpoint   = "https://opencode.ai/zen/go/v1"
	DefaultModel      = "deepseek-v4-flash"
	DefaultCodexModel = "gpt-5-codex"

	ProviderOpenCode = "opencode-go"
	ProviderCodex    = "openai-codex"
)

type CompactionSettings struct {
	Enabled          bool `json:"enabled"`
	ReserveTokens    int  `json:"reserve_tokens"`
	KeepRecentTokens int  `json:"keep_recent_tokens"`
	ContextWindow    int  `json:"context_window,omitempty"`
}

// EvidenceConfig controls the v2 evidence runtime. Enabled=false restores
// v1 behavior (no ledger, no tool guard) for emergency rollback.
type EvidenceConfig struct {
	Enabled             bool `json:"enabled"`
	StrictVerifyReset   bool `json:"strict_verify_reset"`
	MaxCompletionRounds int  `json:"max_completion_rounds"`
	VerifierEnabled     bool `json:"verifier_enabled"`

	// ContinuityReads seeds read credit at turn start for agent-authored
	// files whose on-disk hash still matches the last recorded mutation.
	// Pointer so configs written before this field existed default to true.
	ContinuityReads *bool `json:"continuity_reads,omitempty"`

	// GoalLock when true (default) freezes the user task for the turn.
	GoalLock *bool `json:"goal_lock_enabled,omitempty"`

	// StepScorer when true (default) rejects tool paths that repeat failures
	// or edit away from the failure site after a verify run fails.
	StepScorer *bool `json:"step_scorer_enabled,omitempty"`

	// ParallelForks when true (default) spawns parallel same-model workers in
	// git worktrees after repeated verify failures (same locked goal).
	ParallelForks *bool `json:"parallel_forks_enabled,omitempty"`
	StuckAfterFailures int `json:"stuck_after_failures,omitempty"`
	ParallelForkCount  int `json:"parallel_fork_count,omitempty"`
}

// ContinuityEnabled resolves the default-true tri-state.
func (e EvidenceConfig) ContinuityEnabled() bool {
	return e.ContinuityReads == nil || *e.ContinuityReads
}

func DefaultEvidence() EvidenceConfig {
	trueVal := true
	return EvidenceConfig{
		Enabled:              true,
		StrictVerifyReset:    true,
		MaxCompletionRounds:  12,
		VerifierEnabled:      true,
		GoalLock:             &trueVal,
		StepScorer:           &trueVal,
		ParallelForks:        &trueVal,
		StuckAfterFailures:   2,
		ParallelForkCount:    4,
	}
}

func (e EvidenceConfig) GoalLockEnabled() bool {
	return e.GoalLock == nil || *e.GoalLock
}

func (e EvidenceConfig) StepScorerEnabled() bool {
	return e.StepScorer == nil || *e.StepScorer
}

func (e EvidenceConfig) ParallelForksEnabled() bool {
	return e.ParallelForks == nil || *e.ParallelForks
}

func (e EvidenceConfig) StuckThreshold() int {
	if e.StuckAfterFailures <= 0 {
		return 2
	}
	return e.StuckAfterFailures
}

func (e EvidenceConfig) ForkCount() int {
	if e.ParallelForkCount <= 0 {
		return 4
	}
	if e.ParallelForkCount > 8 {
		return 8
	}
	return e.ParallelForkCount
}

type SkillsSettings struct {
	Enabled             bool                `json:"enabled"`
	EnableSkillCommands bool                `json:"enable_skill_commands"`
	Paths               []string            `json:"paths"`
	Disabled            []string            `json:"disabled"`
	ExternalDirs        []string            `json:"external_dirs"`
	PlatformDisabled    map[string][]string `json:"platform_disabled"`
	GuardAgentCreated   bool                `json:"guard_agent_created"`
	WriteApproval       bool                `json:"write_approval"`
	InlineShell         bool                `json:"inline_shell"`
	InlineShellTimeout  int                 `json:"inline_shell_timeout"`
}

type AgentSettings struct {
	CodingContext string `json:"coding_context"`
}

type PluginsSettings struct {
	Disabled []string `json:"disabled"`
}

// MemorySettings controls the built-in persistent memory (MEMORY.md/USER.md)
// and the background self-improvement review nudges. Unlike Hermes, Enough
// defaults memory ON; the semantics otherwise match Hermes' built-in memory.
type MemorySettings struct {
	Enabled            bool `json:"memory_enabled"`
	UserProfileEnabled bool `json:"user_profile_enabled"`
	// NudgeInterval is the number of user turns between background memory
	// reviews. 0 disables the memory review trigger.
	NudgeInterval int `json:"nudge_interval"`
	// SkillNudgeInterval is the number of tool iterations within turns
	// between background skill reviews. 0 disables the skill review trigger.
	SkillNudgeInterval int `json:"skill_nudge_interval"`
	MemoryCharLimit    int `json:"memory_char_limit"`
	UserCharLimit      int `json:"user_char_limit"`
	WriteApproval      bool `json:"write_approval"`
}

func DefaultMemory() MemorySettings {
	return MemorySettings{
		Enabled:            true,
		UserProfileEnabled: true,
		NudgeInterval:      10,
		SkillNudgeInterval: 10,
		MemoryCharLimit:    2200,
		UserCharLimit:      1375,
		WriteApproval:      false,
	}
}

// CuratorSettings controls the background skill curator (inactivity-triggered,
// no cron). Deterministic stale/archive transitions plus an LLM review pass
// over agent-created skills.
type CuratorSettings struct {
	Enabled          bool    `json:"enabled"`
	IntervalHours    int     `json:"interval_hours"`
	MinIdleHours     float64 `json:"min_idle_hours"`
	StaleAfterDays   int     `json:"stale_after_days"`
	ArchiveAfterDays int     `json:"archive_after_days"`
	PruneBuiltins    bool    `json:"prune_builtins"`
}

func DefaultCurator() CuratorSettings {
	return CuratorSettings{
		Enabled:          true,
		IntervalHours:    168,
		MinIdleHours:     2,
		StaleAfterDays:   30,
		ArchiveAfterDays: 90,
		PruneBuiltins:    true,
	}
}

// Config holds non-secret settings persisted to disk.
type Config struct {
	Provider      string              `json:"provider,omitempty"`
	Endpoint      string              `json:"endpoint"`
	Model         string              `json:"model"`
	ThinkingLevel string              `json:"thinking_level,omitempty"`
	HideThinking  bool                `json:"hide_thinking,omitempty"`
	Compaction    *CompactionSettings `json:"compaction,omitempty"`
	Evidence      *EvidenceConfig     `json:"evidence,omitempty"`
	Skills        *SkillsSettings     `json:"skills,omitempty"`
	Memory        *MemorySettings     `json:"memory,omitempty"`
	Curator       *CuratorSettings    `json:"curator,omitempty"`
	Agent         *AgentSettings      `json:"agent,omitempty"`
	Plugins       *PluginsSettings    `json:"plugins,omitempty"`

	// legacy field — migrated to secrets store on load, never written back
	apiKeyLegacy string `json:"-"`
}

// Runtime bundles config with the in-memory API key (never saved to config.json).
type Runtime struct {
	Provider      string
	Endpoint      string
	Model         string
	APIKey        string
	ThinkingLevel string
	HideThinking  bool
	Compaction    CompactionSettings
	Evidence      EvidenceConfig
	Skills        SkillsSettings
	Memory        MemorySettings
	Curator       CuratorSettings
	Agent         AgentSettings
	Plugins       PluginsSettings
	PreloadedSkills       []string
	PreloadedSkillsPrompt string
}

func DefaultInlineShellEnabled() bool {
	if runtime.GOOS == "windows" {
		return os.Getenv("WSL_DISTRO_NAME") != ""
	}
	return runtime.GOOS == "linux"
}

func Default() Config {
	return Config{
		Endpoint: DefaultEndpoint,
		Model:    DefaultModel,
		Compaction: &CompactionSettings{
			Enabled:          true,
			ReserveTokens:    16384,
			KeepRecentTokens: 20000,
		},
		Skills: &SkillsSettings{
			Enabled:             true,
			EnableSkillCommands: true,
			Paths:               []string{},
			Disabled:            []string{},
			ExternalDirs:        []string{},
			PlatformDisabled:    map[string][]string{"cli": {}, "tui": {}},
			GuardAgentCreated:   false,
			WriteApproval:       false,
			InlineShell:         DefaultInlineShellEnabled(),
			InlineShellTimeout:  10,
		},
		Memory:  func() *MemorySettings { m := DefaultMemory(); return &m }(),
		Curator: func() *CuratorSettings { c := DefaultCurator(); return &c }(),
		Agent: &AgentSettings{
			CodingContext: "auto",
		},
		Plugins: &PluginsSettings{
			Disabled: []string{},
		},
	}
}

func Dir() (string, error) {
	return enoughhome.HomeDir(), nil
}

func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

type fileConfig struct {
	Provider      string              `json:"provider,omitempty"`
	Endpoint      string              `json:"endpoint"`
	Model         string              `json:"model"`
	ThinkingLevel string              `json:"thinking_level,omitempty"`
	HideThinking  bool                `json:"hide_thinking,omitempty"`
	APIKey        string              `json:"api_key,omitempty"`
	Compaction    *CompactionSettings `json:"compaction,omitempty"`
	Evidence      *EvidenceConfig     `json:"evidence,omitempty"`
	Skills        *SkillsSettings     `json:"skills,omitempty"`
	Memory        *MemorySettings     `json:"memory,omitempty"`
	Curator       *CuratorSettings    `json:"curator,omitempty"`
	Agent         *AgentSettings      `json:"agent,omitempty"`
	Plugins       *PluginsSettings    `json:"plugins,omitempty"`
}

func Load() (Config, error) {
	cfg := Default()

	path, err := Path()
	if err != nil {
		return cfg, err
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		// Migration: check if old ~/.config/enough/config.json exists
		home, err := os.UserHomeDir()
		if err == nil {
			oldPath := filepath.Join(home, ".config", "enough", "config.json")
			oldData, err := os.ReadFile(oldPath)
			if err == nil {
				var raw fileConfig
				if err := json.Unmarshal(oldData, &raw); err == nil {
					cfg.Provider = raw.Provider
					cfg.Endpoint = raw.Endpoint
					cfg.Model = raw.Model
					cfg.ThinkingLevel = raw.ThinkingLevel
					cfg.HideThinking = raw.HideThinking
					cfg.apiKeyLegacy = raw.APIKey
					if raw.Compaction != nil {
						cfg.Compaction = raw.Compaction
					}
					if raw.Evidence != nil {
						cfg.Evidence = raw.Evidence
					}
					if raw.Skills != nil {
						cfg.Skills = raw.Skills
					}
					if raw.Memory != nil {
						cfg.Memory = raw.Memory
					}
					if raw.Curator != nil {
						cfg.Curator = raw.Curator
					}
					if raw.Agent != nil {
						cfg.Agent = raw.Agent
					}
					if cfg.Endpoint == "" {
						cfg.Endpoint = DefaultEndpoint
					}
					if cfg.Model == "" {
						cfg.Model = DefaultModel
					}
					if cfg.Compaction == nil {
						cfg.Compaction = &CompactionSettings{
							Enabled:          true,
							ReserveTokens:    16384,
							KeepRecentTokens: 20000,
						}
					}
					if cfg.Skills == nil {
						cfg.Skills = &SkillsSettings{
							Enabled:             true,
							EnableSkillCommands: true,
							Paths:               []string{},
							Disabled:            []string{},
							ExternalDirs:        []string{},
							PlatformDisabled:    map[string][]string{"cli": {}, "tui": {}},
							GuardAgentCreated:   false,
							WriteApproval:       false,
							InlineShell:         DefaultInlineShellEnabled(),
							InlineShellTimeout:  10,
						}
					}
					if cfg.Agent == nil {
						cfg.Agent = &AgentSettings{
							CodingContext: "auto",
						}
					}
					_ = Save(cfg)
					return cfg, nil
				}
			}
		}
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}

	var raw fileConfig
	if err := json.Unmarshal(data, &raw); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}

	cfg.Provider = raw.Provider
	cfg.Endpoint = raw.Endpoint
	cfg.Model = raw.Model
	cfg.ThinkingLevel = raw.ThinkingLevel
	cfg.HideThinking = raw.HideThinking
	cfg.apiKeyLegacy = raw.APIKey
	if raw.Compaction != nil {
		cfg.Compaction = raw.Compaction
	}
	if raw.Evidence != nil {
		cfg.Evidence = raw.Evidence
	}
	if raw.Skills != nil {
		cfg.Skills = raw.Skills
	}
	if raw.Memory != nil {
		cfg.Memory = raw.Memory
	}
	if raw.Curator != nil {
		cfg.Curator = raw.Curator
	}
	if raw.Agent != nil {
		cfg.Agent = raw.Agent
	}
	if raw.Plugins != nil {
		cfg.Plugins = raw.Plugins
	}

	if cfg.Provider == "" {
		cfg.Provider = ProviderOpenCode
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = DefaultEndpoint
	}
	if cfg.Model == "" {
		cfg.Model = DefaultModel
	}
	if cfg.Compaction == nil {
		cfg.Compaction = &CompactionSettings{
			Enabled:          true,
			ReserveTokens:    16384,
			KeepRecentTokens: 20000,
		}
	}
	if cfg.Skills == nil {
		cfg.Skills = &SkillsSettings{
			Enabled:             true,
			EnableSkillCommands: true,
			Paths:               []string{},
			Disabled:            []string{},
			ExternalDirs:        []string{},
			PlatformDisabled:    map[string][]string{"cli": {}, "tui": {}},
			GuardAgentCreated:   false,
			WriteApproval:       false,
			InlineShell:         DefaultInlineShellEnabled(),
			InlineShellTimeout:  10,
		}
	} else {
		// Ensure fields inside Skills are defaulted if missing from older files
		if cfg.Skills.PlatformDisabled == nil {
			cfg.Skills.PlatformDisabled = map[string][]string{"cli": {}, "tui": {}}
		}
		if cfg.Skills.Paths == nil {
			cfg.Skills.Paths = []string{}
		}
		if cfg.Skills.Disabled == nil {
			cfg.Skills.Disabled = []string{}
		}
		if cfg.Skills.ExternalDirs == nil {
			cfg.Skills.ExternalDirs = []string{}
		}
		if cfg.Skills.InlineShellTimeout <= 0 {
			cfg.Skills.InlineShellTimeout = 10
		}
	}
	if cfg.Agent == nil {
		cfg.Agent = &AgentSettings{
			CodingContext: "auto",
		}
	}
	if cfg.Plugins == nil {
		cfg.Plugins = &PluginsSettings{
			Disabled: []string{},
		}
	} else {
		if cfg.Plugins.Disabled == nil {
			cfg.Plugins.Disabled = []string{}
		}
	}

	// one-time migration: move api key from config.json into secret store
	if raw.APIKey != "" && !secrets.HasAPIKey() {
		if err := secrets.SetAPIKey(raw.APIKey); err == nil {
			_ = Save(cfg)
		}
	}

	return cfg, nil
}

func Save(cfg Config) error {
	if cfg.Endpoint == "" {
		cfg.Endpoint = DefaultEndpoint
	}
	if cfg.Model == "" {
		cfg.Model = DefaultModel
	}
	if cfg.Compaction == nil {
		cfg.Compaction = &CompactionSettings{
			Enabled:          true,
			ReserveTokens:    16384,
			KeepRecentTokens: 20000,
		}
	}
	if cfg.Skills == nil {
		cfg.Skills = &SkillsSettings{
			Enabled:             true,
			EnableSkillCommands: true,
			Paths:               []string{},
			Disabled:            []string{},
			ExternalDirs:        []string{},
			PlatformDisabled:    map[string][]string{"cli": {}, "tui": {}},
			GuardAgentCreated:   false,
			WriteApproval:       false,
			InlineShell:         DefaultInlineShellEnabled(),
			InlineShellTimeout:  10,
		}
	}
	if cfg.Memory == nil {
		m := DefaultMemory()
		cfg.Memory = &m
	}
	if cfg.Curator == nil {
		c := DefaultCurator()
		cfg.Curator = &c
	}
	if cfg.Agent == nil {
		cfg.Agent = &AgentSettings{
			CodingContext: "auto",
		}
	}
	if cfg.Plugins == nil {
		cfg.Plugins = &PluginsSettings{
			Disabled: []string{},
		}
	}

	dir, err := Dir()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	path, err := Path()
	if err != nil {
		return err
	}

	raw := fileConfig{
		Provider:      cfg.Provider,
		Endpoint:      cfg.Endpoint,
		Model:         cfg.Model,
		ThinkingLevel: cfg.ThinkingLevel,
		HideThinking:  cfg.HideThinking,
		Compaction:    cfg.Compaction,
		Evidence:      cfg.Evidence,
		Skills:        cfg.Skills,
		Memory:        cfg.Memory,
		Curator:       cfg.Curator,
		Agent:         cfg.Agent,
	}

	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o600)
}

func LoadRuntime() (Runtime, error) {
	cfg, err := Load()
	if err != nil {
		return Runtime{}, err
	}

	provider := cfg.Provider
	if provider == "" {
		provider = ProviderOpenCode
	}

	var key string
	switch provider {
	case ProviderCodex:
		creds, err := auth.ResolveCodexCredentials(context.Background())
		if err != nil {
			return Runtime{}, err
		}
		key = creds.AccessToken
		if cfg.Endpoint == "" || cfg.Endpoint == DefaultEndpoint {
			cfg.Endpoint = creds.BaseURL
		}
	default:
		key, err = secrets.GetAPIKey()
		if err != nil {
			return Runtime{}, err
		}
		if cfg.Endpoint == "" {
			cfg.Endpoint = DefaultEndpoint
		}
	}

	var comp CompactionSettings
	if cfg.Compaction != nil {
		comp = *cfg.Compaction
	} else {
		comp = CompactionSettings{
			Enabled:          true,
			ReserveTokens:    16384,
			KeepRecentTokens: 20000,
		}
	}

	ev := DefaultEvidence()
	if cfg.Evidence != nil {
		ev = *cfg.Evidence
	}

	var sk SkillsSettings
	if cfg.Skills != nil {
		sk = *cfg.Skills
	} else {
		sk = SkillsSettings{
			Enabled:             true,
			EnableSkillCommands: true,
			Paths:               []string{},
			Disabled:            []string{},
			ExternalDirs:        []string{},
			PlatformDisabled:    map[string][]string{"cli": {}, "tui": {}},
			GuardAgentCreated:   false,
			WriteApproval:       false,
			InlineShell:         DefaultInlineShellEnabled(),
			InlineShellTimeout:  10,
		}
	}

	mem := DefaultMemory()
	if cfg.Memory != nil {
		mem = *cfg.Memory
	}

	cur := DefaultCurator()
	if cfg.Curator != nil {
		cur = *cfg.Curator
	}

	ag := AgentSettings{
		CodingContext: "auto",
	}
	if cfg.Agent != nil {
		ag = *cfg.Agent
	}

	pl := PluginsSettings{
		Disabled: []string{},
	}
	if cfg.Plugins != nil {
		pl = *cfg.Plugins
	}

	return Runtime{
		Provider:      provider,
		Endpoint:      cfg.Endpoint,
		Model:         cfg.Model,
		APIKey:        key,
		ThinkingLevel: cfg.ThinkingLevel,
		HideThinking:  cfg.HideThinking,
		Compaction:    comp,
		Evidence:      ev,
		Skills:        sk,
		Memory:        mem,
		Curator:       cur,
		Agent:         ag,
		Plugins:       pl,
	}, nil
}

func Connected() bool {
	cfg, err := Load()
	if err != nil {
		return false
	}
	provider := cfg.Provider
	if provider == "" {
		provider = ProviderOpenCode
	}
	if provider == ProviderCodex {
		return auth.HasCodexAuth()
	}
	return secrets.HasAPIKey()
}
