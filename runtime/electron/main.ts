import { app, BrowserWindow, ipcMain, dialog } from "electron";
import path from "node:path";
import os from "node:os";
import fs from "node:fs";
import { fileURLToPath } from "node:url";
import { Effect } from "effect";
import { bootHollowRuntime, degradedRuntime, attachElectronIpc, type BootedRuntime } from "./bootstrap";

const __dirname = path.dirname(fileURLToPath(import.meta.url));

// Nvidia/Linux GL fixes.
app.commandLine.appendSwitch("ignore-gpu-blocklist");
app.commandLine.appendSwitch("enable-gpu-rasterization");
app.commandLine.appendSwitch("enable-zero-copy");
app.commandLine.appendSwitch("use-gl", "angle");
app.commandLine.appendSwitch("use-angle", "gl");
app.commandLine.appendSwitch("disable-smooth-scrolling");
// Native Wayland surface so Hyprland can frost the transparent Glass window
// (XWayland windows can't be blurred by the compositor). The GL/ANGLE backend
// above avoids the NVIDIA blank-window issue that originally forced X11 here.
// To fall back to X11, set ELECTRON_OZONE_PLATFORM=x11 in the environment.
if (process.platform === "linux") {
  app.commandLine.appendSwitch("ozone-platform", process.env.ELECTRON_OZONE_PLATFORM ?? "wayland");
}

let bridge: BootedRuntime | null = null;

const preloadPath = path.join(__dirname, "preload.cjs");
const devServerUrl = process.env.VITE_DEV_SERVER_URL;
// Desktop UI dist (React app built into hollow/desktop/).
const desktopDist = path.join(__dirname, "..", "..", "desktop", "dist", "index.html");

function createWindow(): void {
  const platform = process.platform;
  const win = new BrowserWindow({
    width: 1280,
    height: 800,
    minWidth: 900,
    minHeight: 600,
    frame: false,
    // See-through + native frosted blur. The window is created transparent at
    // boot so the "Glass" theme (translucent CSS tokens) can swap live with no
    // restart — opaque themes cover this fully, so it's invisible unless Glass
    // is active. On macOS `vibrancy` and on Windows 11 `backgroundMaterial:
    // acrylic` frost the desktop natively; on Linux Electron only makes the
    // window see-through and the compositor does the frost (Hyprland blurs
    // transparent windows; Raven/KWin vary; Mutter does not).
    backgroundColor: "#00000000",
    ...(platform === "darwin"
      ? { transparent: true, vibrancy: "under-window", visualEffectState: "active" }
      : platform === "win32"
        ? { backgroundMaterial: "acrylic" }
        : { transparent: true }),
    webPreferences: {
      nodeIntegration: false,
      contextIsolation: true,
      preload: preloadPath,
      zoomFactor: 1.0,
    },
  });

  win.setMenu(null);

  if (devServerUrl) {
    void win.loadURL(devServerUrl).catch((err) => {
      console.error("[electron] Failed to load dev server:", devServerUrl, err);
    });
    win.webContents.on("did-fail-load", (_event, code, desc, url) => {
      console.error("[electron] did-fail-load:", code, desc, url);
    });
  } else {
    win.loadFile(desktopDist);
  }
}

function listDirectory(targetPath?: string) {
  const home = os.homedir();
  const resolved = targetPath ? path.resolve(targetPath) : home;
  let parent: string | null = path.dirname(resolved);
  if (parent === resolved) parent = null;

  try {
    const entries = fs.readdirSync(resolved, { withFileTypes: true });
    const dirs = entries
      .filter((e) => e.isDirectory() && !e.name.startsWith("."))
      .map((e) => ({ name: e.name, path: path.join(resolved, e.name) }))
      .sort((a, b) => a.name.localeCompare(b.name));

    return { path: resolved, parent, entries: dirs, home };
  } catch (err) {
    return {
      path: resolved,
      parent,
      entries: [],
      home,
      error: err instanceof Error ? err.message : String(err),
    };
  }
}

// Custom frameless title-bar window controls.
ipcMain.on("window-minimize", (event) => {
  const w = BrowserWindow.fromWebContents(event.sender);
  if (w) w.minimize();
});
ipcMain.on("window-maximize", (event) => {
  const w = BrowserWindow.fromWebContents(event.sender);
  if (w) {
    if (w.isMaximized()) w.unmaximize();
    else w.maximize();
  }
});
ipcMain.on("window-close", (event) => {
  const w = BrowserWindow.fromWebContents(event.sender);
  if (w) w.close();
});

ipcMain.handle("fs-list-dir", (_event, targetPath: any) => {
  return listDirectory(typeof targetPath === "string" ? targetPath : undefined);
});

ipcMain.handle("fs-pick-directory", async (event) => {
  const win = BrowserWindow.fromWebContents(event.sender);
  const result = win
    ? await dialog.showOpenDialog(win, { properties: ["openDirectory"] })
    : await dialog.showOpenDialog({ properties: ["openDirectory"] });
  if (result.canceled || result.filePaths.length === 0) return null;
  return result.filePaths[0];
});

app.whenReady().then(async () => {
  let result: BootedRuntime;
  try {
    result = await Effect.runPromise(bootHollowRuntime(process.cwd()));
  } catch (err) {
    console.warn(
      "[electron] Runtime boot failed (agent unavailable):",
      err instanceof Error ? err.message : err,
    );
    console.warn("[electron] Opening UI in degraded mode — connect a provider in ~/.hollow to chat.");
    // Always boot a degraded runtime so IPC registers and the UI is usable,
    // instead of bricking every renderer call with "No handler registered".
    result = await Effect.runPromise(degradedRuntime(process.cwd()));
  }
  bridge = result;

  attachElectronIpc(
    ipcMain,
    result.bridge,
    () => BrowserWindow.getAllWindows().map((w) => w.webContents),
  );

  createWindow();

  app.on("activate", () => {
    if (BrowserWindow.getAllWindows().length === 0) {
      createWindow();
    }
  });
});

app.on("window-all-closed", () => {
  if (process.platform !== "darwin") {
    app.quit();
  }
});

// Runtime cleanup on exit — close MCP manager / running agent.
app.on("before-quit", () => {
  if (bridge?.runtime.agent?.Close) {
    try {
      bridge.runtime.agent.Close();
    } catch {
      // ignore cleanup errors
    }
  }
});
