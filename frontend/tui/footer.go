package tui

import (
	"fmt"
	"os"
	"strings"

	"github.com/enough/enough/backend/agent"
	"github.com/enough/enough/backend/config"
	"github.com/enough/enough/backend/opencode"
	"github.com/enough/enough/backend/session"
	"github.com/enough/enough/backend/skills"
	"github.com/enough/enough/frontend/tui/term"
)

func formatFooterTokens(count int) string {
	switch {
	case count < 1000:
		return fmt.Sprintf("%d", count)
	case count < 10000:
		return fmt.Sprintf("%.1fk", float64(count)/1000)
	case count < 1000000:
		return fmt.Sprintf("%dk", count/1000)
	case count < 10000000:
		return fmt.Sprintf("%.1fM", float64(count)/1000000)
	default:
		return fmt.Sprintf("%dM", count/1000000)
	}
}

func footerProviderLabel(endpoint string) string {
	endpoint = strings.ToLower(endpoint)
	switch {
	case strings.Contains(endpoint, "/zen/go"):
		return "opencode-go"
	case strings.Contains(endpoint, "opencode.ai"):
		return "opencode"
	default:
		return "enough"
	}
}

func footerPWD(cwd string) string {
	if cwd == "" {
		return "~"
	}
	home, err := os.UserHomeDir()
	if err == nil && home != "" && strings.HasPrefix(cwd, home) {
		return "~" + strings.TrimPrefix(cwd, home)
	}
	return cwd
}

func (a *App) renderFooter(width int) []string {
	if a.session == nil || width <= 0 {
		return nil
	}

	cfg, err := config.Load()
	autoCompact := true
	model := config.DefaultModel
	endpoint := config.DefaultEndpoint
	thinking := string(a.thinkingLevel)
	if err == nil {
		if cfg.Compaction != nil {
			autoCompact = cfg.Compaction.Enabled
		}
		if cfg.Model != "" {
			model = cfg.Model
		}
		if cfg.Endpoint != "" {
			endpoint = cfg.Endpoint
		}
		if cfg.ThinkingLevel != "" {
			thinking = cfg.ThinkingLevel
		}
	}

	skillsEnabled := false
	if err == nil {
		skillsEnabled = cfg.Skills.Enabled
	}
	runCfg, runErr := config.LoadRuntime()
	if runErr == nil {
		if runCfg.Model != "" {
			model = runCfg.Model
		}
		if runCfg.Endpoint != "" {
			endpoint = runCfg.Endpoint
		}
		if runCfg.ThinkingLevel != "" {
			thinking = runCfg.ThinkingLevel
		}
		skillsEnabled = runCfg.Skills.Enabled
	}

	contextWindow := agent.ModelContextWindow(model, 0)
	if err == nil && cfg.Compaction != nil && cfg.Compaction.ContextWindow > 0 {
		contextWindow = agent.ModelContextWindow(model, cfg.Compaction.ContextWindow)
	}

	sessionMsgs := a.session.BuildSessionContext().Messages
	tokens := session.EstimateContextTokens(sessionMsgs).Tokens
	percentValue := 0.0
	if contextWindow > 0 {
		percentValue = float64(tokens) * 100 / float64(contextWindow)
	}

	autoTag := ""
	if autoCompact {
		autoTag = " (auto)"
	}
	contextPart := fmt.Sprintf("%.1f%%/%s%s", percentValue, formatFooterTokens(contextWindow), autoTag)
	if contextWindow <= 0 {
		contextPart = fmt.Sprintf("?/%s%s", formatFooterTokens(contextWindow), autoTag)
	}

	var statsLeft string
	switch {
	case percentValue > 90:
		statsLeft = a.styles.FooterErr.Render(contextPart)
	case percentValue > 70:
		statsLeft = a.styles.FooterWarn.Render(contextPart)
	default:
		statsLeft = a.styles.LogDim.Render(contextPart)
	}

	rightSide := model
	if opencode.SupportsThinking(model) {
		if thinking == "" || thinking == "off" {
			rightSide = model + " • thinking off"
		} else {
			rightSide = model + " • " + thinking
		}
	}
	rightSide = fmt.Sprintf("(%s) %s", footerProviderLabel(endpoint), rightSide)

	if a.evidenceCount > 0 {
		statsLeft += a.styles.LogDim.Render(fmt.Sprintf(" · ev %d", a.evidenceCount))
	}

	if skillsEnabled {
		discovered, _ := skills.DiscoverAllSkills(a.session.CWD(), runCfg)
		statsLeft += a.styles.LogDim.Render(fmt.Sprintf(" · skills %d", len(discovered)))
	}

	statsLine := footerJoin(width, statsLeft, a.styles.LogDim.Render(rightSide))

	pwd := footerPWD(a.session.CWD())
	pwdLine := a.styles.LogDim.Render(term.TruncateWidth(pwd, width))

	lines := a.renderObligations(width)
	return append(lines, pwdLine, statsLine)
}

func footerJoin(width int, left, right string) string {
	const minPad = 2
	leftW := term.VisibleWidth(left)
	rightW := term.VisibleWidth(right)
	if leftW+minPad+rightW <= width {
		return left + strings.Repeat(" ", width-leftW-rightW) + right
	}
	avail := width - leftW - minPad
	if avail <= 0 {
		return term.TruncateWidth(left, width)
	}
	return left + strings.Repeat(" ", minPad) + term.TruncateWidth(right, avail)
}
