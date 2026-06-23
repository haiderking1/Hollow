// PORT: backend/secrets/owner_unix.go
// backend/secrets/owner_unix.go
//go:build unix placeholder

import fs from "node:fs";
import { Effect } from "effect";
import { secrets_error, type secrets_error as secrets_error_type } from "./store";

export const verify_owner = (path: string): Effect.Effect<void, secrets_error_type> =>
  Effect.gen(function* () {
    const info = yield* Effect.try({
      try: () => fs.statSync(path),
      catch: (cause) => secrets_error("stat credentials file", cause),
    });

    if (info.uid !== (process.getuid?.() ?? info.uid)) {
      return yield* Effect.fail(
        secrets_error("credentials file is not owned by the current user", null),
      );
    }
  });

/*
PORT STATUS
source path: backend/secrets/owner_unix.go
source lines: 26
draft lines: 33
confidence: high
status: phase_a_draft
todos:
  - verify process.getuid() is available and matches Go os.Getuid on target runtime
notes:
  - Uses Node fs.Stats.uid directly instead of Go's syscall.Stat_t cast.
*/
