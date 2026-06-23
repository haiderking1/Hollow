/** Launch Electron with VITE_DEV_SERVER_URL set (Windows-safe env). */
const { spawn } = require("node:child_process");
const path = require("node:path");

process.env.VITE_DEV_SERVER_URL =
  process.env.VITE_DEV_SERVER_URL || "http://localhost:1420";

const electronBin = require("electron");
const cwd = path.join(__dirname, "..");

const child = spawn(electronBin, ["."], {
  cwd,
  env: process.env,
  stdio: "inherit",
  windowsHide: false,
});

child.on("exit", (code, signal) => {
  if (signal) process.kill(process.pid, signal);
  process.exit(code ?? 0);
});

child.on("error", (err) => {
  console.error("[electron-dev]", err);
  process.exit(1);
});
