import path from "node:path";
import fs from "node:fs";
import { spawn, type ChildProcess } from "node:child_process";
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
  return new TextEncoder().encode("port: 18752\nserver:\n  secret_key: \"hollow-local-searxng\"\n");
};

const repo_url = "https://github.com/searxng/searxng.git";
const health_timeout_ms = 90_000;
const health_interval_ms = 400;

export type searxng_status = (message: string) => void;

let manager_instance: manager | null = null;
let manager_initialized = false;

// Manager runs a local SearXNG instance for Hollow.
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
  ensure_running(
    ctx: AbortSignal,
    on_status: searxng_status = () => {},
  ): Effect.Effect<string, searxng_error_type> {
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

      yield* self.ensure_installed(ctx, on_status);

      on_status("Starting local SearXNG…");
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

      on_status("Waiting for SearXNG to become ready…");
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

  // Stop shuts down a SearXNG process started by Hollow.
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

  private ensure_installed(
    ctx: AbortSignal,
    on_status: searxng_status,
  ): Effect.Effect<void, searxng_error_type> {
    const self = this;
    return Effect.gen(function* () {
      const src_dir = path.join(self._data_dir, "src");
      const webapp = path.join(src_dir, "searx", "webapp.py");
      if (fs.existsSync(webapp)) {
        yield* self.ensure_venv(ctx, on_status);
        return;
      }

      on_status("Installing SearXNG (first run)…");

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

      on_status("Downloading SearXNG source…");
      yield* run_process(ctx, "git", ["clone", "--depth", "1", repo_url, src_dir], "clone searxng");

      yield* self.ensure_venv(ctx, on_status);
    });
  }

  private ensure_venv(
    ctx: AbortSignal,
    on_status: searxng_status,
  ): Effect.Effect<void, searxng_error_type> {
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
      on_status("Creating Python environment…");
      yield* run_process(ctx, py3, ["-m", "venv", venv_dir], "create venv");

      const pip = path.join(venv_dir, "bin", "pip");
      const reqs = path.join(self._data_dir, "src", "requirements.txt");
      on_status("Installing SearXNG dependencies…");
      yield* run_process(
        ctx,
        pip,
        ["install", "-r", reqs],
        "install searxng dependencies (this may take a minute)",
      );
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

const run_process = (
  ctx: AbortSignal,
  command: string,
  args: string[],
  operation: string,
): Effect.Effect<void, searxng_error_type> =>
  Effect.tryPromise({
    try: () =>
      new Promise<void>((resolve, reject) => {
        if (ctx.aborted) {
          reject(ctx.reason ?? new DOMException("The operation was aborted", "AbortError"));
          return;
        }

        const child = spawn(command, args, { stdio: "ignore" });
        let settled = false;
        const cleanup = () => {
          ctx.removeEventListener("abort", on_abort);
        };
        const finish = (cause?: unknown) => {
          if (settled) return;
          settled = true;
          cleanup();
          if (cause === undefined) resolve();
          else reject(cause);
        };
        const on_abort = () => {
          child.kill();
          finish(ctx.reason ?? new DOMException("The operation was aborted", "AbortError"));
        };

        ctx.addEventListener("abort", on_abort, { once: true });
        child.once("error", (cause) => finish(cause));
        child.once("exit", (code, signal) => {
          if (code === 0) finish();
          else finish(new Error(`${command} ${operation} exited with ${signal ?? `status ${code ?? "unknown"}`}`));
        });
      }),
    catch: (cause) => searxng_error(operation, cause),
  });

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
  on_status: searxng_status = () => {},
): Effect.Effect<string, searxng_error_type> => default_manager().ensure_running(ctx, on_status);

// Stop shuts down a SearXNG process started by Hollow.
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

