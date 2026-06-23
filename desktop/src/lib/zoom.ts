const ZOOM_MIN = 0.5
const ZOOM_MAX = 2.5
export const ZOOM_STEP = 0.05

let currentZoom = 1.0
let persistTimer: number | null = null

export function loadZoom(): number {
  try {
    const saved = localStorage.getItem("hollow-zoom")
    if (!saved) return 1.0
    const level = parseFloat(saved)
    if (!Number.isFinite(level)) return 1.0
    return Math.min(Math.max(level, ZOOM_MIN), ZOOM_MAX)
  } catch {
    return 1.0
  }
}

export function initZoom() {
  currentZoom = loadZoom()
  applyZoom(currentZoom)
}

export function applyZoom(level: number) {
  try {
    window.hollowDesktop?.setZoom(level)
  } catch (err) {
    console.error("Failed to set zoom:", err)
  }
}

function clampZoom(level: number) {
  return Math.min(Math.max(level, ZOOM_MIN), ZOOM_MAX)
}

function persistZoom(level: number) {
  if (persistTimer !== null) window.clearTimeout(persistTimer)
  persistTimer = window.setTimeout(() => {
    localStorage.setItem("hollow-zoom", level.toString())
    persistTimer = null
  }, 200)
}

/** Adjust zoom without touching React state — avoids re-parsing every markdown block. */
export function bumpZoom(delta: number): number {
  currentZoom = clampZoom(currentZoom + delta)
  applyZoom(currentZoom)
  persistZoom(currentZoom)
  return currentZoom
}

export function resetZoom(): number {
  currentZoom = 1.0
  applyZoom(currentZoom)
  persistZoom(currentZoom)
  return currentZoom
}
