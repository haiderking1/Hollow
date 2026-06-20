import type { Theme } from "./types"

// Nord — the Arctic, north-bluish color palette (nord0–nord15). Cool slate
// backgrounds with an icy light-blue accent (#88c0d0).
export const nord: Theme = {
  id: "nord",
  name: "Nord",
  swatch: { bg: "#2e3440", fg: "#eceff4", accent: "#88c0d0", border: "rgba(216,222,233,0.14)" },
  tokens: {
    background: "#2e3440",
    foreground: "#eceff4",
    sidebar: "#2e3440",
    surface: "#3b4252",
    "surface-hover": "#434c5e",
    elevated: "#434c5e",
    border: "rgba(216, 222, 233, 0.08)",
    "border-strong": "rgba(216, 222, 233, 0.14)",
    muted: "#3b4252",
    "muted-foreground": "#81a1c1",
    "icon-inactive": "#6f7a90",
    "toggle-off": "#4c566a",
    accent: "#88c0d0",
    "accent-foreground": "#2e3440",
    "accent-muted": "#81a1c1",
    success: "#a3be8c",
    danger: "#bf616a",
    warning: "#ebcb8b",
    info: "#81a1c1",
    add: "#a3be8c",
    "add-bg": "rgba(163, 190, 140, 0.14)",
    del: "#bf616a",
    "del-bg": "rgba(191, 97, 106, 0.14)",
  },
}