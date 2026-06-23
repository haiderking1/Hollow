#!/usr/bin/env python3
"""Adapt Hermes-ported skill trees to Hollow conventions (bundled + optional).

Run from repo root: python3 scripts/adapt_skills.py

Preserves:
  - YAML key ``metadata.hermes:`` (schema namespace)
  - Nous ``Hermes`` *model* names (LLM family, not the agent product)
  - JSON/dict keys like ``"hermes": {`` in model-family maps
"""

from __future__ import annotations

import re
import shutil
from pathlib import Path

REPO = Path(__file__).resolve().parents[1]
ROOTS = [
    REPO / "backend" / "skills" / "bundled",
    REPO / "backend" / "skills" / "optional",
]

SKIP_SUFFIXES = {
    ".png", ".jpg", ".jpeg", ".gif", ".webp", ".pdf", ".zip", ".tar", ".gz",
    ".woff", ".woff2", ".ttf", ".eot", ".ico", ".mp3", ".mp4", ".wav",
    ".pptx", ".docx", ".xlsx", ".bst", ".cls", ".sty", ".pptx",
}

# Lines we must not rewrite (model-family / schema)
PROTECT_LINE_RE = re.compile(
    r"(metadata\.hermes\s*:|"
    r'^\s*"hermes"\s*:|'
    r'^\s*\'hermes\'\s*:|'
    r"return\s+\"hermes\"|"
    r'if\s+"hermes"\s+in\s+model|'
    r"Nous/Hermes|Hermes \d|Hermes 4|"
    r"hermes_constants|"
    r"npx get-shit-done-cc --hermes|"
    r"openai/hermes|"
    r"models/hermes|"
    r"name: hermes-agent|"
    r"# hermes-agent \(deprecated\)|"
    r"not Hermes Agent|"
    r"Hermes-only|"
    r"NOT IN HOLLOW|"
    r"metadata\.hermes|"
    r"/opt/hermes/|"
    r"hermes\.coder|"
    r"01-hermes-setup|"
    r"main-hermes|"
    r"migrate-hermes|"
    r"NousResearch/Nous-Hermes)",
    re.IGNORECASE,
)

REPLACEMENTS: list[tuple[str, str]] = [
    (r"\$\{HERMES_HOME:-\$HOME/\.hermes\}", "${HOLLOW_HOME:-$HOME/.hollow}"),
    (r"\$\{HERMES_HOME:-~/\.hermes\}", "${HOLLOW_HOME:-~/.hollow}"),
    (r"\$\{HERMES_HOME\}", "${HOLLOW_HOME}"),
    (r"os\.environ\.get\(\"HERMES_HOME\"", 'os.environ.get("HOLLOW_HOME"'),
    (r"os\.getenv\(\"HERMES_HOME\"", 'os.getenv("HOLLOW_HOME"'),
    (r"os\.getenv\('HERMES_HOME'", "os.getenv('HOLLOW_HOME'"),
    (r"\$HERMES_HOME", "$HOLLOW_HOME"),
    (r"\bHERMES_HOME\b", "HOLLOW_HOME"),
    (r"HERMES_CFG", "HOLLOW_CFG"),
    (r"HERMES_PLATFORM", "HOLLOW_PLATFORM"),
    (r"HERMES_KANBAN_", "HOLLOW_KANBAN_"),
    (r"HERMES_SKILL_DIR", "HOLLOW_SKILL_DIR"),
    (r"HERMES_SESSION_ID", "HOLLOW_SESSION_ID"),
    (r"~/\.hermes/", "~/.hollow/"),
    (r"/\.hermes/", "/.hollow/"),
    (r"~/.hermes", "~/.hollow"),
    (r"\.hermes/plans", ".hollow/plans"),
    (r"\.hermes/prefill\.json", ".hollow/prefill.json"),
    (r"author: Hermes Agent \+", "author: Hollow +"),
    (r"author: Hermes Agent \(", "author: Hollow ("),
    (r"author: Hermes Agent \+", "author: Hollow +"),
    (r"author: Hermes Agent", "author: Hollow"),
    (r"Hermes Agent \+", "Hollow +"),
    (r"Hermes Agent \(", "Hollow ("),
    (r"Hermes Agent — Implementation Notes", "Hollow — Implementation Notes"),
    (r"Hermes CLI", "Hollow CLI"),
    (r"## Hermes Agent Integration", "## Hollow Integration"),
    (r"## Hermes Integration", "## Hollow Integration"),
    (r"\*\*For Hermes:\*\*", "**For Hollow:**"),
    (r"Rules for Hermes Agents", "Rules for Hollow Agents"),
    (r"Hermes Orchestration Guide", "Hollow Orchestration Guide"),
    (r"via the Hermes terminal", "via Hollow's bash tool"),
    (r"via Hermes terminal", "via Hollow bash"),
    (r"Hermes interacts with", "Hollow interacts with"),
    (r"Hermes terminal", "Hollow bash"),
    (r"Use these Hermes tools", "Use these tools"),
    (r"Use Hermes tools", "Use Hollow's tools"),
    (r"Hermes file tools", "file tools (read_file, write_file, bash, grep)"),
    (r"Hermes tool subprocesses", "Hollow bash subprocesses"),
    (r"Hermes-run ", "Enough-run "),
    (r"Hermes adaptation:", "Hollow adaptation:"),
    (r"Hermes-facing", "Enough-facing"),
    (r"Hermes config paths", "Hollow config paths"),
    (r"from Hermes config", "from Hollow config"),
    (r"Hermes config\.json", "Hollow config.json"),
    (r"Hermes config\.yaml", "Hollow config.json"),
    (r"Read current model and provider from Hermes", "Read current model from Hollow"),
    (r"Restart Hermes", "Restart Hollow"),
    (r"integrated into hermes-agent", "integrated into Hollow"),
    (r"hermes-agent skill", "enough skill"),
    (r"hermes-agent-skill-authoring", "hollow-skill-authoring"),
    (r"debugging-hermes-tui-commands", "enough"),
    (r"openclaw_to_hermes", "openclaw_to_enough"),
    (r"OpenClaw to Hermes", "OpenClaw to Hollow"),
    (r"migrate.*Hermes Agent", "migrate to Hollow"),
    (r"`hermes teams-pipeline", "(teams-pipeline not in Hollow — Hermes-only)"),
    (r"`hermes skills", "`hollow skills"),
    (r"`hermes chat -q", "`hollow -q"),
    (r"`hermes chat", "`enough"),
    (r"`hermes --tui", "`enough"),
    (r"`hermes setup", "`/connect` or `~/.hollow/config.json`"),
    (r"`hermes model", "`/model` in the TUI"),
    (r"`hermes config set", "edit `~/.hollow/config.json`"),
    (r"`hermes config", "edit `~/.hollow/config.json`"),
    (r"`hermes doctor", "`hollow skills sync`"),
    (r"`hermes update", "rebuild/reinstall Hollow"),
    (r"`hermes plugins", "(plugins not in Hollow)"),
    (r"`hermes gateway", "(gateway not in Hollow)"),
    (r"`hermes tools", "Hollow agent tools"),
    (r"`hermes ", "`hollow "),
    (r"\bhermes skills\b", "enough skills"),
    (r"\bhermes gateway\b", "Hollow UI"),
    (r"\btui_gateway\b", "Hollow UI"),
    (r"\bhermes update\b", "reinstall Hollow"),
    (r"\bhermes doctor\b", "hollow skills sync"),
    (r"/home/bb/hermes-agent", "the Hollow project"),
    (r"hermes-agent\.nousresearch\.com", "github.com/haiderking1/Hollow"),
    (r"via `hermes setup`", "in `~/.hollow/.env`"),
    (r"\bhermes setup\b", "`/connect` or `~/.hollow/config.json`"),
    (r"config\.yaml", "config.json"),
    (r"get_hermes_home\(\)", "get_hollow_home()"),
    (r"display_hermes_home\(\)", "display_hollow_home()"),
    (r"from _hermes_home import", "from _hollow_home import"),
    (r"import _hermes_home", "import _hollow_home"),
    (r"_hermes_env=", "_enough_env="),
    (r"\b_hermes_env\b", "_enough_env"),
    (r"running Hermes", "running Hollow"),
    (r"the Hermes Docker", "a Docker"),
    (r"official Hermes Docker", "containerized"),
    (r"machine running Hermes", "machine running Hollow"),
    (r"Hermes-specific", "Hollow-specific"),
    (r"Debugging Hermes", "Debugging Hollow"),
    (r"in Hermes,", "in Hollow,"),
    (r"in Hermes ", "in Hollow "),
    (r"Inside Hermes", "Inside Hollow"),
    (r"From inside Hermes", "From inside Hollow"),
    (r"Hermes, launch", "Hollow, launch"),
    (r"Hermes subagents", "Hollow swarm workers"),
    (r"Hermes test runner", "go test"),
    (r"the Hermes Kanban", "Kanban (Hermes-only — not in Hollow)"),
    (r"Hermes Kanban", "Kanban (Hermes-only)"),
    (r"commands: \[hermes\]", "commands: [enough]"),
    (r"prerequisites:\s*\n\s*commands: \[enough\]", "prerequisites:\n  commands: [bash]"),
    (r"docker, ssh, modal, and daytona backends", "the local workspace"),
    (r"local, docker, ssh, modal, and daytona", "local"),
    (r"Path\.home\(\) / \"\.hermes\"", 'Path.home() / ".hollow"'),
    (r"Path\.home\(\) / '\.hermes'", "Path.home() / '.hollow'"),
    (r"str\(Path\.home\(\) / \"\.hermes\"\)", 'str(Path.home() / ".hollow")'),
    (r"User-Agent\": \"hermes-agent/", 'User-Agent": "hollow-agent/'),
    (r"User-Agent\": \"Hermes-Watcher/", 'User-Agent": "Hollow-Watcher/'),
    (r"\"source\": \"hermes-agent\"", '"source": "enough"'),
    (r"ported into hermes-agent", "ported into Hollow"),
    (r"hermes-agent repository", "Hollow repository"),
    (r"hermes-agent repo", "Hollow repo"),
    (r"parent hermes-agent repo", "parent Hollow repo"),
    (r"Hermes' own native MCP", "Hollow's native MCP (if configured)"),
    (r"Hermes' browser tools", "browser tools (if available)"),
    (r"Hermes' gateway adapters", "Hollow gateway adapters (Hermes-only)"),
    (r"Hermes has three design-related", "Hollow has three design-related"),
    (r"Hermes has `image_generate`", "Hollow has `image_generate`"),
    (r"compose with other Hermes skills", "compose with other Hollow skills"),
    (r"### Hermes Tools Reference", "### Hollow Tools Reference"),
    (r"from Hermes,", "from Hollow,"),
    (r"from Hermes ", "from Hollow "),
    (r"\(use this from Hermes\)", "(use this from Hollow)"),
    (r"Non-interactive \(use this from Hermes\)", "Non-interactive (use this from Hollow)"),
    (r"hermes-outreach", "hollow-outreach"),
    (r"description=\"Hermes telephony", 'description="Hollow telephony'),
    (r"Hermes telephony helper", "Hollow telephony helper"),
    (r"Hermes already ships PyYAML", "PyYAML may already be installed"),
    (r"\$HERMES_OSINT_CACHE", "$HOLLOW_OSINT_CACHE"),
    (r"~/.cache/hermes-osint/", "~/.cache/hollow-osint/"),
    (r"running as Hermes inside", "running as Hollow inside"),
    (r"help=\"Hermes home directory\"", 'help="Hollow home directory (~/.hollow)"'),
    (r"hermes_home = os.environ", "enough_home = os.environ"),
    (r"_HERMES_HOME = Path", "_HOLLOW_HOME = Path"),
    (r"name: hermes-s6-container-supervision", "name: hollow-s6-container-supervision"),
    (r"# Hermes s6-overlay", "# s6-overlay (Hermes Docker image — not in Hollow)"),
    (r"related_skills: \[enough, hollow-dev\]", "related_skills: [enough]"),
    (r"hermes --toolsets mcp", "enough --skills agentmail -q"),
    (r"```yaml\nmcp_servers:", "```json\n\"mcp_servers\":"),
    (r"hermes whatsapp", "(whatsapp gateway — Hermes-only)"),
    (r"hermes mcp list", "hollow skills list (MCP via config.json)"),
    (r"hermes cron", "cron (Hermes-only — use bash + cron on host)"),
    (r"hermes config set", "edit ~/.hollow/config.json"),
    (r"# Hermes config paths", "# Hollow config paths"),
]

SECOND_PASS: list[tuple[str, str]] = [
    (r"\bfor Hermes Agent\b", "for Hollow"),
    (r"\bHermes Agent\b", "Hollow"),
    (r"\bHermes Gateway\b", "messaging gateway (Hermes-only — not in Hollow)"),
    (r"\bHermes config\b", "Hollow `config.json`"),
    (r"\bHermes session\b", "Hollow session"),
    (r"\bHermes memory\b", "Hollow memory (MEMORY.md)"),
    (r"\bHermes TTS\b", "TTS"),
    (r"\bHermes STT\b", "STT"),
    (r"\bHermes-native\b", "Hollow-native"),
    (r"\bHermes-managed\b", "managed"),
    (r"\bHermes Email gateway\b", "email gateway (Hermes-only)"),
    (r"\bHermes Email\b", "email gateway (Hermes-only)"),
    (r"\bGive Hermes\b", "Give Hollow"),
    (r"\bturn Hermes into\b", "turn Hollow into"),
    (r"\bsave credentials into Hermes\b", "save credentials to `~/.hollow/.env`"),
    (r"\bHermes setups\b", "multi-agent setups"),
    (r"\bRestart the Hermes\b", "Restart Hollow"),
    (r"\bRestart Hermes\b", "Restart Hollow"),
    (r"\bfrom Hermes gateway\b", "from Hollow"),
    (r"\bfrom Hermes,\b", "from Hollow,"),
    (r"\bfrom Hermes \b", "from Hollow "),
    (r"\bfrom a Hermes\b", "from Hollow"),
    (r"\bvia Hermes\b", "via Hollow"),
    (r"\bto Hermes\b", "to Hollow"),
    (r"\bin Hermes\b", "in Hollow"),
    (r"\bWhen Hermes\b", "When Hollow"),
    (r"\bIf Hermes\b", "If Hollow"),
    (r"\bthe Hermes\b", "the Hollow"),
    (r"\bHermes Configuration\b", "Hollow Configuration"),
    (r"\bHermes configuration\b", "Hollow configuration"),
    (r"\bHermes MCP\b", "MCP (configure in `config.json` if supported)"),
    (r"\bHermes ->\b", "Hollow ->"),
    (r"\bHermes swarm\b", "Hollow swarm"),
    (r"\bHermes subagent\b", "Hollow subagent"),
    (r"\bHermes worker\b", "Hollow worker"),
    (r"\bHermes CLI\b", "Hollow CLI"),
    (r"\bHermes tool\b", "Hollow tool"),
    (r"\bHermes tools\b", "Hollow tools"),
    (r"\bHermes run\b", "Hollow run"),
    (r"\bHermes process\b", "Hollow process"),
    (r"\bHermes user\b", "user"),
    (r"\bHermes operator\b", "operator"),
    (r"\bHermes developer\b", "developer"),
    (r"^hermes teams-pipeline", "# NOT IN HOLLOW (Hermes CLI only): hermes teams-pipeline"),
    (r"`hermes-agent` \(Hollow swarm", "`agent_swarm` (Hollow swarm"),
    (r"related_skills:.*hermes-video", "related_skills: [ascii-video, manim-video]"),
    (r"hermes-agent@", "hollow-agent@"),
    (r"username \(e\.g\. `hermes-agent`\)", "username (e.g. `hollow-agent`)"),
    (r"description: Plan mode:.*\.hermes/plans/", "description: \"Plan mode: write an actionable markdown plan to .hollow/plans/, no execution.\""),
    (r"description: Give the agent its own dedicated email.*hermes-agent@", 'description: "Give Hollow its own dedicated email inbox via AgentMail."'),
    (r"description: Give Hermes phone", 'description: "Give Hollow phone capabilities via Twilio/Bland/Vapi (optional skill)."'),
    (r"Typical Hermes Workflow", "Typical Hollow workflow"),
    (r"Important Notes for Hermes", "Important Notes for Hollow"),
    (r"for Hermes\.", "for Hollow."),
    (r"Hermes can", "Hollow can"),
    (r"Hermes' existing", "Hollow's existing"),
    (r"So Hermes can", "So Hollow can"),
    (r"exists so Hermes can", "exists so Hollow can"),
    (r"`hermes --tui", "`enough"),
    (r"\bhermes --tui\b", "enough"),
    (r"github\.com/NousResearch/hermes-agent", "github.com/haiderking1/Hollow"),
    (r"```bash\nhermes\n```", "```bash\nenough\n```"),
    (r"\bI want Hermes to own\b", "I want Hollow to own"),
    (r"\bpairs well with Hermes `text_to_speech`", "pairs well with TTS via bash"),
    (r"\bHermes's configured TTS\b", "configured TTS"),
    (r"\bHermes's existing `text_to_speech`", "TTS via bash or external API"),
    (r"\bAsk Hermes to create\b", "Generate"),
    (r"\bThis is Hermes calling\b", "This is Hollow calling"),
    (r"\bHermes rendering / audio\b", "rendering / audio"),
    (r"\bcreates Hermes profiles\b", "creates Hermes profiles"),  # Hermes-only kanban — keep
    (r"\bHermes profile rules\b", "Hermes profile rules"),  # keep
    (r"hermes-agent/skills/", "Hollow skills tree (bundled/)"),
    (r"hermes-agent-harness", "hollow-harness"),
    (r"hermes-tool-quirks", "hollow-tool-quirks"),
    (r"hermes-agent-dev", "hollow-dev"),
    (r"hermes-agent-comfyui", "hollow-comfyui"),
    (r"~/Downloads/hermes-google", "~/Downloads/hollow-google"),
    (r"For Hermes itself,", "In Hollow,"),
    (r"like \"hermes-agent\"", "like \"hollow-agent\""),
    (r"Give it a name like \"hermes-agent\"", "Give it a name like \"hollow-agent\""),
    (r"title like \"hermes-agent-", "title like \"hollow-agent-"),
    (r"purzbeats/hermes-agent-comfyui", "purzbeats/hollow-comfyui-helper"),
    (r"hermes-google-client-secret", "hollow-google-client-secret"),
]

RELATED_SKILL_RE = re.compile(
    r"(related_skills:\s*\[[^\]]*)\bhermes-agent\b([^\]]*\])"
)

HOLLOW_HOME_PY = '''"""Resolve HOLLOW_HOME for standalone skill scripts."""

from __future__ import annotations

import os
from pathlib import Path


def get_hollow_home() -> Path:
    val = os.environ.get("HOLLOW_HOME", "").strip()
    if val:
        return Path(val)
    legacy = os.environ.get("HERMES_HOME", "").strip()
    if legacy:
        return Path(legacy)
    return Path.home() / ".hollow"


def display_hollow_home() -> str:
    home = get_hollow_home()
    try:
        return "~/" + str(home.relative_to(Path.home()))
    except ValueError:
        return str(home)


get_hermes_home = get_hollow_home
display_hermes_home = display_hollow_home
'''

HERMES_AGENT_STUB = """---
name: hermes-agent
description: "Deprecated alias — load the enough skill instead."
version: 2.1.0
author: Hollow
license: MIT
platforms: [linux, darwin, windows]
disable-model-invocation: true
metadata:
  hermes:
    tags: [enough, deprecated]
    related_skills: [enough]
---

# hermes-agent (deprecated)

You are running **Hollow**, not Hermes Agent. Load the **`hollow`** skill for all self-configuration, paths, and CLI commands.
"""

HERMES_ONLY_BANNER = """
> **Not available in Hollow:** This skill targets Hermes-only infrastructure (gateway, profiles, plugins, or CLI subcommands Hollow does not ship). Load the **`hollow`** skill for what *this* agent supports. Only proceed if the user explicitly runs Hermes elsewhere.
"""

HERMES_ONLY_SKILLS = {
    "teams-meeting-pipeline",
    "kanban-worker",
    "kanban-orchestrator",
    "kanban-video-orchestrator",
    "enough-s6-container-supervision",
    "honcho",
}


def adapt_line(line: str) -> str:
    if PROTECT_LINE_RE.search(line):
        return line
    out = line
    for pat, repl in REPLACEMENTS + SECOND_PASS:
        out = re.sub(pat, repl, out)
    return out


def adapt_text(text: str) -> str:
    lines = [adapt_line(ln) for ln in text.splitlines(keepends=True)]
    text = "".join(lines)
    text = RELATED_SKILL_RE.sub(r"\1enough\2", text)
    return text


def skill_name_from_md(path: Path) -> str | None:
    if path.name != "SKILL.md":
        return None
    try:
        head = path.read_text(encoding="utf-8")[:800]
    except OSError:
        return None
    m = re.search(r"^name:\s*(\S+)", head, re.MULTILINE)
    return m.group(1) if m else None


def inject_hermes_only_banner(skill_md: Path) -> None:
    name = skill_name_from_md(skill_md)
    if not name or name not in HERMES_ONLY_SKILLS:
        return
    text = skill_md.read_text(encoding="utf-8")
    if "Not available in Hollow" in text:
        return
    m = re.search(r"\n---\s*\n\n(# [^\n]+\n)", text)
    if m:
        insert_at = m.end(1)
        text = text[:insert_at] + HERMES_ONLY_BANNER + text[insert_at:]
    else:
        text = text + HERMES_ONLY_BANNER
    skill_md.write_text(text, encoding="utf-8")


def ensure_hollow_home_helper(scripts_dir: Path) -> None:
    if not scripts_dir.is_dir():
        return
    target = scripts_dir / "_hollow_home.py"
    if not target.exists():
        target.write_text(HOLLOW_HOME_PY, encoding="utf-8")
    legacy = scripts_dir / "_hermes_home.py"
    if not legacy.exists():
        legacy.write_text(
            '"""Deprecated — use _hollow_home."""\nfrom _hollow_home import *  # noqa: F403\n',
            encoding="utf-8",
        )


def process_tree(root: Path) -> int:
    changed = 0
    for path in root.rglob("*"):
        if not path.is_file() or path.suffix.lower() in SKIP_SUFFIXES:
            continue
        if path.name.startswith("."):
            continue
        try:
            original = path.read_text(encoding="utf-8")
        except (UnicodeDecodeError, OSError):
            continue
        updated = adapt_text(original)
        if updated != original:
            path.write_text(updated, encoding="utf-8")
            changed += 1
        if path.name == "SKILL.md":
            inject_hermes_only_banner(path)
        if path.parent.name == "scripts":
            ensure_hollow_home_helper(path.parent)
    return changed


def post_process() -> None:
    # hermes-agent stub
    stub = REPO / "backend/skills/bundled/autonomous-ai-agents/hermes-agent/SKILL.md"
    stub.write_text(HERMES_AGENT_STUB, encoding="utf-8")

    # Remove Hermes-only reference docs (misleading in Hollow)
    refs = REPO / "backend/skills/bundled/autonomous-ai-agents/hermes-agent/references"
    if refs.is_dir():
        for f in refs.glob("*.md"):
            f.write_text(
                "# Moved\n\nLoad the **`hollow`** skill. This Hermes-only reference is not applicable to Hollow.\n",
                encoding="utf-8",
            )

    # Rename migration script if present
    mig = REPO / "backend/skills/optional/migration/openclaw-migration/scripts"
    old = mig / "openclaw_to_hermes.py"
    new = mig / "openclaw_to_enough.py"
    if old.exists() and not new.exists():
        shutil.move(str(old), str(new))

    # Rename s6 skill folder reference in name only via SKILL frontmatter handled by adapt

    # Sync enough embed copies
    enough_bundled = REPO / "backend/skills/bundled/enough/SKILL.md"
    enough_embed = REPO / "backend/skills/hollow_skill/SKILL.md"
    if enough_bundled.exists():
        shutil.copy2(enough_bundled, enough_embed)


def main() -> None:
    total = 0
    for root in ROOTS:
        if root.is_dir():
            n = process_tree(root)
            print(f"adapted {n} files under {root.relative_to(REPO)}")
            total += n
    post_process()
    print(f"done ({total} file edits)")


if __name__ == "__main__":
    main()
