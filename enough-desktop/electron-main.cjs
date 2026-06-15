process.env.ELECTRON_DISABLE_SECURITY_WARNINGS = 'true';

const { app, BrowserWindow, ipcMain, dialog } = require('electron');
const path = require('path');
const os = require('os');
const fs = require('fs');

// Nvidia/Linux fixes requested verbatim
app.commandLine.appendSwitch("ignore-gpu-blocklist");
app.commandLine.appendSwitch("enable-gpu-rasterization");
app.commandLine.appendSwitch("enable-zero-copy");
app.commandLine.appendSwitch("use-gl", "angle");   // ANGLE-over-GL; Vulkan backend is flaky on NVIDIA
app.commandLine.appendSwitch("use-angle", "gl");
app.commandLine.appendSwitch("disable-smooth-scrolling"); // 1:1 scroll, no easing lag on high-refresh

function createWindow() {
  const isDev = process.env.NODE_ENV === 'development' || !app.isPackaged;
  const preload = path.join(__dirname, 'preload.cjs');

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
      preload: preload,
      zoomFactor: 1.0,
    }
  });

  win.setMenu(null);

  if (isDev) {
    win.loadURL("http://localhost:1420");
  } else {
    win.loadFile(path.join(__dirname, 'dist/index.html'));
  }
}

// IPC Handlers for custom frameless title bar window controls
ipcMain.on('window-minimize', (event) => {
  const win = BrowserWindow.fromWebContents(event.sender);
  if (win) win.minimize();
});

ipcMain.on('window-maximize', (event) => {
  const win = BrowserWindow.fromWebContents(event.sender);
  if (win) {
    if (win.isMaximized()) {
      win.unmaximize();
    } else {
      win.maximize();
    }
  }
});

ipcMain.on('window-close', (event) => {
  const win = BrowserWindow.fromWebContents(event.sender);
  if (win) win.close();
});

function listDirectory(targetPath) {
  const home = os.homedir();
  const resolved = targetPath ? path.resolve(targetPath) : home;
  let parent = path.dirname(resolved);
  if (parent === resolved) parent = null;

  try {
    const entries = fs.readdirSync(resolved, { withFileTypes: true });
    const dirs = entries
      .filter((e) => e.isDirectory() && !e.name.startsWith('.'))
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

ipcMain.handle('fs-list-dir', (_event, targetPath) => listDirectory(targetPath));

ipcMain.handle('fs-pick-directory', async (event) => {
  const win = BrowserWindow.fromWebContents(event.sender);
  const result = await dialog.showOpenDialog(win ?? undefined, {
    properties: ['openDirectory'],
  });
  if (result.canceled || result.filePaths.length === 0) return null;
  return result.filePaths[0];
});

let backendProcess = null;

function startBackend() {
  const isDev = process.env.NODE_ENV === 'development' || !app.isPackaged;
  if (!isDev) return;

  const { spawn } = require('child_process');
  const fs = require('fs');

  const binPath = path.join(__dirname, '../bin/enough');
  if (fs.existsSync(binPath)) {
    console.log('[main] Spawning Go backend from:', binPath);
    backendProcess = spawn(binPath, ['serve'], {
      cwd: os.homedir(),
      stdio: 'inherit'
    });

    backendProcess.on('error', (err) => {
      console.error('[main] Failed to start Go backend:', err);
    });
  } else {
    console.warn('[main] Go backend binary not found at:', binPath);
  }
}

app.whenReady().then(() => {
  startBackend();
  createWindow();

  app.on('activate', () => {
    if (BrowserWindow.getAllWindows().length === 0) {
      createWindow();
    }
  });
});

app.on('window-all-closed', () => {
  if (backendProcess) {
    console.log('[main] Terminating Go backend process...');
    try {
      backendProcess.kill();
    } catch (e) {
      // ignore
    }
  }
  if (process.platform !== 'darwin') {
    app.quit();
  }
});
