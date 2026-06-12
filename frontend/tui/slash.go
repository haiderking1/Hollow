package tui

import (
	"fmt"
	"os"
	"strings"

	"github.com/enough/enough/backend/config"
	"github.com/enough/enough/backend/skills"
)

func (a *App) slashActive() bool {
	if a.mode != modeTask || a.running {
		return false
	}
	v := a.editor.Value()
	if !strings.HasPrefix(v, "/") {
		return false
	}
	rest := strings.TrimPrefix(v, "/")
	if strings.Contains(rest, " ") {
		return false
	}
	return true
}

func (a *App) slashFilter() string {
	return strings.ToLower(strings.TrimPrefix(a.editor.Value(), "/"))
}

func (a *App) filteredSlashCommands() []slashCommand {
	filter := a.slashFilter()
	out := make([]slashCommand, 0, len(slashCommands))
	for _, cmd := range slashCommands {
		if filter == "" || strings.HasPrefix(cmd.name, filter) {
			out = append(out, cmd)
		}
	}

	cfg, workDir := a.slashSkillsContext()
	if !cfg.Skills.Enabled || !cfg.Skills.EnableSkillCommands {
		return out
	}

	discovered, _ := skills.DiscoverAllSkills(workDir, cfg)
	for _, sk := range discovered {
		if skills.IsSkillDisabled(sk.Name, cfg) {
			continue
		}
		fmDummy := map[string]interface{}{
			"platforms":    sk.Platforms,
			"environments": sk.Environments,
		}
		if !skills.SkillMatchesPlatform(fmDummy) || !skills.SkillMatchesEnvironment(fmDummy) {
			continue
		}

		slug := skills.SkillNameToSlashSlug(sk.Name)
		desc := sk.Description
		if len(desc) > 50 {
			desc = desc[:47] + "..."
		}

		// 1. /skill:<name> form
		skillPrefixed := "skill:" + slug
		if filter == "" || strings.HasPrefix(skillPrefixed, filter) {
			out = append(out, slashCommand{
				name: skillPrefixed,
				desc: fmt.Sprintf("run skill: %s (%s)", sk.Name, desc),
			})
		}

		// 2. /<hyphen-slug> form
		if filter == "" || strings.HasPrefix(slug, filter) {
			collides := false
			for _, cmd := range slashCommands {
				if cmd.name == slug {
					collides = true
					break
				}
			}
			if !collides {
				out = append(out, slashCommand{
					name: slug,
					desc: fmt.Sprintf("run skill: %s (%s)", sk.Name, desc),
				})
			}
		}
	}

	return out
}

func (a *App) slashSkillsContext() (config.Runtime, string) {
	if a.agent != nil {
		return a.agent.Cfg(), a.agent.WorkDir()
	}

	cfg, err := config.LoadRuntime()
	if err != nil {
		return config.Runtime{}, ""
	}

	workDir := ""
	if a.session != nil {
		workDir = a.session.CWD()
	}
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	return cfg, workDir
}

func (a *App) clampSlashCursor() {
	cmds := a.filteredSlashCommands()
	if len(cmds) == 0 {
		a.slashCursor = 0
		return
	}
	if a.slashCursor >= len(cmds) {
		a.slashCursor = len(cmds) - 1
	}
	if a.slashCursor < 0 {
		a.slashCursor = 0
	}
}

func (a *App) renderSlashMenu(width int) string {
	if !a.slashActive() {
		return ""
	}

	cmds := a.filteredSlashCommands()
	a.clampSlashCursor()

	var lines []string
	if len(cmds) == 0 {
		lines = append(lines, a.styles.SlashDim.Render("  no matching commands"))
	} else {
		for i, cmd := range cmds {
			marker := "  "
			if i == a.slashCursor {
				marker = "› "
			}
			pad := 14 - len(cmd.name)
			if pad < 1 {
				pad = 1
			}
			line := marker + "/" + cmd.name + strings.Repeat(" ", pad) + cmd.desc
			if i == a.slashCursor {
				lines = append(lines, a.styles.SlashSelected.Render(line))
			} else {
				lines = append(lines, a.styles.SlashDim.Render(line))
			}
		}
	}

	hint := a.styles.SlashDim.Render("  ↑↓ pick   enter run   tab fill   esc close")
	body := strings.Join(lines, "\n") + "\n" + hint

	return a.styles.SlashMenu.
		Width(width - 2).
		Render(body)
}

func (a *App) autocompleteSlash() {
	cmds := a.filteredSlashCommands()
	if len(cmds) == 0 {
		return
	}
	a.clampSlashCursor()
	a.editor.SetValue("/" + cmds[a.slashCursor].name)
}

func (a *App) dismissSlashMenu() {
	a.editor.SetValue("")
	a.slashCursor = 0
}
