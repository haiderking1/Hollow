import type { Theme } from "./types"

// Gruvbox — the warm, earthy palette Hollow shipped originally. Orange accent
// (#d97757) on near-black backgrounds with soft green/red diff colors.
export const gruvbox: Theme = {
  id: "gruvbox",
  name: "Gruvbox",
  swatch: { bg: "#0a0a0a", fg: "#ededec", accent: "#d97757", border: "#34332f" },
  tokens: {
    background: "#0a0a0a",
    foreground: "#ededec",
    sidebar: "#141413",
    surface: "#1a1a18",
    "surface-hover": "#232321",
    elevated: "#1e1e1c",
    border: "#262624",
    "border-strong": "#34332f",
    muted: "#141413",
    "muted-foreground": "#8a8985",
    "icon-inactive": "#8a8985",
    "toggle-off": "#34332f",
    accent: "#d97757",
    "accent-foreground": "#1a1a18",
    "accent-muted": "#c96442",
    success: "#6cae5e",
    danger: "#e0696b",
    warning: "#e0a96b",
    info: "#4a90d9",
    add: "#87bd80",
    "add-bg": "rgba(127, 176, 105, 0.12)",
    del: "#e0696b",
    "del-bg": "rgba(224, 105, 107, 0.12)",
  },
}