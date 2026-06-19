package tui

type composerMode int

const (
	modeTask composerMode = iota
	modeConnect
	modeConnectPicker
	modeConnectCodex
	modeSessionPicker
	modeTreePicker
	modeModelPicker
	modePluginsPicker
	modePluginsSecret
	modeWriteApproval
)

type slashCommand struct {
	name string
	desc string
}

var slashCommands = []slashCommand{
	{name: "connect", desc: "link OpenCode Go/Zen API key or OpenAI Codex OAuth"},
	{name: "model", desc: "pick provider and model"},
	{name: "plugins", desc: "browse and install MCP servers, skills, and more"},
	{name: "new", desc: "start a fresh session"},
	{name: "sessions", desc: "list saved sessions for this project"},
	{name: "resume", desc: "pick a session to resume"},
	{name: "compact", desc: "manually compact conversation context"},
	{name: "auto-compact", desc: "toggle auto-compaction (on|off)"},
	{name: "tree", desc: "navigate to earlier branch point in active session"},
	{name: "skills", desc: "list discovered procedural skills"},
	{name: "skills-toggle", desc: "enable or disable the skills system (on|off)"},
	{name: "skill-commands", desc: "toggle /skill:<name> autocomplete (on|off)"},
	{name: "skill-archive", desc: "move a global skill to ~/.enough/skills/.archive/"},
	{name: "skill-restore", desc: "restore an archived global skill"},
	{name: "memory", desc: "show persistent memory (MEMORY.md + USER.md)"},
	{name: "curator-run", desc: "run the skill curator now (add dry-run to preview)"},
	{name: "curator-status", desc: "show curator state and last run summary"},
	{name: "curator-pin", desc: "pin a skill so the curator never archives it"},
	{name: "curator-unpin", desc: "unpin a skill"},
	{name: "curator-pause", desc: "pause or resume the curator (on|off)"},
	{name: "reload-skills", desc: "rescan procedural skills and reload changes"},
}
