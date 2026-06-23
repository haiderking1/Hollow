---
name: hollow-agent
description: Configure, extend, or troubleshoot Hollow. Paths, CLI, SOUL.md, memory, skills, curator, write approval.
version: 2.1.0
author: Hollow
license: MIT
platforms: [linux, darwin, windows]
metadata:
  hermes:
    tags: [hollow, setup, configuration, cli, skills, memory, soul]
    related_skills: [claude-code, codex, opencode]
---

# Hollow Agent (`hollow-agent` skill)

Operator manual for **Hollow** itself — load with `skill_view(name="hollow-agent")` before editing SOUL.md, memory, config, skills, or curator settings.

**When to load:** rename/re-persona, memory/skills setup, curator, write approval, or any "how does Hollow work?" question. Do not guess paths or invent CLI flags.

## Home & Paths

All state lives under `~/.hollow/` (override with `HOLLOW_HOME`):

| Path | Purpose |
|------|---------|
| `$HOME/.hollow/config.json` | Models, skills, memory, curator, evidence |
| `$HOME/.hollow/.env` | API keys / secrets for skill scripts |
| `$HOME/.hollow/skills/` | Global skill library (sync + hub installs) |
| `$HOME/.hollow/skills/.hub/` | Hub lock file, quarantine, audit log |
| `$HOME/.hollow/skills/.archive/` | Curator / manual archives |
| `$HOME/.hollow/skills/.usage.json` | Skill usage telemetry |
| `$HOME/.hollow/.skills_prompt_snapshot.json` | Prompt index disk cache |
| `$HOME/.hollow/pending/skills/` | Staged skill writes (write-approval gate) |
| `$HOME/.hollow/SOUL.md` | Agent identity (name, persona, tone — user-editable) |
| `$HOME/.hollow/memories/MEMORY.md` | Agent notes (persistent memory) |
| `$HOME/.hollow/memories/USER.md` | User profile memory |
| `$HOME/.hollow/pending/memory/` | Staged memory writes (write-approval gate) |
| `$HOME/.hollow/agent/sessions/` | Session JSONL history |

Project-local skills: `.hollow/skills/`, `.agents/skills/`, `.cursor/skills/`.

**Path rule:** resolve `$HOME` or `$HOLLOW_HOME` before `read_file` / `write_file`. **Never** pass a literal `~` prefix — tools may not expand it.

## SOUL.md (identity)

`SOUL.md` is injected as the **first block** of the system prompt every session.

**Rename / re-persona workflow:**

1. Load this skill if you have not already.
2. Run `bash` with `echo $HOME` if you need the home directory.
3. `read_file` on `$HOME/.hollow/SOUL.md` (full file).
4. `edit_file`: update `# SOUL.md — …` title and the `You are …` line only, unless the user wants more.
5. Tell the user to `/new` — identity refreshes on the next session.

## Memory tool

Use the native **`memory`** tool (not prose promises) to save durable facts:

- `target: user` — name, preferences, communication style
- `target: memory` — environment facts, project conventions

When `memory.write_approval` is on, writes stage to `~/.hollow/pending/memory/` — user approves in TUI (`y`/`n`) or `/memory approve <id>`.

**Never** tell the user you remembered something unless `memory` returned success this turn (or a staged pending id).

**Profile corrections:** USER PROFILE in the system prompt is a frozen snapshot — it can be wrong or ambiguous. When the user corrects how you addressed them or clarifies profile text (e.g. full name `haider` vs nickname `h`), call `memory` in the **same turn**:

```json
{"action":"replace","target":"user","match":"haider","replacement":"User's name is haider — use the full name (lowercase spelling); never shorten to \"h\"."}
```

Apologizing without `memory` leaves the bad entry on disk for the next session.

## CLI

```bash
enough                              # Interactive TUI (default)
enough -q "summarize this repo"     # Single query
enough --skills hollow-agent -q "…" # Preload this skill for one shot
hollow skills sync                  # Seed/update bundled skills from embed
hollow skills list                  # Installed skills
enough skills search "review"       # Hub search
enough skills install ID -y         # Hub install
enough skills configure             # Enable/disable skills
enough curator status               # Curator scheduler + agent-created skill stats
enough curator run [dry-run]        # Run curator now
enough curator pin|unpin <skill>
enough curator restore <skill>      # From ~/.hollow/skills/.archive/
```

Agent tools (not shell): `skills_list`, `skill_view`, `skill_manage`, `memory`, file tools, `bash`, `web_search`, `agent_swarm`.

## TUI Slash Commands

| Command | Action |
|---------|--------|
| `/connect` | Store OpenCode API key |
| `/model` | Pick model + thinking level |
| `/new` | Fresh session (required after SOUL.md edit) |
| `/sessions`, `/resume` | Session picker |
| `/compact`, `/auto-compact` | Context compaction |
| `/tree` | Branch navigation |
| `/memory` | Show MEMORY.md + USER.md |
| `/memory pending`, `/memory approve`, `/memory reject`, `/memory approval on\|off` | Memory write approval |
| `/skills pending`, `/skills diff`, `/skills approve`, `/skills approval on\|off` | Skill write approval |
| `/curator-run`, `/curator-status`, `/curator-pin`, `/curator-unpin`, `/curator-pause` | Skill curator |
| `/skills` | List skills |
| `/skill:<name>` | Invoke a skill |
| `/reload-skills` | Rescan skill dirs |
| `/skill-archive`, `/skill-restore` | Manual archive |

## Config (`config.json`)

Key blocks: `endpoint`, `model`, `thinking_level`, `hide_thinking`, `skills.*`, `memory.*`, `curator.*`, `evidence.*`, `agent.coding_context`.

## Self-improvement

### 1. Background review (after each turn)

Fork replays the conversation; tools limited to `memory`, `skills_list`, `skill_view`, `skill_manage`.

| Key | Default | Meaning |
|-----|---------|---------|
| `memory.nudge_interval` | 10 | User turns between memory review (0 = off) |
| `memory.skill_nudge_interval` | 10 | Tool iterations between skill review (0 = off) |

### 2. Curator

Deterministic stale/archive + LLM pass. Protected built-in: **`hollow-agent`**.

Triggers: idle tick, `/curator-run`, `hollow curator run`.

### 3. Write approval

When `skills.write_approval` or `memory.write_approval` is true, mutating calls stage to `~/.hollow/pending/{skills,memory}/`. TUI shows an approval overlay (`y` approve, `n` reject, `d` diff, `esc` later).

### 4. Bundled sync on startup

Quiet sync on launch (`EnsureBootstrapped`). Opt out: `~/.hollow/.no-bundled-skills`. Manual: `hollow skills sync`.

## Skills workflow

1. **Bundled** — embedded in binary; `hollow skills sync` → `~/.hollow/skills/`
2. **Hub** — `hollow skills install <id>`
3. **Agent-created** — `skill_manage(create)` → `~/.hollow/skills/`
4. **Load** — `skill_view(name)` or `/skill:<name>`

## Not in Hollow (do not invent)

Telegram/Discord/Slack gateways, `hermes` CLI, profiles, Honcho/mem0, kanban dispatcher, Docker/Modal/SSH sandbox backends. Skills tagged `environments: [kanban]` are hidden unless that env exists.
