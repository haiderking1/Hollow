#!/usr/bin/env python3
"""Remove Enough-era naming leftovers; rename _enough_home helpers to _hollow_home."""

from __future__ import annotations

import re
from pathlib import Path

REPO = Path(__file__).resolve().parents[1]
SKIP_DIRS = {"node_modules", "dist", ".git"}
SHIM = '''"""Deprecated — use _hollow_home."""
from _hollow_home import *  # noqa: F403
'''

TEXT_ROOTS = [
    REPO / "backend",
    REPO / "runtime",
    REPO / "desktop",
    REPO / "scripts",
]

# Whole-word / path replacements (order matters for longer patterns first).
REPLACEMENTS = [
    ("openclaw_to_enough.py", "openclaw_to_hollow.py"),
    ("_enough_env", "_hollow_env"),
    ("ENOUGH_HOME", "HOLLOW_HOME"),
    ("$HOME/.enough", "$HOME/.hollow"),
    ("~/.enough", "~/.hollow"),
    ('"source": "enough"', '"source": "hollow"'),
    ("commands: [enough]", "commands: [bash]"),
    ("enough mcp call", "hollow mcp call"),
    ("enough --skills", "hollow --skills"),
    ("enough skills", "hollow skills"),
    ("enough curator", "hollow curator"),
    ("enough -q", "hollow -q"),
    ("enough                              #", "hollow                              #"),
    ("enough\n", "hollow\n"),  # CLI block: bare `enough` line in bash fences
]

EXTENSIONS = {
    ".ts",
    ".tsx",
    ".cjs",
    ".md",
    ".py",
    ".sh",
    ".tmpl",
    ".json",
    ".gitignore",
}


def should_process(path: Path) -> bool:
    if any(part in SKIP_DIRS for part in path.parts):
        return False
    if path.name == "remove_enough_leftovers.py":
        return False
    return path.suffix in EXTENSIONS or path.name.endswith(".d.ts")


def rename_home_helpers() -> list[Path]:
    changed: list[Path] = []
    for src in sorted(REPO.rglob("_enough_home.py")):
        if any(part in SKIP_DIRS for part in src.parts):
            continue
        dst = src.with_name("_hollow_home.py")
        if dst.exists():
            continue
        src.rename(dst)
        shim = src.parent / "_enough_home.py"
        if not shim.exists():
            shim.write_text(SHIM, encoding="utf-8")
        changed.append(dst)
    return changed


def apply_text_replacements() -> list[Path]:
    changed: list[Path] = []
    for root in TEXT_ROOTS:
        if not root.exists():
            continue
        for path in sorted(root.rglob("*")):
            if not path.is_file() or not should_process(path):
                continue
            original = path.read_text(encoding="utf-8")
            text = original
            for old, new in REPLACEMENTS:
                text = text.replace(old, new)
            if text != original:
                path.write_text(text, encoding="utf-8")
                changed.append(path)
    return changed


def main() -> None:
    renamed = rename_home_helpers()
    updated = apply_text_replacements()
    print(f"Renamed {len(renamed)} _enough_home.py -> _hollow_home.py (with shims)")
    print(f"Updated text in {len(updated)} files")
    for path in updated[:25]:
        print(f"  {path.relative_to(REPO)}")
    if len(updated) > 25:
        print(f"  ... and {len(updated) - 25} more")


if __name__ == "__main__":
    main()
