import type { Theme } from "./types"

// Gruvbox Light — the light counterpart (cream background, dark text). Kept on
// a blue accent for consistency with the dark themes.
export const light: Theme = {
  id: "light",
  name: "Light",
  swatch: { bg: "#fbf1c7", fg: "#3c3836", accent: "#2563eb", border: "rgba(60,56,54,0.22)" },
  tokens: {
    background: "#fbf1c7",
    foreground: "#3c3836",
    sidebar: "#f2e5bc",
    surface: "#ebdbb9",
    "surface-hover": "#dace9e",
    elevated: "#e6d7a8",
    border: "rgba(60, 56, 54, 0.12)",
    "border-strong": "rgba(60, 56, 54, 0.22)",
    muted: "#ebdbb9",
    "muted-foreground": "#7c6f64",
    "icon-inactive": "#a8a29e",
    "toggle-off": "#d6d3cb",
    accent: "#2563eb",
    "accent-foreground": "#FFFFFF",
    "accent-muted": "#1d4ed8",
    success: "#427b58",
    danger: "#cc241d",
    warning: "#b57614",
    info: "#076678",
    add: "#427b58",
    "add-bg": "rgba(66, 123, 88, 0.12)",
    del: "#cc241d",
    "del-bg": "rgba(204, 36, 29, 0.12)",
  },
}