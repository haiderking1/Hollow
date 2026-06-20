import type { Theme } from "./types"

// Glass — translucent see-through theme. Backgrounds use alpha so the desktop
// shows through the (transparent) Electron window; text and accents stay solid
// for readability. The frosted blur is native on macOS (vibrancy) / Windows 11
// (acrylic) and compositor-side on Linux (Hyprland blurs; Raven/KWin vary).
export const glass: Theme = {
  id: "glass",
  name: "Glass",
  swatch: { bg: "#0E0E10", fg: "#E6E6E8", accent: "#3B82F6", border: "rgba(255,255,255,0.14)" },
  tokens: {
    background: "rgba(14, 14, 16, 0.55)",
    foreground: "#E6E6E8",
    sidebar: "rgba(14, 14, 16, 0.45)",
    surface: "rgba(23, 23, 26, 0.62)",
    "surface-hover": "rgba(40, 40, 44, 0.72)",
    elevated: "rgba(28, 28, 31, 0.68)",
    border: "rgba(255, 255, 255, 0.08)",
    "border-strong": "rgba(255, 255, 255, 0.14)",
    muted: "rgba(23, 23, 26, 0.62)",
    "muted-foreground": "rgba(142, 142, 147, 0.95)",
    "icon-inactive": "#6B6B70",
    "toggle-off": "rgba(44, 44, 46, 0.8)",
    accent: "#3B82F6",
    "accent-foreground": "#FFFFFF",
    "accent-muted": "#2f6fde",
    success: "#3fb950",
    danger: "#f87171",
    warning: "#e0a96b",
    info: "#60a5fa",
    add: "#3fb950",
    "add-bg": "rgba(63, 185, 80, 0.18)",
    del: "#f87171",
    "del-bg": "rgba(248, 113, 113, 0.18)",
  },
}