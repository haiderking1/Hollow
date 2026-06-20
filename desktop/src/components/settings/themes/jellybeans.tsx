import type { Theme } from "./types"

// Jellybeans — the low-contrast vim theme. Muted near-black backgrounds with a
// warm amber accent (#ffb964) and soft green/red.
export const jellybeans: Theme = {
  id: "jellybeans",
  name: "Jellybeans",
  swatch: { bg: "#161616", fg: "#e8e8d3", accent: "#ffb964", border: "rgba(232,232,211,0.13)" },
  tokens: {
    background: "#161616",
    foreground: "#e8e8d3",
    sidebar: "#1c1c1c",
    surface: "#202020",
    "surface-hover": "#2c2c2c",
    elevated: "#262626",
    border: "rgba(232, 232, 211, 0.07)",
    "border-strong": "rgba(232, 232, 211, 0.13)",
    muted: "#202020",
    "muted-foreground": "#888888",
    "icon-inactive": "#6b6b6b",
    "toggle-off": "#303030",
    accent: "#ffb964",
    "accent-foreground": "#151515",
    "accent-muted": "#e5b124",
    success: "#99c794",
    danger: "#cf6a4c",
    warning: "#ffb964",
    info: "#7e9cd8",
    add: "#99c794",
    "add-bg": "rgba(153, 199, 148, 0.13)",
    del: "#cf6a4c",
    "del-bg": "rgba(207, 106, 76, 0.13)",
  },
}