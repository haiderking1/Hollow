package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/enough/enough/backend/auth"
	"github.com/enough/enough/backend/config"
	"github.com/enough/enough/backend/opencode"
	"github.com/enough/enough/backend/secrets"
)

type modelPickerFocus int

const (
	modelPickerFocusProvider modelPickerFocus = iota
	modelPickerFocusModel
	modelPickerFocusThinking
)

func (a *App) startModelFetch() {
	endpoint := config.DefaultEndpoint
	if cfg, err := config.Load(); err == nil && cfg.Endpoint != "" {
		endpoint = cfg.Endpoint
	}
	apiKey, _ := secrets.GetAPIKey()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if apiKey != "" {
			_ = a.modelRegistry.Refresh(ctx, endpoint, apiKey)
		}
		if auth.HasCodexAuth() {
			if creds, err := auth.ResolveCodexCredentials(ctx); err == nil {
				_ = a.modelRegistry.RefreshCodex(ctx, creds.AccessToken)
			}
		}
		a.requestRender()
	}()
}

func (a *App) openModelPicker(filter string) {
	a.modelPickerFilter = strings.ToLower(strings.TrimSpace(filter))
	a.modelPickerProviderCursor = 0
	a.modelPickerCursor = 0
	a.modelPickerStatus = ""
	a.modelPickerThinking = opencode.ThinkingOff
	a.modelPickerFocus = modelPickerFocusModel

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		endpoint := config.DefaultEndpoint
		if cfg, err := config.Load(); err == nil && cfg.Endpoint != "" {
			endpoint = cfg.Endpoint
		}
		if apiKey, _ := secrets.GetAPIKey(); apiKey != "" {
			_ = a.modelRegistry.Refresh(ctx, endpoint, apiKey)
			a.requestRender()
		}
		if auth.HasCodexAuth() {
			if creds, err := auth.ResolveCodexCredentials(ctx); err == nil {
				_ = a.modelRegistry.RefreshCodex(ctx, creds.AccessToken)
				a.requestRender()
			}
		}
	}()

	cfg, err := config.Load()
	if err == nil {
		a.modelPickerProviderCursor = opencode.ProviderIndex(cfg.Provider)
		a.modelPickerThinking = opencode.ParseThinkingLevel(cfg.ThinkingLevel)
		if a.modelPickerThinking == opencode.ThinkingOff && cfg.ThinkingLevel == "" {
			if m, ok := opencode.LookupCatalogModel(cfg.Model); ok && opencode.SupportsThinking(m.ID) {
				a.modelPickerThinking = opencode.ThinkingMedium
			}
		}
		for i, m := range a.filteredModelPickerItems() {
			if m.ID == cfg.Model {
				a.modelPickerCursor = i
				break
			}
		}
	}

	a.mode = modeModelPicker
	a.editor.SetValue("")
	a.requestRender()
}

func (a *App) dismissModelPicker() {
	a.mode = modeTask
	a.modelPickerFilter = ""
	a.modelPickerProviderCursor = 0
	a.modelPickerCursor = 0
	a.modelPickerStatus = ""
	a.modelPickerFocus = modelPickerFocusModel
	a.requestRender()
}

func (a *App) modelPickerProvider() opencode.ProviderInfo {
	providers := opencode.ModelProviders()
	a.clampModelPickerProviderCursor()
	return providers[a.modelPickerProviderCursor]
}

func (a *App) filteredModelPickerItems() []opencode.ModelInfo {
	items := opencode.ModelsForProvider(a.modelPickerProvider().ID, a.modelRegistry)
	filter := a.modelPickerFilter
	if filter == "" {
		return items
	}
	out := make([]opencode.ModelInfo, 0, len(items))
	for _, m := range items {
		if strings.Contains(strings.ToLower(m.ID), filter) ||
			strings.Contains(strings.ToLower(m.Name), filter) {
			out = append(out, m)
		}
	}
	return out
}

func (a *App) clampModelPickerProviderCursor() {
	providers := opencode.ModelProviders()
	if len(providers) == 0 {
		a.modelPickerProviderCursor = 0
		return
	}
	if a.modelPickerProviderCursor >= len(providers) {
		a.modelPickerProviderCursor = len(providers) - 1
	}
	if a.modelPickerProviderCursor < 0 {
		a.modelPickerProviderCursor = 0
	}
}

func (a *App) clampModelPickerCursor() {
	items := a.filteredModelPickerItems()
	if len(items) == 0 {
		a.modelPickerCursor = 0
		return
	}
	if a.modelPickerCursor >= len(items) {
		a.modelPickerCursor = len(items) - 1
	}
	if a.modelPickerCursor < 0 {
		a.modelPickerCursor = 0
	}
}

func (a *App) modelPickerCurrent() (opencode.ModelInfo, bool) {
	items := a.filteredModelPickerItems()
	a.clampModelPickerCursor()
	if len(items) == 0 {
		return opencode.ModelInfo{}, false
	}
	return items[a.modelPickerCursor], true
}

func (a *App) providerConnected(id string) bool {
	switch id {
	case config.ProviderCodex:
		return auth.HasCodexAuth()
	default:
		return secrets.HasAPIKey()
	}
}

func (a *App) syncModelPickerThinking() {
	m, ok := a.modelPickerCurrent()
	if !ok {
		return
	}
	levels := opencode.SupportedThinkingLevels(m.ID)
	if len(levels) <= 1 {
		a.modelPickerThinking = opencode.ThinkingOff
		return
	}
	for _, l := range levels {
		if l == a.modelPickerThinking {
			return
		}
	}
	a.modelPickerThinking = levels[0]
}

func (a *App) cycleModelPickerFocus() {
	switch a.modelPickerFocus {
	case modelPickerFocusProvider:
		a.modelPickerFocus = modelPickerFocusModel
	case modelPickerFocusModel:
		if m, ok := a.modelPickerCurrent(); ok && opencode.SupportsThinking(m.ID) {
			a.modelPickerFocus = modelPickerFocusThinking
		} else {
			a.modelPickerFocus = modelPickerFocusProvider
		}
	default:
		a.modelPickerFocus = modelPickerFocusProvider
	}
}

func (a *App) moveModelPickerThinking(delta int) {
	m, ok := a.modelPickerCurrent()
	if !ok {
		return
	}
	a.modelPickerThinking = opencode.StepThinkingLevel(a.modelPickerThinking, m.ID, delta)
	a.requestRender()
}

func (a *App) cycleModelPickerThinking() {
	m, ok := a.modelPickerCurrent()
	if !ok || !opencode.SupportsThinking(m.ID) {
		return
	}
	a.modelPickerThinking = opencode.CycleThinkingLevel(a.modelPickerThinking, m.ID)
	a.requestRender()
}

func (a *App) applyModelSelection() {
	provider := a.modelPickerProvider()
	if !a.providerConnected(provider.ID) {
		a.modelPickerStatus = fmt.Sprintf("%s not connected — use /connect", provider.Name)
		return
	}

	m, ok := a.modelPickerCurrent()
	if !ok {
		a.modelPickerStatus = "No models available"
		return
	}

	thinking := ""
	if opencode.SupportsThinking(m.ID) {
		thinking = string(a.modelPickerThinking)
	}

	if err := config.ApplyProviderModel(provider.ID, m.ID, thinking); err != nil {
		a.modelPickerStatus = err.Error()
		return
	}

	a.thinkingLevel = opencode.ParseThinkingLevel(thinking)
	a.dismissModelPicker()

	msg := fmt.Sprintf("Provider: %s · Model: %s (%s ctx)", provider.Name, m.Name, opencode.FormatContextWindow(m.ContextWindow))
	if opencode.SupportsThinking(m.ID) && thinking != "" {
		msg += fmt.Sprintf(" · thinking %s", thinking)
	} else if m.Reasoning {
		msg += " · reasoning"
	}
	a.appendMessage("system", msg)

	if runCfg, err := config.LoadRuntime(); err == nil {
		a.mu.Lock()
		if a.agent != nil {
			a.agent.UpdateConfig(runCfg)
		}
		a.mu.Unlock()
	}
	a.requestRender()
}

func (a *App) moveModelPickerProvider(delta int) {
	a.modelPickerProviderCursor += delta
	a.clampModelPickerProviderCursor()
	a.modelPickerCursor = 0
	a.syncModelPickerThinking()
	a.requestRender()
}

func (a *App) moveModelPickerModel(delta int) {
	a.modelPickerCursor += delta
	a.clampModelPickerCursor()
	a.syncModelPickerThinking()
	a.requestRender()
}

func (a *App) handleModelPickerKey(k parsedKey) bool {
	switch k.action {
	case keyLeft:
		if a.modelPickerFocus == modelPickerFocusThinking {
			a.moveModelPickerThinking(-1)
		} else {
			a.modelPickerFocus = modelPickerFocusProvider
			a.requestRender()
		}
		return true
	case keyRight:
		if a.modelPickerFocus == modelPickerFocusThinking {
			a.moveModelPickerThinking(1)
		} else {
			a.modelPickerFocus = modelPickerFocusModel
			a.requestRender()
		}
		return true
	case keyUp:
		if a.modelPickerFocus == modelPickerFocusProvider {
			a.moveModelPickerProvider(-1)
		} else {
			a.moveModelPickerModel(-1)
		}
		return true
	case keyDown:
		if a.modelPickerFocus == modelPickerFocusProvider {
			a.moveModelPickerProvider(1)
		} else {
			a.moveModelPickerModel(1)
		}
		return true
	case keyRune:
		if k.r == 'k' || k.r == 'K' {
			if a.modelPickerFocus == modelPickerFocusProvider {
				a.moveModelPickerProvider(-1)
			} else {
				a.moveModelPickerModel(-1)
			}
			return true
		}
		if k.r == 'j' || k.r == 'J' {
			if a.modelPickerFocus == modelPickerFocusProvider {
				a.moveModelPickerProvider(1)
			} else {
				a.moveModelPickerModel(1)
			}
			return true
		}
		if k.r == 'h' || k.r == 'H' {
			if a.modelPickerFocus == modelPickerFocusThinking {
				a.moveModelPickerThinking(-1)
			} else {
				a.modelPickerFocus = modelPickerFocusProvider
				a.requestRender()
			}
			return true
		}
		if k.r == 'l' || k.r == 'L' {
			if a.modelPickerFocus == modelPickerFocusThinking {
				a.moveModelPickerThinking(1)
			} else {
				a.modelPickerFocus = modelPickerFocusModel
				a.requestRender()
			}
			return true
		}
	case keyTab:
		if a.modelPickerFocus == modelPickerFocusThinking {
			a.cycleModelPickerThinking()
		} else {
			a.cycleModelPickerFocus()
		}
		a.requestRender()
		return true
	case keyEnter:
		a.applyModelSelection()
		return true
	}
	return false
}

func (a *App) renderModelPicker(width int) string {
	if a.mode != modeModelPicker {
		return ""
	}

	a.clampModelPickerProviderCursor()
	a.clampModelPickerCursor()

	cfg, _ := config.Load()
	currentProvider := cfg.Provider
	if currentProvider == "" {
		currentProvider = config.ProviderOpenCode
	}
	currentModel := cfg.Model

	providers := opencode.ModelProviders()
	items := a.filteredModelPickerItems()

	leftWidth := 22
	if width > 80 {
		leftWidth = 24
	}
	if width < 56 {
		leftWidth = 18
	}
	sep := a.styles.SlashDim.Render(" │ ")

	var leftLines, rightLines []string
	leftLines = append(leftLines, a.styles.SlashName.Render("Provider"))
	rightLines = append(rightLines, a.styles.SlashName.Render("Model"))

	for i, p := range providers {
		marker := "  "
		if p.ID == currentProvider {
			marker = "* "
		}
		if i == a.modelPickerProviderCursor {
			if p.ID == currentProvider {
				marker = "›*"
			} else {
				marker = "› "
			}
		}

		name := p.Name
		if !a.providerConnected(p.ID) {
			name += " ○"
		}

		style := a.styles.SlashDim
		if a.modelPickerFocus == modelPickerFocusProvider && i == a.modelPickerProviderCursor {
			style = a.styles.SlashSelected
		} else if p.ID == currentProvider {
			style = a.styles.SlashName
		}
		leftLines = append(leftLines, style.Render(fmt.Sprintf("%s%-18s", marker, truncateRunes(name, 18))))
	}

	if fetchErr := a.modelRegistry.Err(); fetchErr != nil && a.modelPickerProvider().ID == opencode.ProviderOpenCode && len(items) == 0 {
		rightLines = append(rightLines, a.styles.SlashDim.Render("  could not fetch — catalog"))
	} else if len(items) == 0 {
		rightLines = append(rightLines, a.styles.SlashDim.Render("  no matching models"))
	} else {
		for i, m := range items {
			marker := "  "
			if m.ID == currentModel && a.modelPickerProvider().ID == currentProvider {
				marker = "* "
			}
			if i == a.modelPickerCursor {
				if marker == "* " {
					marker = "›*"
				} else {
					marker = "› "
				}
			}

			thinking := opencode.ThinkingOff
			if opencode.SupportsThinking(m.ID) && i == a.modelPickerCursor {
				thinking = a.modelPickerThinking
			} else if m.ID == currentModel && a.modelPickerProvider().ID == currentProvider {
				thinking = opencode.ParseThinkingLevel(cfg.ThinkingLevel)
			}

			meta := opencode.FormatContextWindow(m.ContextWindow)
			if badge := opencode.FormatThinkingBadge(m, thinking); badge != "" {
				meta += " · " + badge
			}

			label := truncateRunes(m.Name, 18)
			line := fmt.Sprintf("%s%-18s %s", marker, label, meta)

			style := a.styles.SlashDim
			if a.modelPickerFocus == modelPickerFocusModel && i == a.modelPickerCursor {
				style = a.styles.SlashSelected
			}
			rightLines = append(rightLines, style.Render(line))
		}
	}

	maxRows := len(leftLines)
	if len(rightLines) > maxRows {
		maxRows = len(rightLines)
	}
	for len(leftLines) < maxRows {
		leftLines = append(leftLines, "")
	}
	for len(rightLines) < maxRows {
		rightLines = append(rightLines, "")
	}

	var bodyRows []string
	for i := 0; i < maxRows; i++ {
		left := lipgloss.NewStyle().Width(leftWidth).Render(leftLines[i])
		right := rightLines[i]
		bodyRows = append(bodyRows, left+sep+right)
	}

	title := "  /model"
	if a.modelPickerFilter != "" {
		title += fmt.Sprintf(" (filter: %s)", a.modelPickerFilter)
	}

	var hint string
	switch {
	case a.modelPickerStatus != "":
		hint = a.styles.AssistError.Render("  " + a.modelPickerStatus)
	default:
		hint = a.styles.SlashDim.Render("  ←→ column/thinking   ↑↓ pick   tab focus/cycle   enter apply   esc close   ○ = not connected")
	}

	body := a.styles.SlashSelected.Render(title) + "\n" +
		strings.Join(bodyRows, "\n")

	if m, ok := a.modelPickerCurrent(); ok && opencode.SupportsThinking(m.ID) {
		body += "\n" + a.renderModelPickerThinkingRow(m)
	}

	body += "\n" + hint

	return a.styles.SlashMenu.
		Width(width - 2).
		Render(body)
}

func (a *App) renderModelPickerThinkingRow(m opencode.ModelInfo) string {
	levels := opencode.SupportedThinkingLevels(m.ID)
	parts := make([]string, 0, len(levels))
	for _, level := range levels {
		label := opencode.FormatThinkingLevelForModel(m.ID, level)
		style := a.styles.SlashDim
		if a.modelPickerFocus == modelPickerFocusThinking && level == a.modelPickerThinking {
			style = a.styles.SlashSelected
			label = "› " + label
		} else if level == a.modelPickerThinking {
			style = a.styles.SlashName
		}
		parts = append(parts, style.Render(label))
	}
	row := "  Thinking  " + strings.Join(parts, a.styles.SlashDim.Render(" · "))
	if a.modelPickerFocus == modelPickerFocusThinking {
		row = a.styles.SlashSelected.Render("  Thinking") + " " + strings.Join(parts, a.styles.SlashDim.Render(" · "))
	}
	return row
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 1 {
		return string(runes[:max])
	}
	return string(runes[:max-1]) + "…"
}
