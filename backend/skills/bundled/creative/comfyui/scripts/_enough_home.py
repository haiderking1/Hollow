"""Resolve ENOUGH_HOME for standalone skill scripts."""

from __future__ import annotations

import os
from pathlib import Path


def get_enough_home() -> Path:
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


get_hermes_home = get_enough_home
display_hermes_home = display_enough_home
