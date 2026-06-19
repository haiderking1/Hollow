package tui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/enough/enough/backend/config"
	"github.com/enough/enough/backend/skills"
	workflowpkg "github.com/enough/enough/backend/workflow"
)

const slashMenuVisible = 5

func slashMenuViewport(cursor, total int) (start, end int) {
	if total <= 0 {
		return 0, 0
	}
	if total <= slashMenuVisible {
		return 0, total
	}
	start = cursor - slashMenuVisible + 1
	if start < 0 {
		start = 0
	}
	if start > total-slashMenuVisible {
		start = total - slashMenuVisible
	}
	return start, start + slashMenuVisible
}

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
	for _, saved := range workflowpkg.ScanSaved(a.workDir()) {
		if filter == "" || strings.HasPrefix(saved.Name, filter) {
			desc := saved.Meta.Description
			if desc == "" {
				desc = "run saved dynamic workflow"
			}
			out = append(out, slashCommand{name: saved.Name, desc: desc})
		}
	}

	// Only autocomplete discovered skills when explicitly typing /skill:…
	if !strings.HasPrefix(filter, "skill:") {
		return out
	}

	cfg, workDir := a.slashSkillsContext()
	if !cfg.Skills.Enabled || !cfg.Skills.EnableSkillCommands {
		return out
	}

	skillFilter := strings.TrimPrefix(filter, "skill:")
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
		if skillFilter != "" && !strings.HasPrefix(slug, skillFilter) {
			continue
		}

		desc := sk.Description
		if len(desc) > 50 {
			desc = desc[:47] + "..."
		}
		out = append(out, slashCommand{
			name: "skill:" + slug,
			desc: fmt.Sprintf("run skill: %s (%s)", sk.Name, desc),
		})
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

	pickStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#66D9EF")).Bold(true)

	if len(cmds) == 0 {
		body := a.styles.SlashDim.Render("  no matching commands")
		return lipgloss.NewStyle().Width(width).Render(body)
	}

	leftWidth := 16
	if width > 100 {
		leftWidth = 22
	} else if width > 80 {
		leftWidth = 20
	}
	gap := "  "

	start, end := slashMenuViewport(a.slashCursor, len(cmds))
	var rows []string
	for i := start; i < end; i++ {
		cmd := cmds[i]
		selected := i == a.slashCursor

		marker := "  "
		if selected {
			marker = "→ "
		}
		name := truncateRunes(cmd.name, leftWidth)
		nameCell := marker + name

		desc := cmd.desc
		descBudget := width - 4 - leftWidth - len(gap) - 2
		if descBudget > 8 {
			desc = truncateRunes(desc, descBudget)
		}

		var left, right string
		if selected {
			left = pickStyle.Render(fmt.Sprintf("%-*s", leftWidth+2, nameCell))
			right = pickStyle.Render(desc)
		} else {
			left = a.styles.SlashName.Render(fmt.Sprintf("%-*s", leftWidth+2, nameCell))
			right = a.styles.SlashDesc.Render(desc)
		}

		leftCol := lipgloss.NewStyle().Width(leftWidth + 2).Render(left)
		rows = append(rows, "  "+leftCol+gap+right)
	}

	pos := a.slashCursor + 1
	counter := a.styles.SlashDim.Render(fmt.Sprintf("  (%d/%d)", pos, len(cmds)))
	body := strings.Join(rows, "\n") + "\n" + counter

	return lipgloss.NewStyle().Width(width).Render(body)
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
