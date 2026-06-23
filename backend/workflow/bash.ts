// PORT: backend/workflow/bash.go

import { Effect } from "effect";
import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { Runtime } from "./runtime";
import { type BashResult } from "./types";
import { command_context } from "../shell/command";
import { resolve_safe_cwd } from "../shell/cwd";
import { killProcessTree } from "../shell/process_tree";

const maxWorkflowBashOutput = 64 * 1024;

export function runBash(
  this: Runtime,
  ctx: AbortSignal,
  command: string
): Effect.Effect<BashResult, Error> {
  if (command === "") {
    return Effect.fail(new Error("runBash command is empty"));
  }

  const safeDir = resolve_safe_cwd(this.workDir);
  const commandEff = command_context(ctx, command, true, safeDir).pipe(
    Effect.mapError((err) => (err instanceof Error ? err : new Error(String(err))))
  );

  return commandEff.pipe(
    Effect.flatMap((child) =>
      Effect.async<BashResult, Error>((resume) => {
        const stdoutChunks: Buffer[] = [];
        const stderrChunks: Buffer[] = [];

        child.stdout?.on("data", (chunk: Buffer) => {
          stdoutChunks.push(chunk);
        });
        child.stderr?.on("data", (chunk: Buffer) => {
          stderrChunks.push(chunk);
        });

        let completed = false;
        let onAbort: () => void;

        const cleanUp = () => {
          if (completed) return false;
          completed = true;
          ctx.removeEventListener("abort", onAbort);
          return true;
        };

        const finish = (code: number | null, signal: string | null) => {
          if (!cleanUp()) return;

          const stdoutBuf = Buffer.concat(stdoutChunks);
          const stderrBuf = Buffer.concat(stderrChunks);
          const stdoutStr = stdoutBuf.toString("utf8");
          const stderrStr = stderrBuf.toString("utf8");
          const exitCode = code !== null ? code : -1;

          const combined = Buffer.concat([stdoutBuf, stderrBuf]);
          const result: BashResult = {
            stdout: stdoutStr,
            stderr: stderrStr,
            exitCode: exitCode,
          };

          if (combined.length > maxWorkflowBashOutput) {
            const hash = crypto.createHash("sha256").update(combined).digest("hex");
            result.sha256 = hash;
            result.truncated = true;

            const dir = path.join(path.dirname(this.snapshot.scriptPath), "artifacts");
            try {
              fs.mkdirSync(dir, { recursive: true, mode: 0o755 });
            } catch {}

            const logPath = path.join(dir, `bash-${hash.slice(0, 16)}.log`);
            try {
              fs.writeFileSync(logPath, combined, { mode: 0o600 });
            } catch {}

            result.fullOutputPath = logPath;

            const halfMax = Math.floor(maxWorkflowBashOutput / 2);
            if (stdoutStr.length > halfMax) {
              result.stdout = stdoutStr.slice(0, halfMax) + "\n... truncated; full output: " + logPath;
            }
            if (stderrStr.length > halfMax) {
              result.stderr = stderrStr.slice(0, halfMax) + "\n... truncated; full output: " + logPath;
            }
          }

          resume(Effect.succeed(result));
        };

        child.on("exit", (code, signal) => {
          finish(code, signal);
        });

        child.on("close", (code, signal) => {
          finish(code, signal);
        });

        child.on("error", (err) => {
          if (!cleanUp()) return;
          resume(Effect.fail(err));
        });

        onAbort = () => {
          if (!cleanUp()) return;
          if (child.pid) killProcessTree(child.pid);
          resume(Effect.fail(new Error("interrupted")));
        };

        if (ctx.aborted) {
          onAbort();
        } else {
          ctx.addEventListener("abort", onAbort);
        }
      })
    )
  );
}

(Runtime.prototype as any).runBash = runBash;

/*
PORT STATUS
source path: backend/workflow/bash.go
source lines: 61
draft lines: 111
confidence: high
status: phase_b_compile
*/
