#!/usr/bin/env python3
"""Adapt Hermes-ported skill trees to Enough conventions (bundled + optional).

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
    r"NOT IN ENOUGH|"
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
    (r"\$\{HERMES_HOME:-\$HOME/\.hermes\}", "${ENOUGH_HOME:-$HOME/.enough}"),
    (r"\$\{HERMES_HOME:-~/\.hermes\}", "${ENOUGH_HOME:-~/.enough}"),
    (r"\$\{HERMES_HOME\}", "${ENOUGH_HOME}"),
    (r"os\.environ\.get\(\"HERMES_HOME\"", 'os.environ.get("ENOUGH_HOME"'),
    (r"os\.getenv\(\"HERMES_HOME\"", 'os.getenv("ENOUGH_HOME"'),
    (r"os\.getenv\('HERMES_HOME'", "os.getenv('ENOUGH_HOME'"),
    (r"\$HERMES_HOME", "$ENOUGH_HOME"),
    (r"\bHERMES_HOME\b", "ENOUGH_HOME"),
    (r"HERMES_CFG", "ENOUGH_CFG"),
    (r"HERMES_PLATFORM", "ENOUGH_PLATFORM"),
    (r"HERMES_KANBAN_", "ENOUGH_KANBAN_"),
    (r"HERMES_SKILL_DIR", "ENOUGH_SKILL_DIR"),
    (r"HERMES_SESSION_ID", "ENOUGH_SESSION_ID"),
    (r"~/\.hermes/", "~/.enough/"),
    (r"/\.hermes/", "/.enough/"),
    (r"~/.hermes", "~/.enough"),
    (r"\.hermes/plans", ".enough/plans"),
    (r"\.hermes/prefill\.json", ".enough/prefill.json"),
    (r"author: Hermes Agent \+", "author: Enough +"),
    (r"author: Hermes Agent \(", "author: Enough ("),
    (r"author: Hermes Agent \+", "author: Enough +"),
    (r"author: Hermes Agent", "author: Enough"),
    (r"Hermes Agent \+", "Enough +"),
    (r"Hermes Agent \(", "Enough ("),
    (r"Hermes Agent — Implementation Notes", "Enough — Implementation Notes"),
    (r"Hermes CLI", "Enough CLI"),
    (r"## Hermes Agent Integration", "## Enough Integration"),
    (r"## Hermes Integration", "## Enough Integration"),
    (r"\*\*For Hermes:\*\*", "**For Enough:**"),
    (r"Rules for Hermes Agents", "Rules for Enough Agents"),
    (r"Hermes Orchestration Guide", "Enough Orchestration Guide"),
    (r"via the Hermes terminal", "via Enough's bash tool"),
    (r"via Hermes terminal", "via Enough bash"),
    (r"Hermes interacts with", "Enough interacts with"),
    (r"Hermes terminal", "Enough bash"),
    (r"Use these Hermes tools", "Use these tools"),
    (r"Use Hermes tools", "Use Enough's tools"),
    (r"Hermes file tools", "file tools (read_file, write_file, bash, grep)"),
    (r"Hermes tool subprocesses", "Enough bash subprocesses"),
    (r"Hermes-run ", "Enough-run "),
    (r"Hermes adaptation:", "Enough adaptation:"),
    (r"Hermes-facing", "Enough-facing"),
    (r"Hermes config paths", "Enough config paths"),
    (r"from Hermes config", "from Enough config"),
    (r"Hermes config\.json", "Enough config.json"),
    (r"Hermes config\.yaml", "Enough config.json"),
    (r"Read current model and provider from Hermes", "Read current model from Enough"),
    (r"Restart Hermes", "Restart Enough"),
    (r"integrated into hermes-agent", "integrated into Enough"),
    (r"hermes-agent skill", "enough skill"),
    (r"hermes-agent-skill-authoring", "enough-skill-authoring"),
    (r"debugging-hermes-tui-commands", "enough"),
    (r"openclaw_to_hermes", "openclaw_to_enough"),
    (r"OpenClaw to Hermes", "OpenClaw to Enough"),
    (r"migrate.*Hermes Agent", "migrate to Enough"),
    (r"`hermes teams-pipeline", "(teams-pipeline not in Enough — Hermes-only)"),
    (r"`hermes skills", "`enough skills"),
    (r"`hermes chat -q", "`enough -q"),
    (r"`hermes chat", "`enough"),
    (r"`hermes --tui", "`enough"),
    (r"`hermes setup", "`/connect` or `~/.enough/config.json`"),
    (r"`hermes model", "`/model` in the TUI"),
    (r"`hermes config set", "edit `~/.enough/config.json`"),
    (r"`hermes config", "edit `~/.enough/config.json`"),
    (r"`hermes doctor", "`enough skills sync`"),
    (r"`hermes update", "rebuild/reinstall Enough"),
    (r"`hermes plugins", "(plugins not in Enough)"),
    (r"`hermes gateway", "(gateway not in Enough)"),
    (r"`hermes tools", "Enough agent tools"),
    (r"`hermes ", "`enough "),
    (r"\bhermes skills\b", "enough skills"),
    (r"\bhermes gateway\b", "Enough TUI"),
    (r"\btui_gateway\b", "Enough TUI"),
    (r"\bhermes update\b", "reinstall Enough"),
    (r"\bhermes doctor\b", "enough skills sync"),
    (r"/home/bb/hermes-agent", "the Enough project"),
    (r"hermes-agent\.nousresearch\.com", "github.com/enough/enough"),
    (r"via `hermes setup`", "in `~/.enough/.env`"),
    (r"\bhermes setup\b", "`/connect` or `~/.enough/config.json`"),
    (r"config\.yaml", "config.json"),
    (r"get_hermes_home\(\)", "get_enough_home()"),
    (r"display_hermes_home\(\)", "display_enough_home()"),
    (r"from _hermes_home import", "from _enough_home import"),
    (r"import _hermes_home", "import _enough_home"),
    (r"_hermes_env=", "_enough_env="),
    (r"\b_hermes_env\b", "_enough_env"),
    (r"running Hermes", "running Enough"),
    (r"the Hermes Docker", "a Docker"),
    (r"official Hermes Docker", "containerized"),
    (r"machine running Hermes", "machine running Enough"),
    (r"Hermes-specific", "Enough-specific"),
    (r"Debugging Hermes", "Debugging Enough"),
    (r"in Hermes,", "in Enough,"),
    (r"in Hermes ", "in Enough "),
    (r"Inside Hermes", "Inside Enough"),
    (r"From inside Hermes", "From inside Enough"),
    (r"Hermes, launch", "Enough, launch"),
    (r"Hermes subagents", "Enough swarm workers"),
    (r"Hermes test runner", "go test"),
    (r"the Hermes Kanban", "Kanban (Hermes-only — not in Enough)"),
    (r"Hermes Kanban", "Kanban (Hermes-only)"),
    (r"commands: \[hermes\]", "commands: [enough]"),
    (r"prerequisites:\s*\n\s*commands: \[enough\]", "prerequisites:\n  commands: [bash]"),
    (r"docker, ssh, modal, and daytona backends", "the local workspace"),
    (r"local, docker, ssh, modal, and daytona", "local"),
    (r"Path\.home\(\) / \"\.hermes\"", 'Path.home() / ".enough"'),
    (r"Path\.home\(\) / '\.hermes'", "Path.home() / '.enough'"),
    (r"str\(Path\.home\(\) / \"\.hermes\"\)", 'str(Path.home() / ".enough")'),
    (r"User-Agent\": \"hermes-agent/", 'User-Agent": "enough-agent/'),
    (r"User-Agent\": \"Hermes-Watcher/", 'User-Agent": "Enough-Watcher/'),
    (r"\"source\": \"hermes-agent\"", '"source": "enough"'),
    (r"ported into hermes-agent", "ported into Enough"),
    (r"hermes-agent repository", "Enough repository"),
    (r"hermes-agent repo", "Enough repo"),
    (r"parent hermes-agent repo", "parent Enough repo"),
    (r"Hermes' own native MCP", "Enough's native MCP (if configured)"),
    (r"Hermes' browser tools", "browser tools (if available)"),
    (r"Hermes' gateway adapters", "Enough gateway adapters (Hermes-only)"),
    (r"Hermes has three design-related", "Enough has three design-related"),
    (r"Hermes has `image_generate`", "Enough has `image_generate`"),
    (r"compose with other Hermes skills", "compose with other Enough skills"),
    (r"### Hermes Tools Reference", "### Enough Tools Reference"),
    (r"from Hermes,", "from Enough,"),
    (r"from Hermes ", "from Enough "),
    (r"\(use this from Hermes\)", "(use this from Enough)"),
    (r"Non-interactive \(use this from Hermes\)", "Non-interactive (use this from Enough)"),
    (r"hermes-outreach", "enough-outreach"),
    (r"description=\"Hermes telephony", 'description="Enough telephony'),
    (r"Hermes telephony helper", "Enough telephony helper"),
    (r"Hermes already ships PyYAML", "PyYAML may already be installed"),
    (r"\$HERMES_OSINT_CACHE", "$ENOUGH_OSINT_CACHE"),
    (r"~/.cache/hermes-osint/", "~/.cache/enough-osint/"),
    (r"running as Hermes inside", "running as Enough inside"),
    (r"help=\"Hermes home directory\"", 'help="Enough home directory (~/.enough)"'),
    (r"hermes_home = os.environ", "enough_home = os.environ"),
    (r"_HERMES_HOME = Path", "_ENOUGH_HOME = Path"),
    (r"name: hermes-s6-container-supervision", "name: enough-s6-container-supervision"),
    (r"# Hermes s6-overlay", "# s6-overlay (Hermes Docker image — not in Enough)"),
    (r"related_skills: \[enough, enough-dev\]", "related_skills: [enough]"),
    (r"hermes --toolsets mcp", "enough --skills agentmail -q"),
    (r"```yaml\nmcp_servers:", "```json\n\"mcp_servers\":"),
    (r"hermes whatsapp", "(whatsapp gateway — Hermes-only)"),
    (r"hermes mcp list", "enough skills list (MCP via config.json)"),
    (r"hermes cron", "cron (Hermes-only — use bash + cron on host)"),
    (r"hermes config set", "edit ~/.enough/config.json"),
    (r"# Hermes config paths", "# Enough config paths"),
]

SECOND_PASS: list[tuple[str, str]] = [
    (r"\bfor Hermes Agent\b", "for Enough"),
    (r"\bHermes Agent\b", "Enough"),
    (r"\bHermes Gateway\b", "messaging gateway (Hermes-only — not in Enough)"),
    (r"\bHermes config\b", "Enough `config.json`"),
    (r"\bHermes session\b", "Enough session"),
    (r"\bHermes memory\b", "Enough memory (MEMORY.md)"),
    (r"\bHermes TTS\b", "TTS"),
    (r"\bHermes STT\b", "STT"),
    (r"\bHermes-native\b", "Enough-native"),
    (r"\bHermes-managed\b", "managed"),
    (r"\bHermes Email gateway\b", "email gateway (Hermes-only)"),
    (r"\bHermes Email\b", "email gateway (Hermes-only)"),
    (r"\bGive Hermes\b", "Give Enough"),
    (r"\bturn Hermes into\b", "turn Enough into"),
    (r"\bsave credentials into Hermes\b", "save credentials to `~/.enough/.env`"),
    (r"\bHermes setups\b", "multi-agent setups"),
    (r"\bRestart the Hermes\b", "Restart Enough"),
    (r"\bRestart Hermes\b", "Restart Enough"),
    (r"\bfrom Hermes gateway\b", "from Enough"),
    (r"\bfrom Hermes,\b", "from Enough,"),
    (r"\bfrom Hermes \b", "from Enough "),
    (r"\bfrom a Hermes\b", "from Enough"),
    (r"\bvia Hermes\b", "via Enough"),
    (r"\bto Hermes\b", "to Enough"),
    (r"\bin Hermes\b", "in Enough"),
    (r"\bWhen Hermes\b", "When Enough"),
    (r"\bIf Hermes\b", "If Enough"),
    (r"\bthe Hermes\b", "the Enough"),
    (r"\bHermes Configuration\b", "Enough Configuration"),
    (r"\bHermes configuration\b", "Enough configuration"),
    (r"\bHermes MCP\b", "MCP (configure in `config.json` if supported)"),
    (r"\bHermes ->\b", "Enough ->"),
    (r"\bHermes swarm\b", "Enough swarm"),
    (r"\bHermes subagent\b", "Enough subagent"),
    (r"\bHermes worker\b", "Enough worker"),
    (r"\bHermes CLI\b", "Enough CLI"),
    (r"\bHermes tool\b", "Enough tool"),
    (r"\bHermes tools\b", "Enough tools"),
    (r"\bHermes run\b", "Enough run"),
    (r"\bHermes process\b", "Enough process"),
    (r"\bHermes user\b", "user"),
    (r"\bHermes operator\b", "operator"),
    (r"\bHermes developer\b", "developer"),
    (r"^hermes teams-pipeline", "# NOT IN ENOUGH (Hermes CLI only): hermes teams-pipeline"),
    (r"`hermes-agent` \(Enough swarm", "`agent_swarm` (Enough swarm"),
    (r"related_skills:.*hermes-video", "related_skills: [ascii-video, manim-video]"),
    (r"hermes-agent@", "enough-agent@"),
    (r"username \(e\.g\. `hermes-agent`\)", "username (e.g. `enough-agent`)"),
    (r"description: Plan mode:.*\.hermes/plans/", "description: \"Plan mode: write an actionable markdown plan to .enough/plans/, no execution.\""),
    (r"description: Give the agent its own dedicated email.*hermes-agent@", 'description: "Give Enough its own dedicated email inbox via AgentMail."'),
    (r"description: Give Hermes phone", 'description: "Give Enough phone capabilities via Twilio/Bland/Vapi (optional skill)."'),
    (r"Typical Hermes Workflow", "Typical Enough workflow"),
    (r"Important Notes for Hermes", "Important Notes for Enough"),
    (r"for Hermes\.", "for Enough."),
    (r"Hermes can", "Enough can"),
    (r"Hermes' existing", "Enough's existing"),
    (r"So Hermes can", "So Enough can"),
    (r"exists so Hermes can", "exists so Enough can"),
    (r"`hermes --tui", "`enough"),
    (r"\bhermes --tui\b", "enough"),
    (r"github\.com/NousResearch/hermes-agent", "github.com/enough/enough"),
    (r"```bash\nhermes\n```", "```bash\nenough\n```"),
    (r"\bI want Hermes to own\b", "I want Enough to own"),
    (r"\bpairs well with Hermes `text_to_speech`", "pairs well with TTS via bash"),
    (r"\bHermes's configured TTS\b", "configured TTS"),
    (r"\bHermes's existing `text_to_speech`", "TTS via bash or external API"),
    (r"\bAsk Hermes to create\b", "Generate"),
    (r"\bThis is Hermes calling\b", "This is Enough calling"),
    (r"\bHermes rendering / audio\b", "rendering / audio"),
    (r"\bcreates Hermes profiles\b", "creates Hermes profiles"),  # Hermes-only kanban — keep
    (r"\bHermes profile rules\b", "Hermes profile rules"),  # keep
    (r"hermes-agent/skills/", "Enough skills tree (bundled/)"),
    (r"hermes-agent-harness", "enough-harness"),
    (r"hermes-tool-quirks", "enough-tool-quirks"),
    (r"hermes-agent-dev", "enough-dev"),
    (r"hermes-agent-comfyui", "enough-comfyui"),
    (r"~/Downloads/hermes-google", "~/Downloads/enough-google"),
    (r"For Hermes itself,", "In Enough,"),
    (r"like \"hermes-agent\"", "like \"enough-agent\""),
    (r"Give it a name like \"hermes-agent\"", "Give it a name like \"enough-agent\""),
    (r"title like \"hermes-agent-", "title like \"enough-agent-"),
    (r"purzbeats/hermes-agent-comfyui", "purzbeats/enough-comfyui-helper"),
    (r"hermes-google-client-secret", "enough-google-client-secret"),
]

RELATED_SKILL_RE = re.compile(
    r"(related_skills:\s*\[[^\]]*)\bhermes-agent\b([^\]]*\])"
)

ENOUGH_HOME_PY = '''"""Resolve ENOUGH_HOME for standalone skill scripts."""

from __future__ import annotations

import os
from pathlib import Path


def get_enough_home() -> Path:
    val = os.environ.get("ENOUGH_HOME", "").strip()
    if val:
        return Path(val)
    legacy = os.environ.get("HERMES_HOME", "").strip()
    if legacy:
        return Path(legacy)
    return Path.home() / ".enough"


def display_enough_home() -> str:
    home = get_enough_home()
    try:
        return "~/" + str(home.relative_to(Path.home()))
    except ValueError:
        return str(home)


get_hermes_home = get_enough_home
display_hermes_home = display_enough_home
'''

HERMES_AGENT_STUB = """---
name: hermes-agent
description: "Deprecated alias — load the enough skill instead."
version: 2.1.0
author: Enough
license: MIT
platforms: [linux, darwin, windows]
disable-model-invocation: true
metadata:
  hermes:
    tags: [enough, deprecated]
    related_skills: [enough]
---

# hermes-agent (deprecated)

You are running **Enough**, not Hermes Agent. Load the **`enough`** skill for all self-configuration, paths, and CLI commands.
"""

HERMES_ONLY_BANNER = """
> **Not available in Enough:** This skill targets Hermes-only infrastructure (gateway, profiles, plugins, or CLI subcommands Enough does not ship). Load the **`enough`** skill for what *this* agent supports. Only proceed if the user explicitly runs Hermes elsewhere.
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
    if "Not available in Enough" in text:
        return
    m = re.search(r"\n---\s*\n\n(# [^\n]+\n)", text)
    if m:
        insert_at = m.end(1)
        text = text[:insert_at] + HERMES_ONLY_BANNER + text[insert_at:]
    else:
        text = text + HERMES_ONLY_BANNER
    skill_md.write_text(text, encoding="utf-8")


def ensure_enough_home_helper(scripts_dir: Path) -> None:
    if not scripts_dir.is_dir():
        return
    target = scripts_dir / "_enough_home.py"
    if not target.exists():
        target.write_text(ENOUGH_HOME_PY, encoding="utf-8")
    legacy = scripts_dir / "_hermes_home.py"
    if not legacy.exists():
        legacy.write_text(
            '"""Deprecated — use _enough_home."""\nfrom _enough_home import *  # noqa: F403\n',
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
            ensure_enough_home_helper(path.parent)
    return changed


def post_process() -> None:
    # hermes-agent stub
    stub = REPO / "backend/skills/bundled/autonomous-ai-agents/hermes-agent/SKILL.md"
    stub.write_text(HERMES_AGENT_STUB, encoding="utf-8")

    # Remove Hermes-only reference docs (misleading in Enough)
    refs = REPO / "backend/skills/bundled/autonomous-ai-agents/hermes-agent/references"
    if refs.is_dir():
        for f in refs.glob("*.md"):
            f.write_text(
                "# Moved\n\nLoad the **`enough`** skill. This Hermes-only reference is not applicable to Enough.\n",
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
    enough_embed = REPO / "backend/skills/enough_skill/SKILL.md"
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
