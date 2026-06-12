---
name: enough
description: Commands, config paths, slash commands, skills hub, memory, and evidence settings for Enough.
version: 2.0.0
author: Enough
license: MIT
platforms: [linux, darwin, windows]
disable-model-invocation: true
metadata:
  hermes:
    tags: [enough, setup, configuration, cli, skills, memory]
    related_skills: [claude-code, codex, opencode]
---

# Enough Agent Reference

Use this skill whenever the user asks to configure, install, extend, or troubleshoot **Enough** itself.

## Home & Paths

All state lives under `~/.enough/` (override with `ENOUGH_HOME`):

| Path | Purpose |
|------|---------|
| `~/.enough/config.json` | Models, skills, memory, curator, evidence |
| `~/.enough/.env` | API keys / secrets for skill scripts |
| `~/.enough/skills/` | Global skill library (sync + hub installs) |
| `~/.enough/skills/.hub/` | Hub lock file, quarantine, audit log |
| `~/.enough/skills/.archive/` | Curator / manual archives |
| `~/.enough/skills/.usage.json` | Skill usage telemetry |
| `~/.enough/.skills_prompt_snapshot.json` | Prompt index disk cache |
| `~/.enough/pending/skills/` | Staged skill writes (write-approval gate) |
| `~/.enough/SOUL.md` | Agent identity |
| `~/.enough/MEMORY.md` / `USER.md` | Persistent memory |
| `~/.enough/agent/sessions/` | Session JSONL history |

Project-local skills: `.enough/skills/`, `.agents/skills/`, `.cursor/skills/` (see discovery order in code).

## CLI

```bash
enough                          # Interactive TUI (default)
enough -q "summarize this repo" # Single query, stdout = answer, stderr = thinking
enough --skills enough -q "…"  # Preload skills for one shot
enough skills sync              # Seed/update bundled skills from embed
enough skills list              # Installed skills
enough skills search "review"   # Hub search
enough skills install ID -y     # Hub install
enough skills configure         # Enable/disable skills
```

Agent tools (not shell commands): `skills_list`, `skill_view`, `skill_manage`, `bash`, file tools, `web_search`, `agent_swarm`.

## TUI Slash Commands

| Command | Action |
|---------|--------|
| `/connect` | Store OpenCode API key |
| `/model` | Pick model + thinking level |
| `/new` | Fresh session |
| `/sessions`, `/resume` | Session picker |
| `/compact`, `/auto-compact` | Context compaction |
| `/tree` | Branch navigation |
| `/skills` | List skills (+ hub / approval subcommands when enabled) |
| `/skills-toggle`, `/skill-commands` | Skills system toggles |
| `/skill:<name>` | Invoke a skill |
| `/reload-skills` | Rescan skill dirs |
| `/skill-archive`, `/skill-restore` | Manual archive |
| `/memory` | MEMORY.md / USER.md + pending writes |
| `/curator-run`, `/curator-status`, `/curator-pin`, … | Skill curator |

## Config (`config.json`)

Key blocks:
- `endpoint`, `model`, `thinking_level`, `hide_thinking`
- `skills.enabled`, `skills.write_approval`, `skills.guard_agent_created`, `skills.external_dirs`, `skills.disabled`
- `memory.*` — MEMORY/USER, background review intervals
- `curator.*` — stale/archive LLM curator
- `evidence.*` — proof obligation runtime
- `agent.coding_context` — `auto` \| `focus` \| `on` \| `off` (compact non-coding skills in index when `focus`)

## Skills Workflow

1. **Bundled** — shipped inside the binary; `enough skills sync` copies to `~/.enough/skills/`
2. **Hub** — `enough skills install <id>` from skills.sh / GitHub / etc.
3. **Agent-created** — `skill_manage(action='create')` → `~/.enough/skills/`
4. **Load** — `skill_view(name)` or `/skill:<name>`

Python/bash scripts under a skill dir are **not linked into Go** — the agent runs them via `bash` when the skill says so. Requires system `python3` / deps on the user's machine.

## Not in Enough (Hermes-only — do not invent)

Gateway (Telegram, Discord, …), profiles, plugins runtime, Honcho/mem0, kanban dispatcher, `hermes` CLI, Docker/Modal/SSH sandbox backends. Skills tagged `environments: [kanban]` are hidden unless that env exists.

## Deprecated alias

The bundled skill **`hermes-agent`** is a stub — always prefer **`enough`** (this skill).
