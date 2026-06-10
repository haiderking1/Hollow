package tui

type composerMode int

const (
	modeTask composerMode = iota
	modeConnect
	modeSessionPicker
)

const (
	taskPlaceholder    = "describe what you want done..."
	connectPlaceholder = "paste api key..."
)

type slashCommand struct {
	name string
	desc string
}

var slashCommands = []slashCommand{
	{name: "connect", desc: "link your OpenCode API key"},
	{name: "new", desc: "start a fresh session"},
	{name: "sessions", desc: "list saved sessions for this project"},
	{name: "resume", desc: "pick a session to resume"},
}
