// PORT: mirrors backend/web/searxng/manager.go

import path from "node:path";
import fs from "node:fs";
import { spawn, spawnSync, type ChildProcess } from "node:child_process";
import net from "node:net";
import process from "node:process";
import { Effect } from "effect";
import { searxng_error, type searxng_error as searxng_error_type } from "./error";
import { write_state, read_state } from "./state";

// Load real settings.yml or fallback with secret_key
const default_settings = (): Uint8Array => {
  try {
    const p = path.join(__dirname, "settings.yml");
    if (fs.existsSync(p)) {
      return fs.readFileSync(p);
    }
  } catch {}
  return new TextEncoder().encode("port: 18752\nserver:\n  secret_key: \"enough-local-searxng\"\n");
};

const repo_url = "https://github.com/searxng/searxng.git";
const health_timeout_ms = 90_000;
const health_interval_ms = 400;

let manager_instance: manager | null = null;
let manager_initialized = false;

// Manager runs a local SearXNG instance for Enough.
export class manager {
  private _cmd: ChildProcess | null = null;
  private _base_url = "";
  private readonly _data_dir: string;

  constructor(data_dir: string) {
    this._data_dir = data_dir;
  }

  get data_dir(): string {
    return this._data_dir;
  }

  get base_url(): string {
    return this._base_url;
  }

  // EnsureRunning installs (if needed), starts SearXNG, and returns its base URL.
  ensure_running(ctx: AbortSignal): Effect.Effect<string, searxng_error_type> {
    const self = this;
    return Effect.gen(function* () {
      if (self._base_url !== "" && (yield* self.health_ok(ctx, self._base_url))) {
        return self._base_url;
      }

      if (self._data_dir === "") {
        return yield* Effect.fail(searxng_error("searxng data dir unavailable", null));
      }

      const [existing_base, ok] = self.reuse_existing(ctx);
      if (ok) {
        self._base_url = existing_base;
        return existing_base;
      }

      yield* self.ensure_installed(ctx);

      const port = yield* free_port();
      const settings_path = yield* self.write_settings(port);
      const base_url = `http://127.0.0.1:${port}`;
      const python = path.join(self._data_dir, "venv", "bin", "python");
      const src_dir = path.join(self._data_dir, "src");

      self._cmd = yield* Effect.try({
        try: () =>
          spawn(python, ["-m", "searx.webapp"], {
            cwd: src_dir,
            env: {
              ...process.env,
              SEARXNG_SETTINGS_PATH: settings_path,
              SEARXNG_BASE_URL: base_url + "/",
            },
            stdio: "ignore",
          }),
        catch: (cause) => searxng_error("start searxng", cause),
      });

      yield* wait_healthy(ctx, base_url).pipe(
        Effect.catchAll((err) =>
          Effect.gen(function* () {
            yield* self.stop_locked();
            return yield* Effect.fail(err);
          }),
        ),
      );

      self._base_url = base_url;
      yield* write_state(self._data_dir, port, self._cmd.pid ?? 0);
      return base_url;
    });
  }

  // Stop shuts down a SearXNG process started by Enough.
  stop(): Effect.Effect<void, searxng_error_type> {
    const self = this;
    return Effect.gen(function* () {
      yield* self.stop_locked();
    });
  }

  private stop_locked(): Effect.Effect<void, searxng_error_type> {
    const self = this;
    return Effect.promise(async () => {
      if (self._cmd !== null && self._cmd.pid !== undefined && self._cmd.pid > 0) {
        try {
          process.kill(self._cmd.pid, "SIGTERM");
        } catch {
          // ignore
        }
        const done = new Promise<void>((resolve) => {
          self._cmd?.on("exit", () => resolve());
          setTimeout(() => resolve(), 5_000);
        });
        await done;
        if (self._cmd?.pid !== undefined && self._cmd.pid > 0) {
          try {
            process.kill(self._cmd.pid, "SIGKILL");
          } catch {
            // ignore
          }
        }
      }
      self._cmd = null;
      self._base_url = "";
    }).pipe(Effect.catchAll((cause) => Effect.fail(searxng_error("stop searxng", cause))));
  }

  private ensure_installed(ctx: AbortSignal): Effect.Effect<void, searxng_error_type> {
    const self = this;
    return Effect.gen(function* () {
      const src_dir = path.join(self._data_dir, "src");
      const webapp = path.join(src_dir, "searx", "webapp.py");
      if (fs.existsSync(webapp)) {
        yield* self.ensure_venv(ctx);
        return;
      }

      yield* Effect.try({
        try: () => fs.mkdirSync(self._data_dir, { recursive: true, mode: 0o700 }),
        catch: (cause) => searxng_error("create searxng data dir", cause),
      });

      if (look_path("git") === null) {
        return yield* Effect.fail(searxng_error("searxng install requires git", null));
      }
      if (look_path("python3") === null) {
        return yield* Effect.fail(searxng_error("searxng install requires python3", null));
      }

      yield* Effect.try({
        try: () => {
          const result = spawnSync("git", ["clone", "--depth", "1", repo_url, src_dir], {
            stdio: "ignore",
          });
          if (result.status !== 0) {
            throw new Error(`git clone exited with status ${result.status ?? "unknown"}`);
          }
        },
        catch: (cause) => searxng_error("clone searxng", cause),
      });

      yield* self.ensure_venv(ctx);
    });
  }

  private ensure_venv(_ctx: AbortSignal): Effect.Effect<void, searxng_error_type> {
    const self = this;
    return Effect.gen(function* () {
      const venv_python = path.join(self._data_dir, "venv", "bin", "python");
      if (fs.existsSync(venv_python)) {
        return;
      }

      const py3 = look_path("python3");
      if (py3 === null) {
        return yield* Effect.fail(searxng_error("python3 not found", null));
      }

      const venv_dir = path.join(self._data_dir, "venv");
      yield* Effect.try({
        try: () => {
          const result = spawnSync(py3, ["-m", "venv", venv_dir], { encoding: "utf8" });
          if (result.status !== 0) {
            throw new Error(`create venv failed: ${result.stderr ?? ""}`);
          }
        },
        catch: (cause) => searxng_error("create venv", cause),
      });

      const pip = path.join(venv_dir, "bin", "pip");
      const reqs = path.join(self._data_dir, "src", "requirements.txt");
      yield* Effect.try({
        try: () => {
          const result = spawnSync(pip, ["install", "-r", reqs], { stdio: "ignore" });
          if (result.status !== 0) {
            throw new Error(`pip install exited with status ${result.status ?? "unknown"}`);
          }
        },
        catch: (cause) =>
          searxng_error("install searxng dependencies (this may take a minute)", cause),
      });
    });
  }

  private write_settings(port: number): Effect.Effect<string, searxng_error_type> {
    const self = this;
    return Effect.gen(function* () {
      const text = new TextDecoder()
        .decode(default_settings())
        .replaceAll("port: 18752", `port: ${port.toString()}`);
      const p = path.join(self._data_dir, "settings.yml");
      yield* Effect.try({
        try: () => fs.writeFileSync(p, text, { mode: 0o600 }),
        catch: (cause) => searxng_error("write settings", cause),
      });
      return p;
    });
  }

  private reuse_existing(ctx: AbortSignal): [string, boolean] {
    const [port, pid, ok] = read_state(this._data_dir);
    if (!ok) {
      return ["", false];
    }
    if (!process_alive(pid)) {
      return ["", false];
    }
    const base = `http://127.0.0.1:${port}`;
    try {
      const alive = Effect.runSync(this.health_ok(ctx, base));
      return alive ? [base, true] : ["", false];
    } catch {
      return ["", false];
    }
  }

  private health_ok(ctx: AbortSignal, base: string): Effect.Effect<boolean, never> {
    return Effect.promise(() => health_ok_sync(ctx, base));
  }
}

const compute_data_dir = (): string => {
  const home = process.env.HOME ?? process.env.USERPROFILE ?? "";
  if (home === "") {
    return "";
  }
  return path.join(home, ".local", "share", "hollow", "searxng");
};

// Default returns the shared bundled SearXNG manager.
export const default_manager = (): manager => {
  if (!manager_initialized) {
    const dir = compute_data_dir();
    manager_instance = dir !== "" ? new manager(dir) : new manager("");
    manager_initialized = true;
  }
  return manager_instance!;
};

// EnsureRunning installs (if needed), starts SearXNG, and returns its base URL.
export const ensure_running = (
  ctx: AbortSignal,
): Effect.Effect<string, searxng_error_type> => default_manager().ensure_running(ctx);

// Stop shuts down a SearXNG process started by Enough.
export const stop = (): Effect.Effect<void, searxng_error_type> => default_manager().stop();

const health_ok_sync = async (ctx: AbortSignal, base: string): Promise<boolean> => {
  try {
    const resp = await fetch(`${base}/healthz`, { signal: ctx });
    return resp.status === 200;
  } catch {
    return false;
  }
};

const wait_healthy = (ctx: AbortSignal, base: string): Effect.Effect<void, searxng_error_type> =>
  Effect.promise(async () => {
    if (ctx.aborted) {
      throw searxng_error("context already aborted", null);
    }

    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), health_timeout_ms);

    const on_abort = () => controller.abort();
    ctx.addEventListener("abort", on_abort);

    const signal = controller.signal;

    try {
      while (!signal.aborted) {
        try {
          const resp = await fetch(`${base}/healthz`, { signal });
          if (resp.status === 200) {
            return;
          }
        } catch {
          // retry
        }
        await new Promise<void>((resolve) => setTimeout(resolve, health_interval_ms));
      }
      throw searxng_error("searxng did not become ready", null);
    } finally {
      clearTimeout(timer);
      ctx.removeEventListener("abort", on_abort);
    }
  }).pipe(Effect.catchAll((cause) => Effect.fail(searxng_error("wait healthy", cause))));

const free_port = (): Effect.Effect<number, searxng_error_type> =>
  Effect.tryPromise({
    try: () =>
      new Promise<number>((resolve, reject) => {
        const server = net.createServer();
        // listen() is async: address() is only valid after 'listening'.
        // Reading it synchronously returns null under Node (Electron's runtime),
        // which is what surfaced as the "free port" error.
        server.once("listening", () => {
          const address = server.address();
          server.close();
          if (address === null || typeof address === "string") {
            reject(new Error("invalid listen address"));
            return;
          }
          resolve(address.port);
        });
        server.once("error", (err) => {
          server.close();
          reject(err);
        });
        server.listen(0, "127.0.0.1");
      }),
    catch: (cause) => searxng_error("free port", cause),
  });

const process_alive = (pid: number): boolean => {
  if (pid <= 0) {
    return false;
  }
  try {
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
};

const look_path = (name: string): string | null => {
  const paths = (process.env.PATH ?? "").split(path.delimiter);
  for (const dir of paths) {
    const candidate = path.join(dir, name);
    if (fs.existsSync(candidate)) {
      return candidate;
    }
  }
  return null;
};

/*
PORT STATUS
source path: backend/web/searxng/manager.go
source lines: 300
draft lines: 377
confidence: medium
status: phase_a_draft
todos:
  - embed real settings.yml instead of placeholder
  - replace sync spawn helpers with async Effect wrappers if UI responsiveness matters
  - verify process kill/wait behavior matches Go's SIGTERM + 5s fallback
  - consider AbortController cleanup edge cases when ctx fires after completion
notes:
  - Methods returning (T, error) are wrapped in Effect.Effect<T, searxng_error>.
  - state.go logic is consumed through write_state/read_state helpers.
*/
