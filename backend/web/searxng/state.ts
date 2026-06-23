// PORT: backend/web/searxng/state.go

import path from "node:path";
import fs from "node:fs";
import { Effect } from "effect";
import { searxng_error, type searxng_error as searxng_error_type } from "./error";

export type run_state = {
  port: number;
  pid: number;
};

const state_path = (data_dir: string): string => path.join(data_dir, "run.json");

export const write_state = (
  data_dir: string,
  port: number,
  pid: number,
): Effect.Effect<void, searxng_error_type> =>
  Effect.gen(function* () {
    const data = new TextEncoder().encode(JSON.stringify({ port, pid } as run_state));
    const p = state_path(data_dir);
    yield* Effect.try({
      try: () => fs.writeFileSync(p, data, { mode: 0o600 }),
      catch: (cause) => searxng_error("write run state", cause),
    });
  });

export const read_state = (data_dir: string): [number, number, boolean] => {
  try {
    const p = state_path(data_dir);
    const data = fs.readFileSync(p);
    const st = JSON.parse(data.toString("utf8")) as run_state;
    return [st.port, st.pid, st.port > 0 && st.pid > 0];
  } catch {
    return [0, 0, false];
  }
};

/*
PORT STATUS
source path: backend/web/searxng/state.go
source lines: 36
draft lines: 52
confidence: high
status: phase_a_draft
todos:
  - none; logic is a direct port aside from using sync file helpers
notes:
  - write_state returns (error) in Go, modeled as Effect.Effect<void, searxng_error>.
  - read_state has no error return and remains a plain tuple function.
*/
