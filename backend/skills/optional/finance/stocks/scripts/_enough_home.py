"""Resolve HOLLOW_HOME for standalone skill scripts."""

from __future__ import annotations

import os
from pathlib import Path


def get_hollow_home() -> Path:
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


get_hermes_home = get_hollow_home
display_hermes_home = display_hollow_home
