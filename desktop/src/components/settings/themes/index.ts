// Theme registry. Each theme is a self-contained file (see ./dark.tsx,
// ./gruvbox.tsx, …) holding its full palette as data. applyTheme() writes the
// theme's tokens onto documentElement as inline CSS variables, which override
// the :root defaults in index.css — so components keep using semantic token
// classes (bg-surface, border-border, …) and never need to know which theme is
// active. Persistence lives in the prefs module (prefs.theme); App applies
// prefs.theme on mount + whenever it changes.

import type { Theme } from "./types"
import { THEME_TOKEN_KEYS } from "./types"
import { dark } from "./dark"
import { gruvbox } from "./gruvbox"
import { nord } from "./nord"
import { jellybeans } from "./jellybeans"
import { catppuccin } from "./catppuccin"
import { glass } from "./glass"
import { light } from "./light"

export type { Theme } from "./types"

export const THEMES: Theme[] = [dark, glass, gruvbox, nord, jellybeans, catppuccin, light]

/** Resolve a theme by id, falling back to the default (dark) if unknown. */
export function getTheme(id: string): Theme {
  return THEMES.find((t) => t.id === id) ?? THEMES[0]
}

/** Apply a theme to the document by injecting its CSS variables onto :root. */
export function applyTheme(id: string): void {
  try {
    const theme = getTheme(id)
    const root = document.documentElement
    for (const key of THEME_TOKEN_KEYS) {
      const value = theme.tokens[key]
      if (value) root.style.setProperty(`--${key}`, value)
      else root.style.removeProperty(`--${key}`)
    }
    root.dataset.theme = theme.id
  } catch {
    /* ignore — SSR / non-DOM environments */
  }
}