package tui

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/enough/enough/backend/agent"
	"github.com/enough/enough/backend/approval"
	"github.com/enough/enough/backend/auth"
	"github.com/enough/enough/backend/config"
	"github.com/enough/enough/backend/core"
	"github.com/enough/enough/backend/memory"
	"github.com/enough/enough/backend/skills"
)

func (a *App) handleSlash(input string) {
	name, arg, _ := strings.Cut(strings.TrimPrefix(input, "/"), " ")
	name = strings.ToLower(strings.TrimSpace(name))
	arg = strings.TrimSpace(arg)

	switch name {
	case "connect":
		if arg != "" {
			a.saveAPIKey(arg)
			return
		}
		a.mode = modeConnect
		a.editor = NewEditor(1024)
		endpoint, model, err := auth.Settings()
		if err != nil {
			a.appendMessage("error", err.Error())
			a.mode = modeTask
			a.editor = NewEditor(512)
			return
		}
		a.appendMessage("system", fmt.Sprintf("connect — %s · %s\npaste your api key below", endpoint, model))
	case "sessions":
		a.showSessionsList()
	case "resume":
		a.openSessionPicker()
	case "new":
		a.startNewSession()
	case "model":
		a.openModelPicker(arg)
	case "compact":
		a.startCompact(arg)
	case "auto-compact":
		cfg, err := config.Load()
		if err != nil {
			a.appendMessage("error", err.Error())
			return
		}
		if cfg.Compaction == nil {
			cfg.Compaction = &config.CompactionSettings{
				Enabled:          true,
				ReserveTokens:    16384,
				KeepRecentTokens: 20000,
			}
		}

		val := strings.ToLower(arg)
		if val == "on" {
			cfg.Compaction.Enabled = true
			a.appendMessage("system", "Auto-compaction enabled")
		} else if val == "off" {
			cfg.Compaction.Enabled = false
			a.appendMessage("system", "Auto-compaction disabled")
		} else {
			a.appendMessage("error", "Usage: /auto-compact on|off")
			return
		}

		if err := config.Save(cfg); err != nil {
			a.appendMessage("error", err.Error())
			return
		}

		if runCfg, err := config.LoadRuntime(); err == nil {
			a.mu.Lock()
			if a.agent != nil {
				a.agent.UpdateConfig(runCfg)
			}
			a.mu.Unlock()
		}
		a.requestRender()
	case "tree":
		if a.session == nil {
			a.appendMessage("error", "no active session")
			return
		}
		if a.running {
			a.appendMessage("error", "wait for the agent to finish")
			return
		}

		roots := a.session.GetTree()
		if len(roots) == 0 {
			a.appendMessage("system", "No entries in session tree")
			return
		}

		a.treePickerNodes = a.buildFlatTreeNodes(roots, 0, a.session.LeafID())
		a.treePickerCursor = 0
		a.treePickerConfirm = 0
		a.treePickerChoice = 0
		a.treePickerTarget = ""
		a.mode = modeTreePicker
		a.editor.SetValue("")
		a.requestRender()
	case "skills":
		sub, subArg, _ := strings.Cut(arg, " ")
		sub = strings.ToLower(strings.TrimSpace(sub))
		subArg = strings.TrimSpace(subArg)

		switch sub {
		case "pending":
			records, err := approval.ListPending(approval.SubsystemSkills)
			if err != nil {
				a.appendMessage("error", err.Error())
				return
			}
			if len(records) == 0 {
				a.appendMessage("system", "No pending skills updates.")
				return
			}
			var lines []string
			lines = append(lines, "Pending skills updates:")
			for _, r := range records {
				lines = append(lines, fmt.Sprintf("  - %s: %s (origin: %s)", r.ID, r.Summary, r.Origin))
			}
			a.appendMessage("system", strings.Join(lines, "\n"))
			a.requestRender()
			return
		case "diff":
			if subArg == "" {
				a.appendMessage("error", "Usage: /skills diff <id>")
				return
			}
			diff, err := a.pendingWriteDiff(approval.SubsystemSkills, subArg)
			if err != nil {
				a.appendMessage("error", err.Error())
				return
			}
			a.appendMessage("system", fmt.Sprintf("Staged update %s:\n%s", subArg, diff))
			a.requestRender()
			return
		case "approve":
			if subArg == "" {
				a.appendMessage("error", "Usage: /skills approve <id>")
				return
			}
			msg, err := a.approvePendingWrite(approval.SubsystemSkills, subArg)
			if err != nil {
				a.appendMessage("error", fmt.Sprintf("Failed to apply staged update: %s", err.Error()))
				return
			}
			a.appendMessage("system", msg)
			a.requestRender()
			return
		case "reject":
			if subArg == "" {
				a.appendMessage("error", "Usage: /skills reject <id>")
				return
			}
			msg, err := a.rejectPendingWrite(approval.SubsystemSkills, subArg)
			if err != nil {
				a.appendMessage("error", err.Error())
				return
			}
			a.appendMessage("system", msg)
			a.requestRender()
			return
		case "approval":
			if subArg == "" {
				a.appendMessage("error", "Usage: /skills approval on|off")
				return
			}
			cfg, err := config.Load()
			if err != nil {
				a.appendMessage("error", err.Error())
				return
			}
			if subArg == "on" {
				cfg.Skills.WriteApproval = true
				a.appendMessage("system", "Skills write approval enabled")
			} else if subArg == "off" {
				cfg.Skills.WriteApproval = false
				a.appendMessage("system", "Skills write approval disabled")
			} else {
				a.appendMessage("error", "Usage: /skills approval on|off")
				return
			}
			if err := config.Save(cfg); err != nil {
				a.appendMessage("error", err.Error())
				return
			}
			if runCfg, err := config.LoadRuntime(); err == nil {
				a.mu.Lock()
				if a.agent != nil {
					a.agent.UpdateConfig(runCfg)
				}
				a.mu.Unlock()
			}
			a.requestRender()
			return
		}

		cfg, err := config.LoadRuntime()
		if err != nil {
			a.appendMessage("error", err.Error())
			return
		}
		ag := a.ensureAgent(cfg)
		discovered, _ := skills.DiscoverAllSkills(ag.WorkDir(), cfg)
		if len(discovered) == 0 {
			a.appendMessage("system", "No skills discovered. Skills live in ~/.enough/skills/")
			return
		}
		var lines []string
		lines = append(lines, fmt.Sprintf("Discovered %d skills:", len(discovered)))
		for _, sk := range discovered {
			cat := sk.Category
			if cat == "" {
				cat = "general"
			}
			lines = append(lines, fmt.Sprintf("- [%s] %s: %s", cat, sk.Name, sk.Description))
		}
		a.appendMessage("system", strings.Join(lines, "\n"))
	case "skills-toggle":
		cfg, err := config.Load()
		if err != nil {
			a.appendMessage("error", err.Error())
			return
		}
		val := strings.ToLower(arg)
		if val == "on" {
			cfg.Skills.Enabled = true
			a.appendMessage("system", "Skills system enabled")
		} else if val == "off" {
			cfg.Skills.Enabled = false
			a.appendMessage("system", "Skills system disabled")
		} else {
			a.appendMessage("error", "Usage: /skills-toggle on|off")
			return
		}
		if err := config.Save(cfg); err != nil {
			a.appendMessage("error", err.Error())
			return
		}
		if runCfg, err := config.LoadRuntime(); err == nil {
			a.mu.Lock()
			if a.agent != nil {
				a.agent.UpdateConfig(runCfg)
			}
			a.mu.Unlock()
		}
		a.requestRender()
	case "skill-commands":
		cfg, err := config.Load()
		if err != nil {
			a.appendMessage("error", err.Error())
			return
		}
		val := strings.ToLower(arg)
		if val == "on" {
			cfg.Skills.EnableSkillCommands = true
			a.appendMessage("system", "Skill commands enabled")
		} else if val == "off" {
			cfg.Skills.EnableSkillCommands = false
			a.appendMessage("system", "Skill commands disabled")
		} else {
			a.appendMessage("error", "Usage: /skill-commands on|off")
			return
		}
		if err := config.Save(cfg); err != nil {
			a.appendMessage("error", err.Error())
			return
		}
		if runCfg, err := config.LoadRuntime(); err == nil {
			a.mu.Lock()
			if a.agent != nil {
				a.agent.UpdateConfig(runCfg)
			}
			a.mu.Unlock()
		}
		a.requestRender()
	case "skill-archive":
		if arg == "" {
			a.appendMessage("error", "Usage: /skill-archive <name>")
			return
		}
		ok, msg := skills.ArchiveSkill(arg)
		if ok {
			a.appendMessage("system", msg)
			a.bumpChat()
		} else {
			a.appendMessage("error", msg)
		}
		a.requestRender()
	case "skill-restore":
		if arg == "" {
			a.appendMessage("error", "Usage: /skill-restore <name>")
			return
		}
		ok, msg := skills.RestoreSkill(arg)
		if ok {
			a.appendMessage("system", msg)
			a.bumpChat()
		} else {
			a.appendMessage("error", msg)
		}
		a.requestRender()
	case "reload-skills":
		cfg, err := config.LoadRuntime()
		if err != nil {
			a.appendMessage("error", err.Error())
			return
		}
		workDir := ""
		if a.session != nil {
			workDir = a.session.CWD()
		}
		if workDir == "" {
			workDir, _ = os.Getwd()
		}
		diff, err := skills.ReloadSkills(workDir, cfg)
		if err != nil {
			a.appendMessage("error", fmt.Sprintf("Failed to reload skills: %v", err))
			return
		}

		var lines []string
		lines = append(lines, fmt.Sprintf("Reloaded skills: total %d skills, %d slash commands.", diff.Total, diff.Commands))
		if len(diff.Added) > 0 {
			lines = append(lines, fmt.Sprintf("  Added (%d):", len(diff.Added)))
			for _, sk := range diff.Added {
				lines = append(lines, fmt.Sprintf("    - %s: %s", sk["name"], sk["description"]))
			}
		}
		if len(diff.Removed) > 0 {
			lines = append(lines, fmt.Sprintf("  Removed (%d):", len(diff.Removed)))
			for _, sk := range diff.Removed {
				lines = append(lines, fmt.Sprintf("    - %s: %s", sk["name"], sk["description"]))
			}
		}
		a.appendMessage("system", strings.Join(lines, "\n"))
		a.requestRender()
	case "memory":
		sub, subArg, _ := strings.Cut(arg, " ")
		sub = strings.ToLower(strings.TrimSpace(sub))
		subArg = strings.TrimSpace(subArg)

		switch sub {
		case "pending":
			records, err := approval.ListPending(approval.SubsystemMemory)
			if err != nil {
				a.appendMessage("error", err.Error())
				return
			}
			if len(records) == 0 {
				a.appendMessage("system", "No pending memory updates.")
				return
			}
			var lines []string
			lines = append(lines, "Pending memory updates:")
			for _, r := range records {
				lines = append(lines, fmt.Sprintf("  - %s: %s (origin: %s)", r.ID, r.Summary, r.Origin))
			}
			a.appendMessage("system", strings.Join(lines, "\n"))
			a.requestRender()
			return
		case "approve":
			if subArg == "" {
				a.appendMessage("error", "Usage: /memory approve <id>")
				return
			}
			msg, err := a.approvePendingWrite(approval.SubsystemMemory, subArg)
			if err != nil {
				a.appendMessage("error", fmt.Sprintf("Failed to apply memory update: %s", err.Error()))
				return
			}
			a.appendMessage("system", msg)
			a.requestRender()
			return
		case "reject":
			if subArg == "" {
				a.appendMessage("error", "Usage: /memory reject <id>")
				return
			}
			msg, err := a.rejectPendingWrite(approval.SubsystemMemory, subArg)
			if err != nil {
				a.appendMessage("error", err.Error())
				return
			}
			a.appendMessage("system", msg)
			a.requestRender()
			return
		case "approval":
			cfg, err := config.Load()
			if err != nil {
				a.appendMessage("error", err.Error())
				return
			}
			switch strings.ToLower(subArg) {
			case "on":
				cfg.Memory.WriteApproval = true
				a.appendMessage("system", "Memory write approval enabled — memory tool writes will be staged.")
			case "off":
				cfg.Memory.WriteApproval = false
				a.appendMessage("system", "Memory write approval disabled.")
			default:
				a.appendMessage("error", "Usage: /memory approval on|off")
				return
			}
			if err := config.Save(cfg); err != nil {
				a.appendMessage("error", err.Error())
				return
			}
			if runCfg, err := config.LoadRuntime(); err == nil {
				a.mu.Lock()
				if a.agent != nil {
					a.agent.UpdateConfig(runCfg)
				}
				a.mu.Unlock()
			}
			a.requestRender()
			return
		}

		a.showMemory()
	case "curator-run":
		a.runCurator(arg)
	case "curator-status":
		cfg, err := config.LoadRuntime()
		if err != nil {
			a.appendMessage("error", err.Error())
			return
		}
		a.appendMessage("system", skills.CuratorStatusString(cfg.Curator))
		a.requestRender()
	case "curator-pin":
		if arg == "" {
			a.appendMessage("error", "Usage: /curator-pin <skill>")
			return
		}
		skills.PinSkill(arg)
		a.appendMessage("system", fmt.Sprintf("Pinned '%s' — the curator will never archive or consolidate it.", arg))
		a.requestRender()
	case "curator-unpin":
		if arg == "" {
			a.appendMessage("error", "Usage: /curator-unpin <skill>")
			return
		}
		skills.UnpinSkill(arg)
		a.appendMessage("system", fmt.Sprintf("Unpinned '%s'.", arg))
		a.requestRender()
	case "curator-pause":
		switch strings.ToLower(arg) {
		case "on":
			skills.SetCuratorPaused(true)
			a.appendMessage("system", "Curator paused")
		case "off":
			skills.SetCuratorPaused(false)
			a.appendMessage("system", "Curator resumed")
		default:
			a.appendMessage("error", "Usage: /curator-pause on|off")
			return
		}
		a.requestRender()
	default:
		isSkillCmd := false
		skillName := ""
		if strings.HasPrefix(name, "skill:") {
			isSkillCmd = true
			skillName = strings.TrimPrefix(name, "skill:")
		} else {
			cfg, workDir := a.slashSkillsContext()
			if cfg.Skills.Enabled && cfg.Skills.EnableSkillCommands {
				discovered, _ := skills.DiscoverAllSkills(workDir, cfg)
				for _, sk := range discovered {
					if !skills.IsSkillDisabled(sk.Name, cfg) {
						fmDummy := map[string]interface{}{
							"platforms":    sk.Platforms,
							"environments": sk.Environments,
						}
						if skills.SkillMatchesPlatform(fmDummy) && skills.SkillMatchesEnvironment(fmDummy) {
							if skills.SkillNameToSlashSlug(sk.Name) == name {
								isSkillCmd = true
								skillName = sk.Name
								break
							}
						}
					}
				}
			}
		}

		if isSkillCmd {
			if !auth.Connected() {
				a.appendMessage("error", "not connected — type / and pick connect")
				return
			}
			if a.running {
				a.appendMessage("error", "wait for the agent to finish")
				return
			}
			cfg, err := config.LoadRuntime()
			if err != nil {
				a.appendMessage("error", err.Error())
				return
			}
			ag := a.ensureAgent(cfg)
			sessionId := ""
			if a.session != nil {
				sessionId = a.session.SessionID()
			}
			expandedPrompt, cleanBody, err := skills.ExpandSkillSlashCommand(skillName, arg, ag.WorkDir(), cfg, sessionId)
			if err != nil {
				a.appendMessage("error", fmt.Sprintf("failed to expand skill: %v", err))
				return
			}

			if a.compacting {
				a.compactionQueuedMessages = append(a.compactionQueuedMessages, expandedPrompt)
				a.messages = append(a.messages, chatMsg{
					role:     "skillSummary",
					toolName: skillName,
					toolArgs: arg,
					text:     cleanBody,
				})
				a.bumpChat()
				a.requestRender()
				return
			}

			a.messages = append(a.messages, chatMsg{
				role:     "skillSummary",
				toolName: skillName,
				toolArgs: arg,
				text:     cleanBody,
			})
			a.bumpChat()

			a.startAgent(expandedPrompt)
			a.requestRender()
			return
		}
		a.appendMessage("error", "unknown command: /"+name)
	}
}

// showMemory renders the live MEMORY.md / USER.md state.
func (a *App) showMemory() {
	cfg, err := config.LoadRuntime()
	if err != nil {
		a.appendMessage("error", err.Error())
		return
	}
	if !cfg.Memory.Enabled && !cfg.Memory.UserProfileEnabled {
		a.appendMessage("system", "Memory is disabled (config.json → memory.memory_enabled)")
		return
	}
	store := memory.NewStore(cfg.Memory.MemoryCharLimit, cfg.Memory.UserCharLimit)
	store.LoadFromDisk()
	var lines []string
	for _, target := range []string{memory.TargetMemory, memory.TargetUser} {
		res := store.Read(target)
		label := "MEMORY.md"
		if target == memory.TargetUser {
			label = "USER.md"
		}
		lines = append(lines, fmt.Sprintf("%s (%s):", label, res.Usage))
		if len(res.Entries) == 0 {
			lines = append(lines, "  (empty)")
		}
		for _, e := range res.Entries {
			lines = append(lines, "  § "+strings.ReplaceAll(e, "\n", "\n    "))
		}
	}
	lines = append(lines, "", "Files: ~/.enough/memories/  ·  Identity: ~/.enough/SOUL.md")
	a.appendMessage("system", strings.Join(lines, "\n"))
	a.requestRender()
}

// runCurator handles /curator-run [dry-run].
func (a *App) runCurator(arg string) {
	if !auth.Connected() {
		a.appendMessage("error", "not connected — type / and pick connect")
		return
	}
	cfg, err := config.LoadRuntime()
	if err != nil {
		a.appendMessage("error", err.Error())
		return
	}
	dryRun := false
	switch strings.ToLower(strings.TrimSpace(arg)) {
	case "":
	case "dry-run", "--dry-run", "dry":
		dryRun = true
	default:
		a.appendMessage("error", "Usage: /curator-run [dry-run]")
		return
	}
	label := "Curator pass started (summary will appear when done)"
	if dryRun {
		label = "Curator dry-run started (report only — no mutations)"
	}
	a.appendMessage("system", label)
	a.requestRender()
	res := agent.RunCuratorReview(cfg, dryRun, false, a.notifyAsync)
	a.appendMessage("system", "Curator auto-transitions: "+res.AutoSummary)
	a.requestRender()
}

func (a *App) saveAPIKey(key string) {
	a.mode = modeTask
	a.editor = NewEditor(512)

	if err := auth.SaveAPIKey(key); err != nil {
		a.appendMessage("error", err.Error())
		return
	}

	a.appendMessage("assistant", "Done — connected. api key saved securely.")
	if a.agent != nil {
		_ = a.agent.Reset()
		a.agent = nil
	}
	if a.session != nil {
		_ = a.session.NewSession()
		a.messages = nil
		a.bumpChat()
	}
}

func (a *App) cancelConnect() {
	if a.mode == modeConnect {
		a.mode = modeTask
		a.editor = NewEditor(512)
		a.editor.SetValue("")
		a.appendMessage("system", "connect cancelled")
	}
}

func (a *App) startCompact(customInstructions string) {
	if !auth.Connected() {
		a.appendMessage("error", "not connected — type / and pick connect")
		return
	}
	if a.session == nil {
		a.appendMessage("error", "no active session")
		return
	}
	cfg, err := config.LoadRuntime()
	if err != nil {
		a.appendMessage("error", err.Error())
		return
	}

	cmdLine := "/compact"
	if strings.TrimSpace(customInstructions) != "" {
		cmdLine += " " + strings.TrimSpace(customInstructions)
	}
	a.appendMessage("user", cmdLine)
	a.setCompacting(true, "Compacting context...")
	a.requestRender()

	a.runAgentTask(func(emit func(core.Event)) {
		ag := a.ensureAgent(cfg)
		ag.SetEmit(emit)

		_, _ = ag.Compact(context.Background(), customInstructions)
	})
}

func (a *App) runAgentTask(task func(emit func(core.Event))) {
	a.mu.Lock()
	a.running = true
	ch := make(chan core.Event, 64)
	a.agentCh = ch
	a.mu.Unlock()

	go func() {
		defer close(ch)
		emit := func(e core.Event) {
			ch <- e
			a.requestRender()
		}
		task(emit)
	}()
	a.requestRender()
}
