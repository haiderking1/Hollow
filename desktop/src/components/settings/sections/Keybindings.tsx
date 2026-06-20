import { SettingsCard } from "../controls"

const SHORTCUTS: { keys: string; label: string }[] = [
  { keys: "Ctrl + K", label: "Toggle search" },
  { keys: "Ctrl + =", label: "Zoom in" },
  { keys: "Ctrl + −", label: "Zoom out" },
  { keys: "Ctrl + 0", label: "Reset zoom" },
  { keys: "Enter", label: "Send message" },
  { keys: "Shift + Enter", label: "Newline in composer" },
]

export function Keybindings() {
  return (
    <SettingsCard>
      {SHORTCUTS.map((s, i) => (
        <div
          key={s.label}
          className={`flex items-center justify-between gap-6 py-4 ${
            i < SHORTCUTS.length - 1 ? "border-b border-white/[0.06]" : ""
          }`}
        >
          <span className="text-[14px] text-white">{s.label}</span>
          <kbd className="rounded-[8px] border border-white/10 bg-[#1c1c1f] px-2 py-1 font-mono text-[12px] text-[#cfcfd2]">
            {s.keys}
          </kbd>
        </div>
      ))}
    </SettingsCard>
  )
}