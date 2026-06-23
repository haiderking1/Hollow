// PORT: backend/agent/bash.go

import { Effect } from "effect";
import { type tool } from "../opencode/types";
import { Agent, type toolResult } from "./agent";
import { command_context } from "../shell/command";
import { resolve_safe_cwd } from "../shell/cwd";
import {
  trackDetachedChildPid,
  untrackDetachedChildPid,
  killTrackedDetachedChildren,
  installDetachedShutdownHook,
} from "../shell/process_tree";
import { bashCommandBlocked, SanitizeBashOutput } from "./bash_sanitize";
import { configureProcGroup as linuxConfigure, killProcessGroup as linuxKill } from "./bash_linux";
import { configureProcGroup as unixConfigure, killProcessGroup as unixKill } from "./bash_unix";
import { configureProcGroup as windowsConfigure, killProcessGroup as windowsKill } from "./bash_windows";
import { configureProcGroup as otherConfigure, killProcessGroup as otherKill } from "./bash_other";
import process from "node:process";

// Reap detached children when the backend exits naturally.
installDetachedShutdownHook();

const maxBashOutput = 32000;
const exitStdioGrace = 100; // ms
const bashUpdateThrottle = 100; // ms
const truncMarker = "\n... truncated ...";

function configureProcGroup(cmd: any): void {
  if (process.platform === "linux") {
    linuxConfigure(cmd);
  } else if (process.platform === "win32") {
    windowsConfigure(cmd);
  } else if (
    process.platform === "darwin" ||
    process.platform === "freebsd" ||
    process.platform === "openbsd" ||
    process.platform === "netbsd"
  ) {
    unixConfigure(cmd);
  } else {
    otherConfigure(cmd);
  }
}

function killProcessGroup(cmd: any): Error | null {
  if (process.platform === "linux") {
    return linuxKill(cmd);
  } else if (process.platform === "win32") {
    return windowsKill(cmd);
  } else if (
    process.platform === "darwin" ||
    process.platform === "freebsd" ||
    process.platform === "openbsd" ||
    process.platform === "netbsd"
  ) {
    return unixKill(cmd);
  } else {
    return otherKill(cmd);
  }
}

export function bashTool(): tool {
  const schema = {
    type: "object",
    properties: {
      command: { type: "string" },
    },
    required: ["command"],
  };
  return {
    type: "function",
    function: {
      name: "bash",
      description: "Run a shell command in the project workspace. Do NOT run mpv, sixel, blessed, or full-screen TUI apps — they break the Hollow terminal. Use curl, tests, and plain-text commands only.",
      parameters: new TextEncoder().encode(JSON.stringify(schema)),
    },
  };
}

Agent.prototype.toolBash = function (
  this: Agent,
  ctx: AbortSignal,
  id: string,
  argsJSON: string
): Effect.Effect<toolResult, Error> {
  let args: { command: string };
  try {
    args = JSON.parse(argsJSON);
  } catch (err) {
    return Effect.succeed({ output: err instanceof Error ? err.message : String(err), isErr: true });
  }

  const blocked = bashCommandBlocked(args.command);
  if (blocked !== "") {
    return Effect.succeed({ output: blocked, isErr: true });
  }

  const safeDir = resolve_safe_cwd(this.workDir);
  const commandEff = command_context(ctx, args.command, true, safeDir);

  return commandEff.pipe(
    Effect.flatMap((child) => {
      configureProcGroup(child);
      if (child.pid) trackDetachedChildPid(child.pid);

      const delta = new BashDeltaEmitter((chunk) => {
        const [clean] = SanitizeBashOutput(chunk);
        if (clean !== "") {
          try {
            if (this.emit) {
              this.emit({ kind: "tool_delta", data: { id, chunk: clean } });
            }
          } catch {}
        }
      });

      const sw = new BashStreamWriter(maxBashOutput, (chunk) => delta.add(chunk));

      child.stdout?.on("data", (chunk: Buffer) => {
        sw.write(chunk.toString("utf8"));
      });
      child.stderr?.on("data", (chunk: Buffer) => {
        sw.write(chunk.toString("utf8"));
      });

      const started = Date.now();
      this.registerBashCmd(child);

      // waitForChildProcess: resolve once the process has exited AND its stdio
      // has ended, with a short grace backstop so inherited stdio handles held
      // by detached descendants can't hang us forever. Mirrors Flame's
      // utils/child-process.ts waitForChildProcess.
      return Effect.async<toolResult, Error>((resume) => {
        let settled = false;
        let exited = false;
        let exitCode: number | null = null;
        let stdoutEnded = child.stdout === null || child.stdout === undefined;
        let stderrEnded = child.stderr === null || child.stderr === undefined;
        let postExitTimer: NodeJS.Timeout | undefined;

        const cleanup = () => {
          if (postExitTimer) {
            clearTimeout(postExitTimer);
            postExitTimer = undefined;
          }
          child.removeListener("error", onError);
          child.removeListener("exit", onExit);
          child.removeListener("close", onClose);
          child.stdout?.removeListener("end", onStdoutEnd);
          child.stderr?.removeListener("end", onStderrEnd);
          ctx.removeEventListener("abort", onAbort);
          if (child.pid) untrackDetachedChildPid(child.pid);
          this.unregisterBashCmd(child);
        };

        const finalize = (code: number | null) => {
          if (settled) return;
          settled = true;
          cleanup();
          sw.finalize();
          delta.flush();
          child.stdout?.destroy();
          child.stderr?.destroy();

          const duration = Date.now() - started;
          const [text] = SanitizeBashOutput(sw.toString());

          if (ctx.aborted) {
            const out = text === "" ? "" : text.endsWith("\n") ? text : text + "\n";
            resume(Effect.succeed({ output: out + "Command aborted", isErr: true }));
            return;
          }

          this.recordCommandRun(args.command, code ?? -1, text, duration);

          if (code !== null && code !== 0) {
            const out = text === "" ? "" : text.endsWith("\n") ? text : text + "\n";
            resume(Effect.succeed({ output: out + `Command exited with code ${code}`, isErr: true }));
            return;
          }
          resume(Effect.succeed({ output: text }));
        };

        const maybeFinalizeAfterExit = () => {
          if (!exited || settled) return;
          if (stdoutEnded && stderrEnded) finalize(exitCode);
        };

        const onStdoutEnd = () => {
          stdoutEnded = true;
          maybeFinalizeAfterExit();
        };
        const onStderrEnd = () => {
          stderrEnded = true;
          maybeFinalizeAfterExit();
        };
        const onExit = (code: number | null) => {
          exited = true;
          exitCode = code;
          maybeFinalizeAfterExit();
          if (!settled) {
            postExitTimer = setTimeout(() => finalize(code), exitStdioGrace);
          }
        };
        const onClose = (code: number | null) => finalize(code);
        const onError = (err: Error) => {
          if (settled) return;
          settled = true;
          cleanup();
          resume(Effect.succeed({ output: err.message, isErr: true }));
        };
        const onAbort = () => {
          killProcessGroup(child);
          finalize(exitCode);
        };

        child.stdout?.once("end", onStdoutEnd);
        child.stderr?.once("end", onStderrEnd);
        child.once("error", onError);
        child.once("exit", onExit);
        child.once("close", onClose);
        if (ctx.aborted) {
          onAbort();
        } else {
          ctx.addEventListener("abort", onAbort);
        }
      });
    }),
    Effect.catchAll((err: any) =>
      Effect.succeed({ output: err instanceof Error ? err.message : String(err), isErr: true })
    )
  );
};

Agent.prototype.registerBashCmd = function (this: Agent, cmd: any) {
  this.activeBashCmd = cmd;
};

Agent.prototype.unregisterBashCmd = function (this: Agent, cmd: any) {
  if (this.activeBashCmd === cmd) {
    this.activeBashCmd = null;
  }
};

Agent.prototype.killActiveBash = function (this: Agent) {
  const cmd = this.activeBashCmd;
  if (cmd !== null) {
    killProcessGroup(cmd);
  }
  // Reap any other detached children still tracked (e.g. descendants that
  // outlived their parent). The active cmd is also in the set; re-killing a
  // dead pid is a no-op.
  killTrackedDetachedChildren();
};

export class BashStreamWriter {
  private buf: string[] = [];
  private max: number;
  private total = 0;
  private truncated = false;
  private onChunk?: (chunk: string) => void;
  private currentLen = 0;

  constructor(max: number, onChunk?: (chunk: string) => void) {
    this.max = max;
    this.onChunk = onChunk;
  }

  write(chunk: string): void {
    this.total += chunk.length;
    let emit = "";
    if (!this.truncated) {
      const room = this.max - this.currentLen;
      if (room <= 0) {
        this.truncated = true;
        this.buf.push(truncMarker);
        this.currentLen += truncMarker.length;
        emit = truncMarker;
      } else if (chunk.length <= room) {
        const [clean] = SanitizeBashOutput(chunk);
        this.buf.push(clean);
        this.currentLen += clean.length;
        emit = clean;
      } else {
        const [clean] = SanitizeBashOutput(chunk.slice(0, room));
        this.buf.push(clean);
        this.buf.push(truncMarker);
        this.currentLen += clean.length + truncMarker.length;
        this.truncated = true;
        emit = clean + truncMarker;
      }
    }
    if (emit !== "" && this.onChunk) {
      this.onChunk(emit);
    }
  }

  finalize(): void {
    let emit = "";
    if (this.total > this.max && !this.truncated) {
      this.truncated = true;
      this.buf.push(truncMarker);
      this.currentLen += truncMarker.length;
      emit = truncMarker;
    }
    if (emit !== "" && this.onChunk) {
      this.onChunk(emit);
    }
  }

  toString(): string {
    return this.buf.join("");
  }
}

export class BashDeltaEmitter {
  private emit: (chunk: string) => void;
  private pending: string[] = [];
  private lastAt = 0;
  private timer: NodeJS.Timeout | null = null;

  constructor(emit: (chunk: string) => void) {
    this.emit = emit;
  }

  add(chunk: string): void {
    if (chunk === "") {
      return;
    }
    this.pending.push(chunk);
    const now = Date.now();
    const delay = bashUpdateThrottle - (now - this.lastAt);
    if (delay <= 0) {
      this.flushLocked();
      return;
    }
    if (this.timer === null) {
      this.timer = setTimeout(() => {
        this.timer = null;
        this.flushLocked();
      }, delay);
    }
  }

  flush(): void {
    if (this.timer !== null) {
      clearTimeout(this.timer);
      this.timer = null;
    }
    this.flushLocked();
  }

  private flushLocked(): void {
    if (this.pending.length === 0) {
      return;
    }
    this.emit(this.pending.join(""));
    this.pending = [];
    this.lastAt = Date.now();
  }
}

/*
PORT STATUS
source path: backend/agent/bash.go
source lines: 304
draft lines: 279
confidence: high
status: phase_b_compile
*/
