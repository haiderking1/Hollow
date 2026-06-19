# Terminal modes

Enough normally renders in the primary terminal buffer. Enable alternate-screen mode when you want a full-terminal application surface whose scrollback cannot expose shell output from before Enough launched.

Enable it with either:

```sh
ENOUGH_ALT_SCREEN=1 enough
ENOUGH_NO_FLICKER=1 enough
```

Or persist/toggle it inside Enough:

```text
/tui alt-screen on
/tui alt-screen off
```

On exit, Enough leaves the alternate buffer and restores the previous shell display.

## tmux over SSH

Use a terminal definition that supports tmux features:

```tmux
set -g default-terminal "tmux-256color"
set -as terminal-features ",xterm-256color:RGB"
set -g mouse on
setw -g mode-keys vi
```

Reload with `tmux source-file ~/.tmux.conf`. Native terminal scrollback is intentionally unavailable in alternate-screen applications under tmux; enter tmux copy mode with `Ctrl+b [` and leave it with `q`.
