package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/enough/enough/backend/config"
	"github.com/enough/enough/backend/mcp"
	"github.com/enough/enough/frontend/tui/term"
)

type pluginsTab int

const (
	pluginsTabMCP pluginsTab = iota
	pluginsTabSkills
	pluginsTabInstalled
)

type pluginsPickerFocus int

const (
	pluginsPickerFocusSearch pluginsPickerFocus = iota
	pluginsPickerFocusList
)

var pluginsTabLabels = []string{"MCP Servers", "Skills", "Installed"}

// searchIconGlyph is a single-width terminal search mark (not emoji).
const searchIconGlyph = "⌕"

func (s Styles) pluginsSearchBorderColor(focused bool) lipgloss.Color {
	if focused {
		return lipgloss.Color("#7c8cff")
	}
	return lipgloss.Color("#2a2a34")
}

// renderFixedInputBox draws a closed 3-row box. Content must be plain text with visible width <= innerW.
func renderFixedInputBox(innerW int, plainLine string, border lipgloss.Color) []string {
	if innerW < 8 {
		innerW = 8
	}
	vis := term.VisibleWidth(plainLine)
	if vis > innerW {
		plainLine = term.TruncateWidth(plainLine, innerW)
		vis = innerW
	}
	if vis < innerW {
		plainLine += strings.Repeat(" ", innerW-vis)
	}

	b := lipgloss.NewStyle().Foreground(border)
	rule := strings.Repeat("─", innerW+2)
	return []string{
		b.Render("╭" + rule + "╮"),
		b.Render("│") + " " + plainLine + " " + b.Render("│"),
		b.Render("╰" + rule + "╯"),
	}
}

func (a *App) openPluginsPicker() {
	a.mode = modePluginsPicker
	a.pluginsPickerTab = int(pluginsTabMCP)
	a.pluginsPickerCursor = 0
	a.pluginsPickerFocus = pluginsPickerFocusList
	a.pluginsPickerFilter = ""
	a.pluginsPickerStatus = ""
	a.pluginsPendingEntryID = ""
	a.requestRender()
}

func (a *App) dismissPluginsPicker() {
	a.mode = modeTask
	a.pluginsPickerFilter = ""
	a.pluginsPickerStatus = ""
	a.pluginsPendingEntryID = ""
	a.editor = NewEditor(512)
	a.editor.SetValue("")
	a.requestRender()
}

func (a *App) pluginsPickerActiveTab() pluginsTab {
	if a.pluginsPickerTab < 0 {
		return pluginsTabMCP
	}
	if a.pluginsPickerTab >= len(pluginsTabLabels) {
		return pluginsTabInstalled
	}
	return pluginsTab(a.pluginsPickerTab)
}

func (a *App) movePluginsPickerTab(delta int) {
	a.pluginsPickerTab += delta
	if a.pluginsPickerTab < 0 {
		a.pluginsPickerTab = 0
	}
	if a.pluginsPickerTab >= len(pluginsTabLabels) {
		a.pluginsPickerTab = len(pluginsTabLabels) - 1
	}
	a.pluginsPickerCursor = 0
	a.pluginsPickerFilter = ""
	a.requestRender()
}

func (a *App) cyclePluginsPickerFocus() {
	switch a.pluginsPickerFocus {
	case pluginsPickerFocusSearch:
		a.pluginsPickerFocus = pluginsPickerFocusList
	default:
		a.pluginsPickerFocus = pluginsPickerFocusSearch
	}
	a.requestRender()
}

func (a *App) pluginsSearchPlainLine(innerW int, query string, focused bool) string {
	icon := searchIconGlyph + " "
	var rest string
	switch {
	case query == "" && focused:
		rest = "▎"
	case query == "":
		rest = "Search..."
	default:
		rest = query
		if focused {
			rest += "▎"
		}
	}
	line := icon + rest
	if w := term.VisibleWidth(line); w > innerW {
		line = term.TruncateWidth(line, innerW)
	} else if w < innerW {
		line += strings.Repeat(" ", innerW-w)
	}
	return line
}

func (a *App) pluginsPickerCatalogForTab() []mcp.CatalogEntry {
	cfg, _ := config.Load()
	switch a.pluginsPickerActiveTab() {
	case pluginsTabMCP:
		var entries []mcp.CatalogEntry
		for _, entry := range mcp.Catalog() {
			if entry.Kind == mcp.CatalogKindMCP {
				entries = append(entries, entry)
			}
		}
		return entries
	case pluginsTabInstalled:
		var entries []mcp.CatalogEntry
		for _, entry := range mcp.Catalog() {
			if mcp.IsCatalogInstalled(cfg, entry) {
				entries = append(entries, entry)
			}
		}
		return entries
	default:
		return nil
	}
}

func (a *App) pluginsPickerVisibleEntries() []mcp.CatalogEntry {
	entries := a.pluginsPickerCatalogForTab()
	filter := strings.ToLower(strings.TrimSpace(a.pluginsPickerFilter))
	if filter == "" {
		return entries
	}
	out := make([]mcp.CatalogEntry, 0, len(entries))
	for _, entry := range entries {
		if strings.Contains(strings.ToLower(entry.Name), filter) ||
			strings.Contains(strings.ToLower(entry.Description), filter) {
			out = append(out, entry)
		}
	}
	return out
}

func (a *App) clampPluginsPickerCursor() {
	entries := a.pluginsPickerVisibleEntries()
	if len(entries) == 0 {
		a.pluginsPickerCursor = 0
		return
	}
	if a.pluginsPickerCursor >= len(entries) {
		a.pluginsPickerCursor = len(entries) - 1
	}
	if a.pluginsPickerCursor < 0 {
		a.pluginsPickerCursor = 0
	}
}

func (a *App) pluginsPickerCurrentEntry() (mcp.CatalogEntry, bool) {
	entries := a.pluginsPickerVisibleEntries()
	a.clampPluginsPickerCursor()
	if len(entries) == 0 {
		return mcp.CatalogEntry{}, false
	}
	return entries[a.pluginsPickerCursor], true
}

func (a *App) renderPluginsTabBar() string {
	accent := lipgloss.Color("#7c8cff")
	dark := lipgloss.Color("#0d0d0f")
	activeTab := lipgloss.NewStyle().Background(accent).Foreground(dark).Padding(0, 1).Bold(true)
	inactiveTab := a.styles.SlashDim

	title := a.styles.SlashSelected.Render("Plugins")
	var tabParts []string
	for i, name := range pluginsTabLabels {
		if i == a.pluginsPickerTab {
			tabParts = append(tabParts, activeTab.Render(name))
		} else {
			tabParts = append(tabParts, inactiveTab.Render(name))
		}
	}
	return "  " + title + "   " + strings.Join(tabParts, "   ")
}

func (a *App) renderPluginsSubheader(entries []mcp.CatalogEntry) string {
	total := len(entries)
	pos := 0
	if total > 0 {
		a.clampPluginsPickerCursor()
		pos = a.pluginsPickerCursor + 1
	}
	switch a.pluginsPickerActiveTab() {
	case pluginsTabMCP:
		return "  " + a.styles.SlashName.Render("Browse") +
			a.styles.SlashDim.Render(fmt.Sprintf(" (%d/%d)", pos, total))
	case pluginsTabInstalled:
		return "  " + a.styles.SlashName.Render("Installed") +
			a.styles.SlashDim.Render(fmt.Sprintf(" (%d/%d)", pos, total))
	default:
		return ""
	}
}

func (a *App) renderPluginsSearchLines(width int) []string {
	// Picker lines use a 2-char indent; box top is innerW+4 visible. Keep inside terminal width.
	innerW := width - 10
	if innerW < 20 {
		innerW = 20
	}

	query := a.pluginsPickerFilter
	focused := a.pluginsPickerFocus == pluginsPickerFocusSearch
	plain := a.pluginsSearchPlainLine(innerW, query, focused)

	border := a.styles.pluginsSearchBorderColor(focused)
	boxLines := renderFixedInputBox(innerW, plain, border)
	out := make([]string, len(boxLines))
	for i, line := range boxLines {
		out[i] = "  " + line
	}
	return out
}

func (a *App) renderPluginsPickerHint() string {
	switch a.pluginsPickerFocus {
	case pluginsPickerFocusSearch:
		return a.styles.SlashDim.Render("  ↓ enter tab list · esc close")
	default:
		return a.styles.SlashDim.Render("  ←→ tabs  ↑ search  ↓ pick  enter install  d remove  esc close")
	}
}

func (a *App) pluginsPickerFocusListFromSearch() {
	a.pluginsPickerFocus = pluginsPickerFocusList
	a.clampPluginsPickerCursor()
	a.requestRender()
}

func (a *App) pluginsPickerFocusSearchFromList() {
	a.pluginsPickerFocus = pluginsPickerFocusSearch
	a.requestRender()
}

func (a *App) renderPluginsEntryList(cfg config.Config, entries []mcp.CatalogEntry, emptyMsg string) []string {
	if len(entries) == 0 {
		if strings.TrimSpace(a.pluginsPickerFilter) != "" {
			return []string{a.styles.SlashDim.Render("  no matching plugins")}
		}
		return []string{a.styles.SlashDim.Render("  " + emptyMsg)}
	}
	var lines []string
	for i, entry := range entries {
		lines = append(lines, a.renderPluginsEntryLine(cfg, entry, i))
	}
	return lines
}

func (a *App) renderPluginsEntryLine(cfg config.Config, entry mcp.CatalogEntry, index int) string {
	selected := a.pluginsPickerFocus == pluginsPickerFocusList && index == a.pluginsPickerCursor
	marker := "  "
	if selected {
		marker = "› "
	}

	desc := truncateRunes(entry.Description, 44)
	installed := mcp.IsCatalogInstalled(cfg, entry)

	if selected {
		line := marker + entry.Name + " · " + desc
		if installed {
			line += " · installed"
		}
		return a.styles.SlashSelected.Render(line)
	}

	line := marker + a.styles.SlashName.Render(entry.Name) + a.styles.SlashDesc.Render(" · "+desc)
	if installed {
		line += a.styles.LogAccent.Render(" · installed")
	}
	return line
}

func (a *App) renderPluginsPicker(width int) string {
	if a.mode != modePluginsPicker {
		return ""
	}

	a.clampPluginsPickerCursor()
	cfg, _ := config.Load()
	entries := a.pluginsPickerVisibleEntries()

	var lines []string
	lines = append(lines, a.renderPluginsTabBar())

	switch a.pluginsPickerActiveTab() {
	case pluginsTabSkills:
		lines = append(lines, "")
		lines = append(lines, a.styles.SlashDim.Render("  coming soon"))
		lines = append(lines, a.styles.SlashDesc.Render("  agent skill packs will be installable here"))
	default:
		lines = append(lines, "")
		lines = append(lines, a.renderPluginsSubheader(entries))
		lines = append(lines, a.renderPluginsSearchLines(width)...)
		lines = append(lines, "")
		emptyMsg := "no mcp servers in catalog"
		if a.pluginsPickerActiveTab() == pluginsTabInstalled {
			emptyMsg = "nothing installed"
		}
		lines = append(lines, a.renderPluginsEntryList(cfg, entries, emptyMsg)...)
	}

	if a.pluginsPickerStatus != "" {
		lines = append(lines, "", a.styles.AssistError.Render("  "+a.pluginsPickerStatus))
	}
	lines = append(lines, "", a.renderPluginsPickerHint())

	body := strings.Join(lines, "\n")
	// Do not set Width here — lipgloss reflows/wraps inner lines and breaks hand-drawn box borders.
	return a.styles.SlashMenu.Render(body)
}

func (a *App) handlePluginsPickerKey(k parsedKey) bool {
	switch k.action {
	case keyLeft:
		if a.pluginsPickerFocus == pluginsPickerFocusList {
			a.movePluginsPickerTab(-1)
		}
		return true
	case keyRight:
		if a.pluginsPickerFocus == pluginsPickerFocusList {
			a.movePluginsPickerTab(1)
		}
		return true
	case keyUp:
		switch a.pluginsPickerFocus {
		case pluginsPickerFocusSearch:
			a.pluginsPickerFocusListFromSearch()
		case pluginsPickerFocusList:
			if a.pluginsPickerCursor > 0 {
				a.pluginsPickerCursor--
				a.requestRender()
			} else {
				a.pluginsPickerFocusSearchFromList()
			}
		}
		return true
	case keyDown:
		switch a.pluginsPickerFocus {
		case pluginsPickerFocusSearch:
			a.pluginsPickerFocusListFromSearch()
		case pluginsPickerFocusList:
			entries := a.pluginsPickerVisibleEntries()
			if a.pluginsPickerCursor < len(entries)-1 {
				a.pluginsPickerCursor++
				a.requestRender()
			}
		}
		return true
	case keyTab:
		a.cyclePluginsPickerFocus()
		return true
	case keyEnter:
		if a.pluginsPickerFocus == pluginsPickerFocusSearch {
			a.pluginsPickerFocusListFromSearch()
			return true
		}
		if a.pluginsPickerActiveTab() == pluginsTabMCP || a.pluginsPickerActiveTab() == pluginsTabInstalled {
			entry, ok := a.pluginsPickerCurrentEntry()
			if !ok {
				return true
			}
			cfg, _ := config.Load()
			if mcp.IsCatalogInstalled(cfg, entry) {
				a.pluginsPickerStatus = fmt.Sprintf("%s is installed — press d to remove", entry.Name)
				a.requestRender()
				return true
			}
			if a.pluginsPickerActiveTab() == pluginsTabInstalled {
				return true
			}
			a.activatePluginsEntry(entry)
		}
		a.requestRender()
		return true
	case keyEscape:
		a.dismissPluginsPicker()
		return true
	case keyBackspace:
		if a.pluginsPickerFocus == pluginsPickerFocusSearch && len(a.pluginsPickerFilter) > 0 {
			a.pluginsPickerFilter = a.pluginsPickerFilter[:len(a.pluginsPickerFilter)-1]
			a.pluginsPickerCursor = 0
			a.requestRender()
			return true
		}
		return false
	case keyRune:
		if a.pluginsPickerFocus == pluginsPickerFocusSearch {
			a.pluginsPickerFilter += string(k.r)
			a.pluginsPickerCursor = 0
			a.requestRender()
			return true
		}
		if k.r == 'h' || k.r == 'H' {
			a.movePluginsPickerTab(-1)
			a.pluginsPickerFocus = pluginsPickerFocusList
			return true
		}
		if k.r == 'l' || k.r == 'L' {
			a.movePluginsPickerTab(1)
			a.pluginsPickerFocus = pluginsPickerFocusList
			return true
		}
		if k.r == 'j' || k.r == 'J' {
			entries := a.pluginsPickerVisibleEntries()
			if a.pluginsPickerCursor < len(entries)-1 {
				a.pluginsPickerCursor++
				a.requestRender()
			}
			return true
		}
		if k.r == 'k' || k.r == 'K' {
			if a.pluginsPickerCursor > 0 {
				a.pluginsPickerCursor--
				a.requestRender()
			} else {
				a.pluginsPickerFocusSearchFromList()
			}
			return true
		}
		if k.r == 'd' || k.r == 'D' {
			entry, ok := a.pluginsPickerCurrentEntry()
			if !ok {
				return true
			}
			a.removePluginsEntry(entry)
			a.requestRender()
			return true
		}
		if a.pluginsPickerActiveTab() == pluginsTabSkills {
			return false
		}
		a.pluginsPickerFocus = pluginsPickerFocusSearch
		a.pluginsPickerFilter += string(k.r)
		a.pluginsPickerCursor = 0
		a.requestRender()
		return true
	default:
		return false
	}
}

func (a *App) activatePluginsEntry(entry mcp.CatalogEntry) {
	cfg, err := config.Load()
	if err != nil {
		a.pluginsPickerStatus = err.Error()
		return
	}
	if mcp.IsCatalogInstalled(cfg, entry) {
		a.pluginsPickerStatus = fmt.Sprintf("%s is already installed — press d to remove", entry.Name)
		return
	}
	if len(entry.Secrets) > 0 {
		a.pluginsPendingEntryID = entry.ID
		a.mode = modePluginsSecret
		a.editor = NewEditor(1024)
		a.editor.SetValue("")
		label := entry.Secrets[0].Label
		if entry.Secrets[0].Optional {
			label += " (optional — enter to skip)"
		}
		a.appendMessage("system", fmt.Sprintf("plugins — %s · paste %s below", entry.Name, label))
		return
	}
	a.installPluginsEntry(entry, nil)
}

func (a *App) removePluginsEntry(entry mcp.CatalogEntry) {
	cfg, err := config.Load()
	if err != nil {
		a.pluginsPickerStatus = err.Error()
		return
	}
	if !mcp.IsCatalogInstalled(cfg, entry) {
		a.pluginsPickerStatus = fmt.Sprintf("%s is not installed", entry.Name)
		return
	}
	if err := mcp.RemoveCatalogEntry(&cfg, entry); err != nil {
		a.pluginsPickerStatus = err.Error()
		return
	}
	if err := config.Save(cfg); err != nil {
		a.pluginsPickerStatus = err.Error()
		return
	}
	a.applyRuntimeConfig()
	a.pluginsPickerStatus = fmt.Sprintf("removed %s", entry.Name)
}

func (a *App) installPluginsEntry(entry mcp.CatalogEntry, secrets map[string]string) {
	cfg, err := config.Load()
	if err != nil {
		a.pluginsPickerStatus = err.Error()
		return
	}
	if err := mcp.InstallCatalogEntry(&cfg, entry, secrets); err != nil {
		a.pluginsPickerStatus = err.Error()
		return
	}
	if err := config.Save(cfg); err != nil {
		a.pluginsPickerStatus = err.Error()
		return
	}
	a.applyRuntimeConfig()
	a.mode = modePluginsPicker
	a.pluginsPickerStatus = fmt.Sprintf("installed %s — tools available as mcp_%s_*", entry.Name, entry.ServerName)
}

func (a *App) savePluginsSecret(raw string) {
	entryID := a.pluginsPendingEntryID
	entry, ok := mcp.CatalogEntryByID(entryID)
	if !ok {
		a.appendMessage("error", "unknown plugin install")
		a.mode = modeTask
		a.editor = NewEditor(512)
		return
	}
	secrets := map[string]string{}
	if len(entry.Secrets) > 0 {
		key := strings.TrimSpace(raw)
		if key != "" {
			secrets[entry.Secrets[0].Key] = key
		} else if !entry.Secrets[0].Optional {
			a.appendMessage("error", entry.Secrets[0].Label+" is required")
			return
		}
	}
	a.pluginsPendingEntryID = ""
	a.installPluginsEntry(entry, secrets)
	a.appendMessage("system", fmt.Sprintf("%s configured.", entry.Name))
}

func (a *App) applyRuntimeConfig() {
	runCfg, err := config.LoadRuntime()
	if err != nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.agent != nil {
		a.agent.UpdateConfig(runCfg)
	}
}
