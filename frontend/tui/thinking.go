package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/enough/enough/backend/config"
	"github.com/enough/enough/backend/opencode"
)

func (a *App) loadThinkingSettings() {
	cfg, err := config.Load()
	if err != nil {
		a.thinkingLevel = opencode.ThinkingOff
		return
	}
	a.thinkingLevel = opencode.ParseThinkingLevel(cfg.ThinkingLevel)
	a.hideThinking = cfg.HideThinking
}

func (a *App) cycleThinkingLevel() {
	cfg, err := config.Load()
	if err != nil {
		a.appendMessage("error", err.Error())
		return
	}
	if !opencode.SupportsThinking(cfg.Model) {
		a.appendMessage("system", "Current model does not support thinking")
		return
	}

	next := opencode.CycleThinkingLevel(opencode.ParseThinkingLevel(cfg.ThinkingLevel), cfg.Model)
	cfg.ThinkingLevel = string(next)
	if err := config.Save(cfg); err != nil {
		a.appendMessage("error", err.Error())
		return
	}

	a.thinkingLevel = next
	a.appendMessage("system", fmt.Sprintf("Thinking level: %s", next))
}

func (a *App) toggleThinkingVisibility() {
	cfg, err := config.Load()
	if err != nil {
		a.appendMessage("error", err.Error())
		return
	}

	cfg.HideThinking = !cfg.HideThinking
	if err := config.Save(cfg); err != nil {
		a.appendMessage("error", err.Error())
		return
	}

	a.hideThinking = cfg.HideThinking
	state := "visible"
	if a.hideThinking {
		state = "hidden"
	}
	a.appendMessage("system", fmt.Sprintf("Thinking blocks: %s", state))
}

func (a *App) composerStyle() lipgloss.Style {
	return a.styles.InputBox.Copy().BorderForeground(thinkingBorderColor(string(a.thinkingLevel)))
}
