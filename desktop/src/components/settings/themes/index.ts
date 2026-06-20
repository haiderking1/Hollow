// Theme registry. Themes are applied by setting
// `document.documentElement.dataset.theme`, which selects a token block in
// index.css (default / dark = `:root`, light = `:root[data-theme="light"]`).
// Components use semantic tokens, so this is the only place theme switching
// lives. Persistence is handled by the prefs module (prefs.theme), which is
// the single source of truth — App applies prefs.theme on mount + change.

export type ThemeId = "dark" | "light"

export interface ThemeDef {
  id: ThemeId
  name: string
  /** Preview swatch colors (hex) for the picker. */
  swatch: { bg: string; fg: string; accent: string; border: string }
}

export const THEMES: ThemeDef[] = [
  {
    id: "dark",
    name: "Dark",
    swatch: { bg: "#0E0E10", fg: "#FFFFFF", accent: "#3B82F6", border: "rgba(255,255,255,0.10)" },
  },
  {
    id: "light",
    name: "Light",
    swatch: { bg: "#fbf1c7", fg: "#3c3836", accent: "#c44a26", border: "#d5c39a" },
  },
]

/** Apply a theme to the document. */
export function applyTheme(id: ThemeId): void {
  try {
    document.documentElement.dataset.theme = id
  } catch {
    /* ignore */
  }
}