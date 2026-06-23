// PORT: backend/session/delete_cwd.go

import { Effect } from "effect";
import path from "node:path";
import { delete_session } from "./delete";
import { list_for_cwd } from "./list";

// DeleteForCWD removes all session JSONL files for a project directory.
// skipPath, if non-empty, is left on disk (e.g. the active session).
export const delete_for_cwd = (cwd: string, skip_path: string): Effect.Effect<number, Error> => {
  return Effect.gen(function* () {
    const infos = yield* list_for_cwd(cwd);
    const clean_skip = skip_path ? path.resolve(skip_path) : "";
    let deleted = 0;
    for (const info of infos) {
      if (clean_skip !== "" && path.resolve(info.path) === clean_skip) {
        continue;
      }
      const res = yield* Effect.either(delete_session(info.path));
      if (res._tag === "Left") {
        return yield* Effect.fail(res.left);
      }
      deleted++;
    }
    return deleted;
  });
};

/*
PORT STATUS
source path: backend/session/delete_cwd.go
source lines: 26
draft lines: 29
confidence: high
status: phase_b_compile
*/
