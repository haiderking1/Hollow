import type { Theme } from "./types"

// The default Hollow dark theme — the unified spec palette (page #0E0E10,
// card #17171A, blue #3B82F6 accent). Mirrors the :root block in index.css so
// first paint (before JS applies a theme) already looks right.
export const dark: Theme = {
  id: "dark",
  name: "Dark",
  swatch: { bg: "#0E0E10", fg: "#E6E6E8", accent: "#3B82F6", border: "rgba(255,255,255,0.10)" },
  tokens: {
    background: "#0E0E10",
    foreground: "#E6E6E8",
    sidebar: "#0E0E10",
    surface: "#17171A",
    "surface-hover": "#1c1c1f",
    elevated: "#1c1c1f",
    border: "rgba(255, 255, 255, 0.06)",
    "border-strong": "rgba(255, 255, 255, 0.10)",
    muted: "#17171A",
    "muted-foreground": "#8E8E93",
    "icon-inactive": "#6B6B70",
    "toggle-off": "#2C2C2E",
    accent: "#3B82F6",
    "accent-foreground": "#FFFFFF",
    "accent-muted": "#2f6fde",
    success: "#3fb950",
    danger: "#f87171",
    warning: "#e0a96b",
    info: "#60a5fa",
    add: "#3fb950",
    "add-bg": "rgba(63, 185, 80, 0.12)",
    del: "#f87171",
    "del-bg": "rgba(248, 113, 113, 0.12)",
  },
}