// PORT STATUS: active
// Bun + Electron unified entry. Agent runs in main; renderer talks IPC only.
// NO WebSocket, NO Go binary spawn (no serve.go port).

import { app, BrowserWindow, ipcMain } from "electron";
import path from "node:path";
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
// Wayland + Vulkan often renders a blank window on NVIDIA; prefer X11 unless overridden.
if (process.platform === "linux" && !process.env.ELECTRON_OZONE_PLATFORM_HINT) {
  app.commandLine.appendSwitch("ozone-platform-hint", "x11");
}

let bridge: BootedRuntime | null = null;

const preloadPath = path.join(__dirname, "preload.cjs");
const devServerUrl = process.env.VITE_DEV_SERVER_URL;
// Desktop UI dist (React app built into hollow/desktop/).
const desktopDist = path.join(__dirname, "..", "..", "desktop", "dist", "index.html");

function createWindow(): void {
  const win = new BrowserWindow({
    width: 1280,
    height: 800,
    minWidth: 900,
    minHeight: 600,
    frame: false,
    backgroundColor: "#0a0a0a",
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

app.whenReady().then(async () => {
  let result: BootedRuntime;
  try {
    result = await Effect.runPromise(bootHollowRuntime(process.cwd()));
  } catch (err) {
    console.warn(
      "[electron] Runtime boot failed (agent unavailable):",
      err instanceof Error ? err.message : err,
    );
    console.warn("[electron] Opening UI in degraded mode — connect a provider in ~/.enough to chat.");
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
