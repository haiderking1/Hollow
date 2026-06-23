// PORT: backend/secrets/owner_other.go
// backend/secrets/owner_other.go
//go:build !unix placeholder

import fs from "node:fs";
import { Effect } from "effect";
import { secrets_error, type secrets_error as secrets_error_type } from "./store";

export const verify_owner = (path: string): Effect.Effect<void, secrets_error_type> =>
  Effect.gen(function* () {
    yield* Effect.try({
      try: () => fs.statSync(path),
      catch: (cause) => secrets_error("stat credentials file", cause),
    });
  });

/*
PORT STATUS
source path: backend/secrets/owner_other.go
source lines: 10
draft lines: 27
confidence: high
status: phase_a_draft
todos:
  - no owner check on non-Unix platforms; confirm this matches Go behavior
notes:
  - Minimal port that only verifies the file exists.
*/
