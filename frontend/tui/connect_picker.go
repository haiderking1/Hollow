package tui

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/enough/enough/backend/auth"
	"github.com/enough/enough/backend/config"
)

type connectOption struct {
	id   string
	name string
	desc string
}

var connectOptions = []connectOption{
	{id: config.ProviderOpenCode, name: "OpenCode Go", desc: "paste API key"},
	{id: config.ProviderCodex, name: "OpenAI Codex", desc: "browser OAuth (ChatGPT subscription)"},
}

func (a *App) openConnectPicker() {
	a.mode = modeConnectPicker
	a.connectPickerCursor = 0
	a.connectPickerStatus = ""
	a.requestRender()
}

func (a *App) renderConnectPicker(width int) string {
	if a.mode != modeConnectPicker {
		return ""
	}
	var lines []string
	lines = append(lines, a.styles.SlashSelected.Render("  connect — pick provider"))
	for i, opt := range connectOptions {
		marker := "  "
		if i == a.connectPickerCursor {
			marker = "› "
		}
		name := a.styles.SlashName.Render(opt.name)
		desc := a.styles.SlashDesc.Render("  " + opt.desc)
		lines = append(lines, fmt.Sprintf("%s%s%s", marker, name, desc))
	}
	if a.connectPickerStatus != "" {
		lines = append(lines, a.styles.SlashDim.Render("  "+a.connectPickerStatus))
	}
	return lipgloss.NewStyle().Width(width).Render(strings.Join(lines, "\n"))
}

func (a *App) handleConnectPickerKey(k parsedKey) bool {
	switch k.action {
	case keyUp:
		if a.connectPickerCursor > 0 {
			a.connectPickerCursor--
		}
	case keyDown:
		if a.connectPickerCursor < len(connectOptions)-1 {
			a.connectPickerCursor++
		}
	case keyEnter:
		a.selectConnectOption(connectOptions[a.connectPickerCursor].id)
	case keyEscape:
		a.cancelConnect()
	default:
		return false
	}
	a.requestRender()
	return true
}

func (a *App) selectConnectOption(provider string) {
	switch provider {
	case config.ProviderOpenCode:
		a.mode = modeConnect
		a.editor = NewEditor(1024)
		_, endpoint, model, err := config.ConnectionSettings()
		if err != nil {
			a.appendMessage("error", err.Error())
			a.mode = modeTask
			a.editor = NewEditor(512)
			return
		}
		a.appendMessage("system", fmt.Sprintf("connect — OpenCode · %s · %s\npaste your api key below", endpoint, model))
	case config.ProviderCodex:
		a.startCodexOAuth()
	default:
		a.appendMessage("error", "unknown provider")
		a.mode = modeTask
		a.editor = NewEditor(512)
	}
}

func (a *App) startCodexOAuth() {
	if a.codexOAuthCancel != nil {
		a.codexOAuthCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.codexOAuthCancel = cancel
	a.mode = modeConnectCodex
	a.connectPickerStatus = "starting OAuth..."
	a.requestRender()

	go func() {
		defer func() {
			a.mu.Lock()
			a.codexOAuthCancel = nil
			a.mu.Unlock()
		}()

		start, err := auth.StartCodexDeviceAuth(ctx)
		if err != nil {
			a.appendMessage("error", err.Error())
			a.mode = modeTask
			a.editor = NewEditor(512)
			a.requestRender()
			return
		}

		a.appendMessage("system", fmt.Sprintf(
			"OpenAI Codex sign-in\n\n1. Open in your browser:\n   %s\n\n2. Enter this code:\n   %s\n\nWaiting for sign-in... (esc to cancel)",
			start.VerifyURL, start.UserCode,
		))
		_ = openBrowser(start.VerifyURL)
		a.connectPickerStatus = "waiting for browser sign-in..."
		a.requestRender()

		if err := auth.PollCodexDeviceAuth(ctx, start); err != nil {
			if ctx.Err() != nil {
				return
			}
			a.appendMessage("error", err.Error())
			a.mode = modeTask
			a.editor = NewEditor(512)
			a.requestRender()
			return
		}

		if err := config.EnableCodexProvider(); err != nil {
			a.appendMessage("error", err.Error())
			a.mode = modeTask
			a.editor = NewEditor(512)
			a.requestRender()
			return
		}

		a.appendMessage("assistant", "Done — connected via OpenAI Codex OAuth.")
		a.mode = modeTask
		a.editor = NewEditor(512)
		if a.agent != nil {
			_ = a.agent.Reset()
			a.agent = nil
		}
		if a.session != nil {
			_ = a.session.NewSession()
			a.messages = nil
			a.bumpChat()
		}
		a.requestRender()
	}()
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return fmt.Errorf("unsupported platform")
	}
	return cmd.Start()
}
