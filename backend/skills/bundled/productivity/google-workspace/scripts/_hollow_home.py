"""Resolve HOLLOW_HOME for standalone skill scripts.

Skill scripts may run outside the Hollow process (system Python, CI) where
no in-process helper exists. This module mirrors the Hollow home layout
without a separate agent binary.

Legacy: ``get_hermes_home`` / ``HOLLOW_HOME`` still work as aliases.
"""

from __future__ import annotations

import os
from pathlib import Path


def get_hollow_home() -> Path:
    """Return the Hollow home directory (default: ~/.hollow)."""
    val = os.environ.get("HOLLOW_HOME", "").strip()
    if val:
        return Path(val)
    legacy = os.environ.get("HOLLOW_HOME", "").strip()
    if legacy:
        return Path(legacy)
    return Path.home() / ".hollow"


def display_hollow_home() -> str:
    home = get_hollow_home()
    try:
        return "~/" + str(home.relative_to(Path.home()))
    except ValueError:
        return str(home)


# Backward-compatible aliases for ported Hermes skill scripts
get_hermes_home = get_hollow_home
display_hermes_home = display_hollow_home
