package tui

import (
	"os"

	"github.com/enough/enough/backend/web"
	"github.com/enough/enough/frontend/tui/markdown"
	"github.com/enough/enough/frontend/tui/term"
)

func Run() error {
	return RunWithPreloads(nil)
}

func RunWithPreloads(preloads []string) error {
	defer web.Stop()

	t, err := term.New()
	if err != nil {
		return err
	}

	markdown.InitTerminalCapabilities(int(os.Stdin.Fd()))

	app := newApp(t)
	app.preloadedSkills = preloads
	return app.run()
}
