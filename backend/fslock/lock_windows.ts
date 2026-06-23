// PORT: backend/fslock/lock_windows.go
// backend/fslock/lock_windows.go
//go:build windows placeholder

import { Effect } from "effect";
import { file_descriptor, fs_lock_error, fs_lock_error as make_fs_lock_error } from "./lock_unix";

// Lock acquires an exclusive lock on the file handle (blocks until acquired).
export const lock = (f: file_descriptor): Effect.Effect<void, fs_lock_error> =>
  Effect.try({
    try: () => {
      // TODO: wire to actual Windows LockFileEx binding.
      // const handle = windows.Handle(f.Fd());
      // const ol = new windows.Overlapped();
      // return windows.LockFileEx(handle, windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, ol);
      void f;
    },
    catch: (cause) => make_fs_lock_error("lock", cause),
  });

// Unlock releases the exclusive lock.
export const unlock = (f: file_descriptor): Effect.Effect<void, fs_lock_error> =>
  Effect.try({
    try: () => {
      // TODO: wire to actual Windows UnlockFileEx binding.
      // const handle = windows.Handle(f.Fd());
      // const ol = new windows.Overlapped();
      // return windows.UnlockFileEx(handle, 0, 1, 0, ol);
      void f;
    },
    catch: (cause) => make_fs_lock_error("unlock", cause),
  });

/*
PORT STATUS
source path: backend/fslock/lock_windows.go
source lines: 34
draft lines: 48
confidence: medium
status: phase_a_draft
todos:
  - bind to actual Windows LockFileEx/UnlockFileEx via Bun FFI or native addon
  - reuse a shared overlapped / handle representation once FFI shape is known
  - decide whether to deduplicate file_descriptor / fs_lock_error into a shared module
notes:
  - Mirrors LockFileEx/UnlockFileEx call shape from golang.org/x/sys/windows.
  - Function names shadow lock_unix.port.ts because each platform module is loaded
    independently in the final bundler.
*/
