// PORT: backend/browser/launch.go

import { Effect } from "effect";
import { spawn } from "node:child_process";
import fs from "node:fs/promises";
import path from "node:path";
import { home_agent_dir } from "../session/paths";
import { detachProcess as detachUnix } from "./launch_unix";
import { detachProcess as detachWindows } from "./launch_windows";

export const LaunchTimeoutMS = 20000;
export const LaunchPollMS = 250;
export const DefaultCDPPort = 9222;

const launch_promises = new Map<string, Promise<void>>();

export function reset_browser_launch_state_for_tests(): void {
  launch_promises.clear();
}

export function should_auto_launch_browser(): boolean {
  return process.env.HOLLOW_BROWSER_AUTO_LAUNCH !== "0";
}

export function get_browser_profile_dir(): Effect.Effect<string, Error> {
  const override = (process.env.HOLLOW_BROWSER_PROFILE_DIR || "").trim();
  if (override !== "") {
    return Effect.tryPromise({
      try: async () => {
        await fs.mkdir(override, { recursive: true, mode: 0o700 });
        return override;
      },
      catch: (cause) => (cause instanceof Error ? cause : new Error(String(cause))),
    });
  }
  return home_agent_dir().pipe(
    Effect.flatMap((home) => {
      const profileDir = path.join(home, "browser-profile");
      return Effect.tryPromise({
        try: async () => {
          await fs.mkdir(profileDir, { recursive: true, mode: 0o700 });
          return profileDir;
        },
        catch: (cause) => (cause instanceof Error ? cause : new Error(String(cause))),
      });
    })
  );
}

export function parse_cdp_port(baseUrl: string): number {
  try {
    const u = new URL(baseUrl);
    if (!u.port) {
      return DefaultCDPPort;
    }
    const port = parseInt(u.port, 10);
    if (isNaN(port)) {
      return DefaultCDPPort;
    }
    return port;
  } catch {
    return DefaultCDPPort;
  }
}

async function command_exists(cmd: string): Promise<boolean> {
  const paths = (process.env.PATH ?? "").split(path.delimiter);
  for (const dir of paths) {
    const candidate = path.join(dir, cmd);
    try {
      const s = await fs.stat(candidate);
      if (s.isFile()) return true;
    } catch {}
    if (process.platform === "win32") {
      const exe = candidate.endsWith(".exe") ? candidate : `${candidate}.exe`;
      try {
        const s = await fs.stat(exe);
        if (s.isFile()) return true;
      } catch {}
    }
  }
  return false;
}

export function resolve_browser_executable(): Effect.Effect<string, Error> {
  return Effect.tryPromise({
    try: async () => {
      const override = (process.env.HOLLOW_BROWSER_EXECUTABLE || "").trim();
      if (override !== "") {
        try {
          await fs.stat(override);
          return override;
        } catch {
          throw new Error(`HOLLOW_BROWSER_EXECUTABLE not found: ${override}`);
        }
      }

      const goos = process.platform;
      if (goos === "win32") {
        const programFiles = process.env.ProgramFiles || "C:\\Program Files";
        const programFilesX86 = process.env["ProgramFiles(x86)"] || "C:\\Program Files (x86)";
        const candidates = [
          path.join(programFiles, "Google", "Chrome", "Application", "chrome.exe"),
          path.join(programFilesX86, "Google", "Chrome", "Application", "chrome.exe"),
          path.join(programFiles, "Microsoft", "Edge", "Application", "msedge.exe"),
          path.join(programFilesX86, "Microsoft", "Edge", "Application", "msedge.exe"),
        ];
        for (const c of candidates) {
          try {
            await fs.stat(c);
            return c;
          } catch {}
        }
      } else if (goos === "darwin") {
        const candidates = [
          "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
          "/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
          "/Applications/Chromium.app/Contents/MacOS/Chromium",
        ];
        for (const c of candidates) {
          try {
            await fs.stat(c);
            return c;
          } catch {}
        }
      } else if (goos === "linux") {
        const commands = [
          "google-chrome-stable",
          "google-chrome",
          "chromium-browser",
          "chromium",
          "microsoft-edge",
        ];
        for (const cmd of commands) {
          if (await command_exists(cmd)) {
            return cmd;
          }
        }
        return commands[0];
      }

      throw new Error(
        "No Chrome or Edge executable found. Install Chrome/Edge or set HOLLOW_BROWSER_EXECUTABLE to the browser binary path."
      );
    },
    catch: (cause) => (cause instanceof Error ? cause : new Error(String(cause))),
  });
}

export function is_cdp_connection_error(err: Error | null | undefined): boolean {
  if (!err) {
    return false;
  }
  const msg = err.message.toLowerCase();
  return (
    msg.includes("fetch failed") ||
    msg.includes("econnrefused") ||
    msg.includes("connection refused") ||
    msg.includes("unable to connect") ||
    msg.includes("network") ||
    msg.includes("timed out")
  );
}

export function format_cdp_connection_error(
  baseUrl: string,
  err: Error | null,
  launched: boolean,
): Effect.Effect<string, never> {
  return get_browser_profile_dir().pipe(
    Effect.match({
      onFailure: () => "",
      onSuccess: (dir) => dir,
    }),
    Effect.map((profileDir) => {
      const detail = err ? err.message : "";
      const manual = `Start Chrome/Edge manually, e.g. chrome --remote-debugging-port=${parse_cdp_port(
        baseUrl
      )} --user-data-dir="${profileDir}"`;
      if (launched) {
        return `Could not connect to browser CDP at ${baseUrl} after auto-launch (${detail}). ${manual}`;
      }
      if (should_auto_launch_browser()) {
        return `Could not connect to browser CDP at ${baseUrl} (${detail}). Auto-launch was attempted but failed. ${manual}`;
      }
      return `Could not connect to browser CDP at ${baseUrl} (${detail}). Set HOLLOW_BROWSER_AUTO_LAUNCH=1 (default) or ${manual}`;
    })
  );
}

export function wait_for_cdp_ready(
  baseUrl: string,
  timeoutMs: number,
): Effect.Effect<boolean, Error> {
  return Effect.tryPromise({
    try: async () => {
      const u = new URL(baseUrl);
      u.pathname = "/json/version";
      const versionUrl = u.toString();

      const deadline = Date.now() + timeoutMs;
      while (Date.now() < deadline) {
        try {
          const controller = new AbortController();
          const id = setTimeout(() => controller.abort(), 2000);
          const resp = await fetch(versionUrl, { signal: controller.signal });
          clearTimeout(id);
          if (resp.status === 200) {
            return true;
          }
        } catch {}
        await new Promise((resolve) => setTimeout(resolve, LaunchPollMS));
      }
      return false;
    },
    catch: (cause) => (cause instanceof Error ? cause : new Error(String(cause))),
  });
}

function detach_process_plat(opts: any): void {
  if (process.platform === "win32") {
    detachWindows(opts);
  } else {
    detachUnix(opts);
  }
}

export function launch_browser_once(baseUrl: string): Effect.Effect<void, Error> {
  return resolve_browser_executable().pipe(
    Effect.flatMap((executable) =>
      get_browser_profile_dir().pipe(
        Effect.flatMap((profileDir) => {
          const port = parse_cdp_port(baseUrl);
          const args = [
            `--remote-debugging-port=${port}`,
            `--user-data-dir=${profileDir}`,
            "--no-first-run",
            "--no-default-browser-check",
            "about:blank",
          ];

          return Effect.tryPromise({
            try: async () => {
              const opts: any = {
                stdio: "ignore",
              };
              detach_process_plat(opts);

              const child = spawn(executable, args, opts);
              child.unref();
            },
            catch: (cause) => (cause instanceof Error ? cause : new Error(String(cause))),
          });
        })
      )
    ),
    Effect.flatMap(() =>
      wait_for_cdp_ready(baseUrl, LaunchTimeoutMS).pipe(
        Effect.flatMap((ready) => {
          if (!ready) {
            return Effect.fail(
              new Error(`CDP did not become ready on ${baseUrl} within ${LaunchTimeoutMS}ms`)
            );
          }
          return Effect.void;
        })
      )
    )
  );
}

export function ensure_browser_launched(baseUrl: string): Effect.Effect<boolean, Error> {
  if (!should_auto_launch_browser()) {
    return Effect.succeed(false);
  }

  return Effect.tryPromise({
    try: async () => {
      let p = launch_promises.get(baseUrl);
      if (!p) {
        p = Effect.runPromise(launch_browser_once(baseUrl));
        launch_promises.set(baseUrl, p);
      }
      await p;
      return true;
    },
    catch: (cause) => (cause instanceof Error ? cause : new Error(String(cause))),
  });
}

/*
PORT STATUS
source path: backend/browser/launch.go
source lines: 256
draft lines: 247
confidence: high
status: phase_b_compile
*/
