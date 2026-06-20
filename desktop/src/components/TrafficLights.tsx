import { cn } from "../lib/utils"

// macOS-style traffic-light window controls. Wired to the real Electron
// window-control IPC exposed by the preload (window.hollowDesktop.minimize /
// maximize / close). Drop into any app-drag header — the buttons are app-no-drag
// so they stay clickable while the bar lets you drag the window.
export function TrafficLights({ className }: { className?: string }) {
  const win =
    typeof window !== "undefined"
      ? (window.hollowDesktop as { close?: () => void; minimize?: () => void; maximize?: () => void } | undefined)
      : undefined

  return (
    <div className={cn("group app-no-drag flex items-center gap-2", className)}>
      <button
        onClick={() => win?.close?.()}
        aria-label="Close"
        className="flex h-3 w-3 items-center justify-center rounded-full bg-[#ff5f57] text-[8px] font-bold leading-none text-black/50 transition-colors hover:brightness-110"
      >
        <span className="opacity-0 group-hover:opacity-100">✕</span>
      </button>
      <button
        onClick={() => win?.minimize?.()}
        aria-label="Minimize"
        className="flex h-3 w-3 items-center justify-center rounded-full bg-[#febc2e] text-[9px] font-bold leading-none text-black/50 transition-colors hover:brightness-110"
      >
        <span className="opacity-0 group-hover:opacity-100">−</span>
      </button>
      <button
        onClick={() => win?.maximize?.()}
        aria-label="Maximize"
        className="flex h-3 w-3 items-center justify-center rounded-full bg-[#28c840] text-[8px] font-bold leading-none text-black/50 transition-colors hover:brightness-110"
      >
        <span className="opacity-0 group-hover:opacity-100">+</span>
      </button>
    </div>
  )
}