# Enough

Enough is a terminal coding agent for inspecting, editing, testing, and iterating on software projects with native tools and parallel subagents. It keeps a persistent session transcript, uses the configured OpenCode-compatible model endpoint, and exposes coding tools directly to the model so changes can be made and verified in the workspace.

## Build

```sh
make build
```

The binary is written to `bin/enough`.

## Windows

Enough on Windows is a native `.exe` TUI using Git Bash for shell commands (same model as other coding agents — not WSL, not PowerShell).

### First-time setup

1. Open PowerShell and provision Git Bash (PortableGit, ~45 MB, no admin):

   ```powershell
   irm https://raw.githubusercontent.com/haiderking1/Hollow/main/scripts/install-windows.ps1 | iex
   ```

2. **Open a new terminal** so `ENOUGH_GIT_BASH_PATH` and PATH updates apply.

3. Build Enough:

   ```powershell
   git clone https://github.com/haiderking1/Hollow.git
   cd Hollow
   go build -o bin/enough.exe ./cmd/enough
   .\bin\enough.exe
   ```

### Overrides

- `ENOUGH_GIT_BASH_PATH` — point at any `bash.exe`
- `shell_path` in `~/.enough/config.json` — overrides env (highest priority)

### Recovery

Delete `%LOCALAPPDATA%\enough\git\` and re-run the install script.

```sh
make install
```

Installs the built binary according to the project `Makefile`.

## Configuration

Enough stores config and runtime data under `~/.enough/` (override with `ENOUGH_HOME`):

```text
~/.enough/config.json          # settings
~/.enough/skills/              # global skills library
~/.enough/agent/sessions/      # session transcripts
```

On first run, Enough migrates an existing `~/.config/enough/config.json` into `~/.enough/config.json`.

Defaults live in `backend/config/config.go`:

- endpoint: `https://opencode.ai/zen/go/v1`
- model: `deepseek-v4-flash`

The API key is stored via the secrets backend, not written to `config.json`.

## Skills

Skills are reusable procedural instructions (Markdown with YAML frontmatter). Enough discovers them from:

- `~/.enough/skills/` — global library (`skill_manage` writes here)
- `{project}/.enough/skills/` — project skills (win name collisions)
- `~/.cursor/skills/` and `{project}/.cursor/skills/` — Cursor-compatible paths

Use `/skills` in the TUI to list discovered skills, `/skill:<name> <args>` to run one, and `/skill-archive <name>` or `/skill-restore <name>` to manage the global library. See `backend/skills/enough_skill/SKILL.md` for the full slash-command reference.

## Tools

Enough exposes native coding tools including:

- `read_file`: reads a file and reports the full line count in the header, even when output is truncated.
- `write_file`: writes a complete file.
- `edit_file`: replaces text in an existing file.
- `list_dir`, `glob`, `grep`: inspect the workspace.
- `bash`: runs shell commands in the workspace.
- `web_search`: searches current web content.
- `agent_swarm`: runs parallel subagents.

### agent_swarm

`agent_swarm` accepts either a `tasks` array or a `goal`.

Each task has:

- `id`: optional display label.
- `prompt`: the complete self-contained worker instruction.
- `depends_on`: optional task ids from the same call. A dependent task starts after those tasks finish and receives their outputs.

Useful options:

- `shared_context`: prepended to every worker prompt.
- `max_concurrency`: number of workers to run at once. Default: `16`.
- `retry`: worker retry count for stream/API errors or empty output. Default: `3`.
- `max_turns_per_agent`: optional cap; workers otherwise run to completion.
- `isolate: "worktree"`: runs each worker in a separate git worktree/branch. Clean worktrees are removed; dirty worktrees are reported and left for review.

Nested swarms are available up to depth `3`. Worktree-isolated workers disable nested `agent_swarm` so nested edits cannot escape the isolated worktree. Without worktree isolation, a swarm call rejects tasks that appear to target the same path; split the work or use `isolate: "worktree"`.

## Dynamic workflows

`/workflow <task>` asks the main agent to write a task-specific JavaScript orchestration program under `.enough/workflows/<id>/workflow.js`. Enough validates and shows the script before execution; use `--yes` to run immediately.

The sandboxed workflow SDK provides `spawnAgent`, `pipeline`, `runBash`, `fetchJSON`, `today`, and `log`. `pipeline()` runs role-specific subjobs through a dynamic 16-slot pool, validates optional JSON Schemas, and lets later stages route from prior structured results. Workflows run in the background, so the normal composer remains available.

Commands:

- `/workflows` — list runs and open phase/agent details.
- `/workflow-cancel` — stop the active workflow.
- `/workflow-resume [id]` — resume a paused checkpoint without rerunning completed subjob keys.
- `/workflow-save <name>` — save the last script as a project slash command.
- `/effort ultracode on|off` — enable natural-language workflow triggering.

Saved project workflows live under `.enough/workflows/saved/`; personal workflows live under `~/.enough/workflows/saved/`. Project names win collisions. A complete reference is in `examples/workflows/open-pr-audit/workflow.js`.

Workflow parallelism is intentionally not cost-throttled. The default ceiling is 16 concurrent agents and the 1000-agent total is a runaway-loop fuse. Provider quota errors checkpoint and pause the run; they do not trigger preemptive slowdown.

## Alternate-screen TUI

Use `/tui alt-screen on` or launch with `ENOUGH_ALT_SCREEN=1` (`ENOUGH_NO_FLICKER=1` is an alias). Alternate-screen mode prevents pre-launch shell history from appearing in Enough scrollback and restores the previous terminal display on exit. See `docs/terminal.md` for tmux-over-SSH copy-mode setup.

## MCP Support

Enough has native support for Model Context Protocol (MCP) clients. Configured servers are dynamically registered as first-class agent tools.

### Configuration

Add `mcp_servers` to your `~/.enough/config.json`:

```json
{
  "mcp_servers": {
    "qmd": {
      "command": "qmd",
      "args": ["mcp"],
      "env": { "API_KEY": "your-key" },
      "cwd": "/optional/workdir",
      "enabled": true,
      "timeout": 30,
      "connect_timeout": 45,
      "tools": {
        "include": ["search", "get"],
        "exclude": ["dangerous_tool"]
      }
    },
    "another-remote": {
      "url": "http://localhost:8181/mcp",
      "headers": { "Authorization": "Bearer token" },
      "enabled": true
    }
  }
}
```

### Slash Commands in TUI

- `/plugins` — Browse and install integrations (Exa, Context7, more coming).
- `/mcp list` — Lists configured MCP servers, their connection status, and tool counts.
- `/mcp status` — Shows detailed health status and lists exposed tools for each server.
- `/mcp reload` — Reloads the MCP configuration and reconnects to servers.

### CLI Commands

- `enough add mcp <name>` — Interactive wizard; writes the server to `~/.enough/config.json`.
- `enough remove mcp <name>` — Removes a server from config (same as deleting it manually).
- `enough mcp list` — Lists configured servers.
- `enough mcp test <server-name>` — Connects to a server and prints its available tools.
- `enough mcp call <server.tool> '<json-args>'` — Directly calls an MCP tool and prints its result.

## Known Limits

- Swarm workers do not inherit MCP tools. MCP tools are available only to the main agent.
- Worktree isolation requires a git repository. Outside git, `isolate: "worktree"` falls back to the shared working directory.
- `agent_swarm` does not implement future Flame extras such as consensus, verifier, blackboard, quorum, or transcript saving.
- Parallel editing of the same file in one non-isolated swarm call is blocked rather than merged automatically.

## Tests

```sh
go test -race ./...
```
