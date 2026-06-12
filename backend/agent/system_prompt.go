package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/enough/enough/backend/config"
	"github.com/enough/enough/backend/memory"
	"github.com/enough/enough/backend/skills"
)

// Session system prompt — three tiers joined with "\n\n", built once per
// session and replayed verbatim every turn so the upstream prefix cache stays
// warm:
//
//   - stable   — identity (SOUL.md or built-in persona, always wrapped by the
//     Enough identity lockdown), memory guidance, skills guidance, skills
//     index.
//   - context  — project context files (AGENTS.md) discovered under the
//     working directory.
//   - volatile — frozen MEMORY.md / USER.md snapshots, date line (day
//     granularity only — minute precision would bust the cache), session ID.
//
// Enough policy (differs from Hermes): NO "Model:" / "Provider:" lines —
// base-model disclosure is forbidden.
//
// Invalidation happens only at session boundaries: /new, session switch,
// compaction, or explicit invalidation. Memory writes mid-session never
// touch the cached prompt.

// MemoryGuidance is appended to the stable tier when the memory tool is
// enabled. Ported from Hermes' MEMORY_GUIDANCE (agent/prompt_builder.py);
// the session_search reference is adapted away because Enough has no session
// search tool.
const MemoryGuidance = "You have persistent memory across sessions. Save durable facts using the memory " +
	"tool: user preferences, environment details, tool quirks, and stable conventions. " +
	"Memory is injected into every turn, so keep it compact and focused on facts that " +
	"will still matter later.\n" +
	"Prioritize what reduces future user steering — the most valuable memory is one " +
	"that prevents the user from having to correct or remind you again. " +
	"User preferences and recurring corrections matter more than procedural task details.\n" +
	"Do NOT save task progress, session outcomes, completed-work logs, or temporary TODO " +
	"state to memory; past session transcripts hold those. " +
	"Specifically: do not record PR numbers, issue numbers, commit SHAs, 'fixed bug X', " +
	"'submitted PR Y', 'Phase N done', file counts, or any artifact that will be stale " +
	"in 7 days. If a fact will be stale in a week, it does not belong in memory. " +
	"If you've discovered a new way to do something, solved a problem that could be " +
	"necessary later, save it as a skill with the skill tool.\n" +
	"Write memories as declarative facts, not instructions to yourself. " +
	"'User prefers concise responses' ✓ — 'Always respond concisely' ✗. " +
	"'Project uses pytest with xdist' ✓ — 'Run tests with pytest -n 4' ✗. " +
	"Imperative phrasing gets re-read as a directive in later sessions and can " +
	"cause repeated work or override the user's current request. Procedures and " +
	"workflows belong in skills, not memory."

const contextFileMaxChars = 24000

// SystemPromptInputs collects everything the tiered builder reads.
type SystemPromptInputs struct {
	WorkDir   string
	Cfg       config.Runtime
	ToolNames []string
	// Store provides the frozen MEMORY/USER snapshots. May be nil (memory
	// disabled).
	Store     *memory.Store
	SessionID string
	// Now anchors the volatile date line. Zero value means time.Now().
	Now time.Time
	PreloadedSkillsPrompt string
}

// BuildSessionSystemPrompt assembles the full session system prompt from the
// three tiers. Callers cache the result for the lifetime of the session.
func BuildSessionSystemPrompt(in SystemPromptInputs) string {
	stable := buildStableTier(in)
	context := buildContextTier(in)
	volatile := buildVolatileTier(in)

	var parts []string
	for _, p := range []string{stable, context, volatile} {
		if strings.TrimSpace(p) != "" {
			parts = append(parts, strings.TrimSpace(p))
		}
	}
	return strings.Join(parts, "\n\n")
}

func buildStableTier(in SystemPromptInputs) string {
	var parts []string

	// Identity: SOUL.md replaces the default persona when present, but the
	// identity lockdown + operating rules always apply (SOUL is scanned for
	// prompt injection before inject and appears exactly once).
	soul := ""
	if in.Cfg.Memory.Enabled || in.Cfg.Memory.UserProfileEnabled {
		soul = memory.LoadSoul()
	} else {
		// SOUL.md is identity, not memory — load it regardless, but only
		// seed the default file when the memory stack is on.
		if data, err := os.ReadFile(memory.SoulPath()); err == nil && strings.TrimSpace(string(data)) != "" {
			soul = memory.LoadSoul()
		}
	}
	if soul != "" {
		parts = append(parts, soul, identityLockdown, agentRules)
	} else {
		parts = append(parts, systemPrompt)
	}

	memoryToolEnabled := false
	for _, t := range in.ToolNames {
		if t == memory.ToolName {
			memoryToolEnabled = true
			break
		}
	}
	if memoryToolEnabled {
		parts = append(parts, MemoryGuidance)
	}

	if in.Cfg.Skills.Enabled {
		if hasSkillManage(in.ToolNames) {
			parts = append(parts, skills.GuidanceBlock)
		}
		if hasSkillTools(in.ToolNames) {
			if idx := skills.BuildIndexPrompt(in.WorkDir, in.Cfg, in.ToolNames); strings.TrimSpace(idx) != "" {
				parts = append(parts, idx)
			}
		} else {
			sks, _ := skills.DiscoverAllSkills(in.WorkDir, in.Cfg)
			if len(sks) > 0 {
				parts = append(parts, strings.TrimSpace(skills.FormatSkillsForPrompt(sks)))
			}
		}
	}
	if in.PreloadedSkillsPrompt != "" {
		parts = append(parts, in.PreloadedSkillsPrompt)
	}

	return strings.Join(parts, "\n\n")
}

// buildContextTier loads project context files. Currently AGENTS.md in the
// working directory root (top-level only, scanned for prompt injection,
// head/tail truncated when oversized).
func buildContextTier(in SystemPromptInputs) string {
	if in.WorkDir == "" {
		return ""
	}
	for _, name := range []string{"AGENTS.md", "agents.md"} {
		path := filepath.Join(in.WorkDir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		content := strings.TrimSpace(string(data))
		if content == "" {
			continue
		}
		if ids := memory.ContextThreatIDs(content); len(ids) > 0 {
			return fmt.Sprintf(
				"## %s\n\n[BLOCKED: %s contained threat pattern(s): %s. Its content was removed from the system prompt.]",
				name, name, strings.Join(ids, ", "))
		}
		return truncateContextFile(fmt.Sprintf("## %s\n\n%s", name, content), "AGENTS.md")
	}
	return ""
}

func buildVolatileTier(in SystemPromptInputs) string {
	var parts []string

	if in.Store != nil {
		if in.Cfg.Memory.Enabled {
			if block := in.Store.FormatForSystemPrompt(memory.TargetMemory); block != "" {
				parts = append(parts, block)
			}
		}
		if in.Cfg.Memory.UserProfileEnabled {
			if block := in.Store.FormatForSystemPrompt(memory.TargetUser); block != "" {
				parts = append(parts, block)
			}
		}
	}

	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	// Day granularity only — the prompt stays byte-stable for the full day.
	// Enough policy: never add Model:/Provider: lines here.
	line := "Conversation started: " + now.Format("Monday, January 02, 2006")
	if in.SessionID != "" {
		line += "\nSession ID: " + in.SessionID
	}
	parts = append(parts, line)

	return strings.Join(parts, "\n\n")
}

func truncateContextFile(content, filename string) string {
	if len(content) <= contextFileMaxChars {
		return content
	}
	headChars := contextFileMaxChars * 7 / 10
	tailChars := contextFileMaxChars * 2 / 10
	marker := fmt.Sprintf(
		"\n\n[...truncated %s: kept %d+%d of %d chars. Use file tools to read the full file.]\n\n",
		filename, headChars, tailChars, len(content))
	return content[:headChars] + marker + content[len(content)-tailChars:]
}
