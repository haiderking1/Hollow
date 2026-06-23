// PORT: backend/session/delete.go

import { Effect } from "effect";
import { spawn } from "node:child_process";
import fs from "node:fs/promises";
import path from "node:path";

// DeleteResult reports how a session file was removed.
export type delete_result = {
  method: string; // "trash" or "unlink"
};

// Delete removes a session JSONL file, trying the trash CLI first like Flame.
export const delete_session = (session_path: string): Effect.Effect<delete_result, Error> => {
  return Effect.tryPromise({
    try: async () => {
      const clean_path = path.resolve(session_path);
      if (session_path.trim() === "") {
        throw new Error("empty session path");
      }
      // Verify file exists
      await fs.stat(clean_path);

      const base = path.basename(clean_path);
      const args = base.startsWith("-") ? ["--", clean_path] : [clean_path];

      let trashed = false;
      let trash_err: Error | null = null;

      try {
        const trash_proc = spawn("trash", args, { stdio: "ignore" });
        const exit_code = await new Promise<number | null>((resolve) => {
          trash_proc.on("close", resolve);
          trash_proc.on("error", () => resolve(null));
        });
        if (exit_code === 0) {
          trashed = true;
        } else {
          // Verify if it actually deleted despite non-zero exit code
          try {
            await fs.stat(clean_path);
          } catch (e: any) {
            if (e.code === "ENOENT") {
              trashed = true;
            }
          }
          if (!trashed) {
            trash_err = new Error(`trash exit code ${exit_code}`);
          }
        }
      } catch (err: any) {
        trash_err = err instanceof Error ? err : new Error(String(err));
      }

      if (trashed) {
        return { method: "trash" };
      }

      try {
        await fs.unlink(clean_path);
        return { method: "unlink" };
      } catch (unlink_err: any) {
        if (trash_err !== null) {
          throw new Error(`${unlink_err.message} (trash: ${trash_err.message})`);
        }
        throw unlink_err;
      }
    },
    catch: (cause) => cause instanceof Error ? cause : new Error(String(cause)),
  });
};

/*
PORT STATUS
source path: backend/session/delete.go
source lines: 48
draft lines: 72
confidence: high
status: phase_b_compile
*/
