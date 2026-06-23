// PORT: backend/shell/command_unix.go

// setHideWindow is a no-op on non-Windows.
export const set_hide_window = (_cmd: unknown): void => {
  // No-op on non-Windows
};

/*
PORT STATUS
source path: backend/shell/command_unix.go
source lines: 9
draft lines: 19
confidence: high
status: phase_a_draft
todos:
  - none
notes:
  - Platform placeholder matching Go build-tag split.
*/
