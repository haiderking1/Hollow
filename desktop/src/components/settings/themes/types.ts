// Shared shape for every theme. A theme is just an id, a display name, a few
// preview swatch colors for the picker, and the full set of CSS variable values
// it applies. applyTheme() (see index.ts) writes `tokens` onto documentElement,
// so inline styles override the index.css :root defaults — components keep using
// semantic token classes and never need to know which theme is active.

export interface ThemeSwatch {
  bg: string
  fg: string
  accent: string
  border: string
}

export interface Theme {
  id: string
  name: string
  /** Preview colors for the sidebar picker. */
  swatch: ThemeSwatch
  /** CSS variable name (without the leading --) → value. */
  tokens: Record<string, string>
}

// Every CSS variable a theme must define. applyTheme writes each of these so
// switching themes never leaves a stale value from the previous one.
export const THEME_TOKEN_KEYS = [
  "background",
  "foreground",
  "sidebar",
  "surface",
  "surface-hover",
  "elevated",
  "border",
  "border-strong",
  "muted",
  "muted-foreground",
  "icon-inactive",
  "toggle-off",
  "accent",
  "accent-foreground",
  "accent-muted",
  "success",
  "danger",
  "warning",
  "info",
  "add",
  "add-bg",
  "del",
  "del-bg",
] as const