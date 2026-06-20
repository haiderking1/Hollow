// PORT STATUS: active
// Boots the hollow runtime in the Electron main process.
// Agent NEVER leaves main — the renderer talks IPC only.

import { Effect } from "effect";
import { AgentRuntimeImpl } from "../agent_runtime";
import { DesktopBridge } from "../desktop_bridge";
import {
  registerElectronIpc,
  type ElectronMainApi,
  type WebContentsLike,
} from "../ipc/electron_ipc";

export interface BootedRuntime {
  runtime: AgentRuntimeImpl;
  bridge: DesktopBridge;
}

/**
 * Boot the Hollow runtime: load config, open the recent session, and wire the
 * DesktopBridge. Returns the runtime + bridge so Electron main can attach IPC.
 */
export const bootHollowRuntime = (workDir?: string): Effect.Effect<BootedRuntime, Error> =>
  Effect.gen(function* () {
    const runtime = new AgentRuntimeImpl();
    yield* runtime.boot(workDir);
    const bridge = new DesktopBridge(runtime);
    return { runtime, bridge };
  });

/**
 * Degraded runtime for when a real (non-key) boot error happens: just the
 * event bus, no agent. Never fails — lets Electron always attach IPC so the
 * UI opens instead of bricking with "No handler registered for 'hollow:dispatch'".
 */
export const degradedRuntime = (workDir?: string): Effect.Effect<BootedRuntime, never> =>
  Effect.gen(function* () {
    const runtime = new AgentRuntimeImpl();
    yield* runtime.bootDegraded(workDir);
    return { runtime, bridge: new DesktopBridge(runtime) };
  });

/**
 * Register the Electron IPC handlers against a real ipcMain.
 * `getWebContents` is typically `() => BrowserWindow.getAllWindows().map(w => w.webContents)`.
 */
export const attachElectronIpc = (
  api: ElectronMainApi,
  bridge: DesktopBridge,
  getWebContents: () => WebContentsLike[],
): void => {
  registerElectronIpc(api, bridge, { getAllWebContents: getWebContents });
};
