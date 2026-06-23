// PORT: backend/skills/preprocessing.go

import { Effect } from "effect";
import path from "node:path";
import { spawn } from "node:child_process";
import { command_context, git_root_from_bash_exe } from "../shell/command";
import { resolve_safe_cwd } from "../shell/cwd";
import { resolve_bash } from "../shell/resolve";
import { InlineShellMaxOutput } from "./constants";

const inlineShellRe = /!`([^`\n]+)`/g;

export type PreprocessingConfig = {
  TemplateVars: boolean;
  InlineShell: boolean;
  InlineShellTimeout: number;
};

export function DefaultInlineShellEnabled(): boolean {
  return process.platform === "linux" || process.platform === "win32";
}

export function DefaultPreprocessingConfig(): PreprocessingConfig {
  return {
    TemplateVars: true,
    InlineShell: DefaultInlineShellEnabled(),
    InlineShellTimeout: 10,
  };
}

function substituteTemplateVars(content: string, skillDir: string, sessionId: string): string {
  let res = content;
  if (skillDir !== "") {
    res = res.replaceAll("${HOLLOW_SKILL_DIR}", skillDir);
    res = res.replaceAll("${HERMES_SKILL_DIR}", skillDir);
    res = res.replaceAll("${FLAME_SKILL_DIR}", skillDir);
  }
  if (sessionId !== "") {
    res = res.replaceAll("${HOLLOW_SESSION_ID}", sessionId);
    res = res.replaceAll("${HERMES_SESSION_ID}", sessionId);
    res = res.replaceAll("${FLAME_SESSION_ID}", sessionId);
  }
  return res;
}

function runInlineShell(command: string, cwd: string, timeoutSec: number): Effect.Effect<string, never> {
  if (process.platform !== "linux" && process.platform !== "win32") {
    return Effect.succeed("[inline-shell error: inline shell execution is supported on Linux and Windows only]");
  }

  const limit = timeoutSec <= 0 ? 10 : timeoutSec;
  const safeCwd = resolve_safe_cwd(cwd);

  return resolve_bash().pipe(
    Effect.flatMap((bash_exe) => {
      const args = ["-c", command];
      const env: NodeJS.ProcessEnv = {
        ...process.env,
        TERM: "dumb",
        NO_COLOR: "1",
        CLICOLOR: "0",
        FORCE_COLOR: "0",
      };

      if (process.platform === "win32") {
        const bash_abs = path.resolve(bash_exe);
        const git_root = git_root_from_bash_exe(bash_abs);
        const new_entries = [
          path.join(git_root, "cmd"),
          path.join(git_root, "bin"),
          path.join(git_root, "usr", "bin"),
        ];
        const path_key = Object.keys(env).find((k) => k.toLowerCase() === "path") ?? "PATH";
        const path_val = env[path_key] ?? "";
        env[path_key] = path_val !== "" ? `${new_entries.join(";")};${path_val}` : new_entries.join(";");
      }

      return Effect.async<string, never>((resume) => {
        const controller = new AbortController();
        const timer = setTimeout(() => {
          controller.abort();
        }, limit * 1000);

        const cleanup = () => {
          clearTimeout(timer);
        };

        try {
          const child = spawn(bash_exe, args, {
            env,
            windowsHide: process.platform === "win32",
            cwd: safeCwd,
            signal: controller.signal,
          });

          let stdout = "";
          let stderr = "";

          child.stdout?.on("data", (chunk) => {
            stdout += chunk;
          });

          child.stderr?.on("data", (chunk) => {
            stderr += chunk;
          });

          child.on("close", (code) => {
            cleanup();
            if (controller.signal.aborted) {
              resume(Effect.succeed(`[inline-shell timeout after ${limit}s: ${command}]`));
              return;
            }

            if (code !== 0) {
              let stderrStr = stderr.trimEnd();
              if (stderrStr !== "") {
                if (stderrStr.length > InlineShellMaxOutput) {
                  stderrStr = stderrStr.slice(0, InlineShellMaxOutput) + "...[truncated]";
                }
                resume(Effect.succeed(stderrStr));
                return;
              }
              resume(Effect.succeed(`[inline-shell error: exit code ${code}]`));
              return;
            }

            let out = stdout.trimEnd();
            if (out === "" && stderr.length > 0) {
              out = stderr.trimEnd();
            }

            if (out.length > InlineShellMaxOutput) {
              out = out.slice(0, InlineShellMaxOutput) + "...[truncated]";
            }

            resume(Effect.succeed(out));
          });

          child.on("error", (err) => {
            cleanup();
            if (controller.signal.aborted) {
              resume(Effect.succeed(`[inline-shell timeout after ${limit}s: ${command}]`));
              return;
            }
            resume(Effect.succeed(`[inline-shell error: ${err.message}]`));
          });
        } catch (err: any) {
          cleanup();
          resume(Effect.succeed(`[inline-shell error: ${err.message || String(err)}]`));
        }
      });
    }),
    Effect.catchAll((err) => {
      const msg = typeof err === "object" && err !== null && "reason" in err ? (err as any).reason : String(err);
      return Effect.succeed(`[inline-shell error: ${msg}]`);
    })
  );
}

function expandInlineShell(content: string, skillDir: string, timeoutSec: number): Effect.Effect<string, never> {
  if (!content.includes("!`")) {
    return Effect.succeed(content);
  }

  const matches: string[] = [];
  let m;
  inlineShellRe.lastIndex = 0;
  while ((m = inlineShellRe.exec(content)) !== null) {
    matches.push(m[1]);
  }

  if (matches.length === 0) {
    return Effect.succeed(content);
  }

  const tasks = matches.map((cmd) => {
    const trimmed = cmd.trim();
    if (trimmed === "") {
      return Effect.succeed("");
    }
    return runInlineShell(trimmed, skillDir, timeoutSec);
  });

  return Effect.all(tasks).pipe(
    Effect.map((results) => {
      let idx = 0;
      return content.replace(inlineShellRe, () => {
        return results[idx++];
      });
    })
  );
}

export function PreprocessSkillContent(
  content: string,
  skillDir: string,
  sessionId: string,
  inlineShellEnabled: boolean,
  timeoutSec: number,
): Effect.Effect<string, never> {
  if (content === "") {
    return Effect.succeed(content);
  }

  const substituted = substituteTemplateVars(content, skillDir, sessionId);
  if (inlineShellEnabled) {
    const limit = timeoutSec <= 0 ? 10 : timeoutSec;
    return expandInlineShell(substituted, skillDir, limit);
  }
  return Effect.succeed(substituted);
}

/*
PORT STATUS
source path: backend/skills/preprocessing.go
source lines: 127
draft lines: 198
confidence: high
status: phase_b_compile
*/
