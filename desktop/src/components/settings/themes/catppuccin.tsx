import type { Theme } from "./types"

// Catppuccin Mocha — the pastel dark palette. Soft dark-blue base (#1e1e2e)
// with a mauve accent (#cba6f5) and pastel green/red/pink.
export const catppuccin: Theme = {
  id: "catppuccin",
  name: "Catppuccin",
  swatch: { bg: "#1e1e2e", fg: "#cdd6f4", accent: "#cba6f5", border: "rgba(205,214,244,0.16)" },
  tokens: {
    background: "#1e1e2e",
    foreground: "#cdd6f4",
    sidebar: "#181825",
    surface: "#313244",
    "surface-hover": "#45475a",
    elevated: "#313244",
    border: "rgba(205, 214, 244, 0.10)",
    "border-strong": "rgba(205, 214, 244, 0.16)",
    muted: "#313244",
    "muted-foreground": "#a6adc8",
    "icon-inactive": "#6c7086",
    "toggle-off": "#45475a",
    accent: "#cba6f5",
    "accent-foreground": "#1e1e2e",
    "accent-muted": "#b4befe",
    success: "#a6e3a1",
    danger: "#f38ba8",
    warning: "#f9e2af",
    info: "#89b4fa",
    add: "#a6e3a1",
    "add-bg": "rgba(166, 227, 161, 0.14)",
    del: "#f38ba8",
    "del-bg": "rgba(243, 139, 168, 0.14)",
  },
}