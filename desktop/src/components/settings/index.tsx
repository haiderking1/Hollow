import { useState } from "react"
import { X } from "lucide-react"
import type { AgentModel, AgentSessionInfo, CodexLoginState, ConnectionInfo } from "../../agent/rpc"
import { SectionHeader } from "./controls"
import { SettingsNav, type SectionId } from "./nav"
import type { HollowPrefs, PrefKey } from "./prefs"
import { Archive } from "./sections/Archive"
import { Connections } from "./sections/Connections"
import { General } from "./sections/General"
import { Keybindings } from "./sections/Keybindings"
import { Providers, type ProvidersProps } from "./sections/Providers"
import { SourceControl } from "./sections/SourceControl"

const SECTION_TITLES: Record<SectionId, string> = {
  general: "General",
  keybindings: "Keybindings",
  providers: "Providers",
  sourceControl: "Source Control",
  connections: "Connections",
  archive: "Archive",
}

export interface SettingsPageProps extends ProvidersProps {
  open: boolean
  onClose: () => void
  prefs: HollowPrefs
  onPref: <K extends PrefKey>(key: K, value: HollowPrefs[K]) => void
  hiddenThreads: string[]
  sessions: AgentSessionInfo[]
  threadAliases: Record<string, string>
  onUnhideThread: (id: string) => void
  onArchiveThread: (id: string) => void
}

export default function SettingsPage({
  open,
  onClose,
  prefs,
  onPref,
  models,
  currentModelId,
  connections,
  codexLogin,
  settingsError,
  onClearError,
  onSelectModel,
  onConnectKey,
  onRemoveKey,
  onStartCodexLogin,
  onCancelCodexLogin,
  hiddenThreads,
  sessions,
  threadAliases,
  onUnhideThread,
  onArchiveThread,
}: SettingsPageProps) {
  const [section, setSection] = useState<SectionId>("general")
  if (!open) return null

  return (
    <div className="fixed inset-0 z-50 flex bg-[#0E0E10] text-white">
      <SettingsNav active={section} onNavigate={setSection} onBack={onClose} />

      <main className="flex min-w-0 flex-1 flex-col">
        <header className="app-drag flex h-12 shrink-0 items-center justify-end border-b border-white/[0.06] px-4 select-none">
          <button
            onClick={onClose}
            className="app-no-drag flex h-6 w-6 items-center justify-center rounded-md text-[#8E8E93] transition-colors hover:bg-white/[0.06] hover:text-white"
            aria-label="Close settings"
          >
            <X className="h-4 w-4" strokeWidth={2.25} />
          </button>
        </header>

        <div className="flex-1 overflow-y-auto px-10 py-8">
          <div className="mx-auto max-w-2xl">
            <SectionHeader>{SECTION_TITLES[section]}</SectionHeader>

            {section === "general" && <General prefs={prefs} onPref={onPref} />}
            {section === "keybindings" && <Keybindings />}
            {section === "providers" && (
              <Providers
                models={models}
                currentModelId={currentModelId}
                connections={connections}
                codexLogin={codexLogin}
                settingsError={settingsError}
                onClearError={onClearError}
                onSelectModel={onSelectModel}
                onConnectKey={onConnectKey}
                onRemoveKey={onRemoveKey}
                onStartCodexLogin={onStartCodexLogin}
                onCancelCodexLogin={onCancelCodexLogin}
              />
            )}
            {section === "sourceControl" && <SourceControl />}
            {section === "connections" && <Connections connections={connections} />}
            {section === "archive" && (
              <Archive
                hiddenThreads={hiddenThreads}
                sessions={sessions}
                threadAliases={threadAliases}
                onUnhide={onUnhideThread}
                onDelete={onArchiveThread}
              />
            )}
          </div>
        </div>
      </main>
    </div>
  )
}