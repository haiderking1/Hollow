"""Resolve ENOUGH_HOME for standalone skill scripts.

Skill scripts may run outside the Enough process (system Python, CI) where
no in-process helper exists. This module mirrors the Enough home layout
without importing the Go binary.

Legacy: ``get_hermes_home`` / ``ENOUGH_HOME`` still work as aliases.
"""

from __future__ import annotations

import os
from pathlib import Path


def get_enough_home() -> Path:
    """Return the Enough home directory (default: ~/.enough)."""
    val = os.environ.get("ENOUGH_HOME", "").strip()
    if val:
        return Path(val)
    legacy = os.environ.get("ENOUGH_HOME", "").strip()
    if legacy:
        return Path(legacy)
    return Path.home() / ".enough"


def display_enough_home() -> str:
    home = get_enough_home()
    try:
        return "~/" + str(home.relative_to(Path.home()))
    except ValueError:
        return str(home)


# Backward-compatible aliases for ported Hermes skill scripts
get_hermes_home = get_enough_home
display_hermes_home = display_enough_home
