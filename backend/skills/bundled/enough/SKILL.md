---
name: enough
description: Commands, config paths, slash commands, skills, and evidence runtime settings for Enough.
disable-model-invocation: true
platforms: [linux, darwin, windows]
---

# Enough Agent Reference

Use this skill to view paths, configuration settings, slash commands, and control structures for Enough.

## Path Conventions
All Enough settings and files reside under the home root `~/.enough/` (or overridden by the `ENOUGH_HOME` environment variable):
- Config file: `~/.enough/config.json`
- Global skills library: `~/.enough/skills/`
- Telemetry usage stats: `~/.enough/skills/.usage.json`
- Archived skills: `~/.enough/skills/.archive/`
- Legacy read-only skills: `~/.enough/agent/skills/`
- Prompt snapshot cache: `~/.enough/.skills_prompt_snapshot.json`
- Session database: `~/.enough/agent/sessions/`

## TUI Slash Commands
Enter these commands in the chat input area:
- `/connect <api_key>` or `/connect`: Link your OpenCode API key securely.
- `/new`: Reset the current conversation and start a fresh session.
- `/sessions`: Display the list of saved sessions for this project.
- `/resume`: Open the picker to select and load a saved session.
- `/compact <instructions>`: Manually compact history using optional guidance.
- `/auto-compact on|off`: Toggle automatic token-limit context compaction.
- `/tree`: Visualize branching session history and jump to any historical node.
- `/skills`: Print a categorized list of all discovered procedural skills.
- `/skills-toggle on|off`: Enable or disable the skills system.
- `/skill-commands on|off`: Toggle autocomplete menu entries for `/skill:<name>`.
- `/skill:<name> <args>`: Execute a specific skill, injecting its instructions as a synthetic user prompt.
- `/skill-archive <name>`: Move a global skill to `~/.enough/skills/.archive/`.
- `/skill-restore <name>`: Restore an archived global skill from `.archive/`.

## Skills Configuration (`config.json`)
The skills config block under `skills` supports:
- `enabled` (boolean): Global toggle for the skills system.
- `enable_skill_commands` (boolean): Toggles `/skill:<name>` autocomplete registration.
- `paths` (string array): Extra folders/files to scan for skills.
- `disabled` (string array): List of skill names to ignore.

Example:
```json
{
  "skills": {
    "enabled": true,
    "enable_skill_commands": true,
    "paths": ["/extra/skills", "~/custom-skills"],
    "disabled": ["unsafe-skill"]
  }
}
```

## Evidence Runtime Configuration (`config.json`)
Controls for the proof obligation and verification ledger runtime:
- `evidence.enabled` (boolean): Toggles the v2 evidence runtime and tool guard.
- `evidence.strict_verify_reset` (boolean): Strict verification resetting behavior.
- `evidence.verifier_enabled` (boolean): Toggles automated verification commands.
- `evidence.continuity_reads` (boolean): Seeds read credit on unchanged agent-authored files.

## Model and Thinking Settings (`config.json`)
- `endpoint` (string): Target API endpoint for OpenCode agent calls.
- `model` (string): Selected LLM.
- `thinking_level` (string): Model thinking level.
- `hide_thinking` (boolean): Toggles visibility of model thinking blocks.
