import { Check } from "lucide-react"
import { cn } from "../../../lib/utils"
import type { HollowPrefs, PrefKey } from "../prefs"
import { THEMES } from "../themes"

export function Appearance({
  prefs,
  onPref,
}: {
  prefs: HollowPrefs
  onPref: <K extends PrefKey>(key: K, value: HollowPrefs[K]) => void
}) {
  return (
    <div className="flex flex-col gap-2">
      {THEMES.map((t) => {
        const active = t.id === prefs.theme
        return (
          <button
            key={t.id}
            onClick={() => onPref("theme", t.id)}
            aria-pressed={active}
            className={cn(
              "flex w-full items-center gap-3 rounded-lg border px-3 py-2.5 transition-colors",
              active ? "border-accent bg-surface-hover" : "border-border hover:border-border-strong",
            )}
          >
            <span
              className="h-5 w-5 shrink-0 rounded-full border-2"
              style={{ background: t.tokens.background, borderColor: t.tokens.accent }}
            />
            <span className="text-[13px] font-medium text-foreground">{t.name}</span>
            {active && <Check className="ml-auto h-4 w-4 text-accent" strokeWidth={2.25} />}
          </button>
        )
      })}
    </div>
  )
}