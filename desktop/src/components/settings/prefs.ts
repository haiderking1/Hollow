import type { ThemeId } from "./themes"

// App-level preferences persisted to localStorage. These drive the General
// settings rows. Theme is also applied to the document (see themes/index.ts).
export interface HollowPrefs {
  theme: ThemeId
  timeFormat: "system" | "12" | "24"
  diffWrap: boolean
  hideWhitespace: boolean
  assistantOutput: boolean
  autoOpenTaskPanel: boolean
  newThreadLocation: "local"
}

// Defaults match the reference layout's on/off states.
export const DEFAULT_PREFS: HollowPrefs = {
  theme: "dark",
  timeFormat: "system",
  diffWrap: false,
  hideWhitespace: true,
  assistantOutput: false,
  autoOpenTaskPanel: true,
  newThreadLocation: "local",
}

const KEY = "hollow-prefs"

export function loadPrefs(): HollowPrefs {
  try {
    const v = localStorage.getItem(KEY)
    if (!v) return DEFAULT_PREFS
    return { ...DEFAULT_PREFS, ...(JSON.parse(v) as Partial<HollowPrefs>) }
  } catch {
    return DEFAULT_PREFS
  }
}

export function savePrefs(p: HollowPrefs): void {
  try {
    localStorage.setItem(KEY, JSON.stringify(p))
  } catch {
    /* ignore */
  }
}

export type PrefKey = keyof HollowPrefs