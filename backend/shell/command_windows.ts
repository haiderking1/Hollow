// PORT: backend/shell/command_windows.go

// setHideWindow hides the child console window on Windows in the Go version.
// Node/Bun child_process does this with the windowsHide option at spawn time.
export const set_hide_window = (cmd: { windows_hide?: boolean }): void => {
  cmd.windows_hide = true;
};

/*
PORT STATUS
source path: backend/shell/command_windows.go
source lines: 15
draft lines: 20
confidence: high
status: phase_a_draft
todos:
  - ensure command_context passes windowsHide: true for Windows spawns
notes:
  - SysProcAttr.CreationFlags is represented by child_process windowsHide.
*/
