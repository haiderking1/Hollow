import { RotateCcw } from "lucide-react"
import { PillSelect, SettingRow, SettingsCard, Toggle } from "../controls"
import type { HollowPrefs, PrefKey } from "../prefs"
import { applyTheme, type ThemeId } from "../themes"

const THEME_OPTIONS = [
  { value: "dark", label: "Dark" },
  { value: "light", label: "Light" },
]
const TIME_OPTIONS = [
  { value: "system", label: "System default" },
  { value: "12", label: "12-hour" },
  { value: "24", label: "24-hour" },
]
const NEW_THREAD_OPTIONS = [{ value: "local", label: "Local" }]

export function General({
  prefs,
  onPref,
}: {
  prefs: HollowPrefs
  onPref: <K extends PrefKey>(key: K, value: HollowPrefs[K]) => void
}) {
  const setThemePref = (id: ThemeId) => {
    applyTheme(id)
    onPref("theme", id)
  }
  const resetTheme = () => setThemePref("dark")

  return (
    <SettingsCard>
      <SettingRow
        label="Theme"
        subtitle="Choose how Hollow looks across the app."
        icon={
          <button
            onClick={resetTheme}
            title="Reset to default"
            className="text-[#6B6B70] transition-colors hover:text-white"
          >
            <RotateCcw className="h-3.5 w-3.5" strokeWidth={2} />
          </button>
        }
      >
        <PillSelect
          value={prefs.theme}
          onChange={(v) => setThemePref(v as ThemeId)}
          options={THEME_OPTIONS}
        />
      </SettingRow>

      <SettingRow
        label="Time format"
        subtitle="System default follows your browser or OS clock preference."
      >
        <PillSelect
          value={prefs.timeFormat}
          onChange={(v) => onPref("timeFormat", v as HollowPrefs["timeFormat"])}
          options={TIME_OPTIONS}
        />
      </SettingRow>

      <SettingRow label="Diff line wrapping" subtitle="Set the default wrap state when the diff panel opens.">
        <Toggle checked={prefs.diffWrap} onChange={(v) => onPref("diffWrap", v)} />
      </SettingRow>

      <SettingRow label="Hide whitespace changes" subtitle="Set whether the diff panel ignores whitespace-only edits by default.">
        <Toggle checked={prefs.hideWhitespace} onChange={(v) => onPref("hideWhitespace", v)} />
      </SettingRow>

      <SettingRow label="Assistant output" subtitle="Show token-by-token output while a response is in progress.">
        <Toggle checked={prefs.assistantOutput} onChange={(v) => onPref("assistantOutput", v)} />
      </SettingRow>

      <SettingRow label="Auto-open task panel" subtitle="Open the right-side plan and task panel automatically when steps appear.">
        <Toggle checked={prefs.autoOpenTaskPanel} onChange={(v) => onPref("autoOpenTaskPanel", v)} />
      </SettingRow>

      <SettingRow label="New threads" subtitle="Where new conversation threads are created by default." isLast>
        <PillSelect
          value={prefs.newThreadLocation}
          onChange={(v) => onPref("newThreadLocation", v as HollowPrefs["newThreadLocation"])}
          options={NEW_THREAD_OPTIONS}
        />
      </SettingRow>
    </SettingsCard>
  )
}