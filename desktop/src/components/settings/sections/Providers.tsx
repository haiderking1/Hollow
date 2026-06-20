import { useEffect, useState } from "react"
import type { AgentModel, CodexLoginState, ConnectionInfo } from "../../../agent/rpc"
import { KeyCard, PillSelect, SettingsCard } from "../controls"

// Provider ids (mirror backend/config/config.ts constants).
const OPENCODE = "opencode-go" // opencode + zen share this key slot
const NEURALWATT = "neuralwatt"
const CODEX = "openai-codex"

export interface ProvidersProps {
  models: AgentModel[]
  currentModelId: string | null
  connections: ConnectionInfo[]
  codexLogin: CodexLoginState | null
  settingsError: string | null
  onClearError: () => void
  onSelectModel: (m: AgentModel) => void
  onConnectKey: (provider: string, key: string) => void
  onRemoveKey: (provider: string) => void
  onStartCodexLogin: () => void
  onCancelCodexLogin: () => void
}

export function Providers({
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
}: ProvidersProps) {
  const currentModel = models.find((m) => m.id === currentModelId)
  const providers = Array.from(new Set(models.map((m) => m.provider))).sort()
  const [provider, setProvider] = useState<string>(currentModel?.provider ?? "")
  const [keys, setKeys] = useState<Record<string, string>>({})
  const [pending, setPending] = useState<string | null>(null)

  useEffect(() => {
    if (currentModel) setProvider(currentModel.provider)
  }, [currentModel?.provider])

  useEffect(() => {
    setPending(null)
  }, [connections, settingsError])

  const conn = (id: string) => connections.find((c) => c.provider === id)
  const opencodeConnected = conn(OPENCODE)?.connected ?? false
  const neuralwattConnected = conn(NEURALWATT)?.connected ?? false
  const codexConnected = conn(CODEX)?.connected ?? false
  const anyConnected = connections.some((c) => c.connected)
  const providerModels = models.filter((m) => m.provider === provider)

  const connect = (p: string) => {
    setPending(p)
    onConnectKey(p, keys[p] ?? "")
    setKeys((k) => ({ ...k, [p]: "" }))
  }
  const disconnect = (p: string) => {
    setPending(p)
    onRemoveKey(p)
  }

  return (
    <div className="space-y-4">
      {settingsError && (
        <div className="flex items-start gap-2 rounded-lg border border-red-500/40 bg-red-500/10 p-2.5 text-[11px] text-red-300">
          <span className="flex-1">{settingsError}</span>
          <button onClick={onClearError} className="text-red-300/70 hover:text-red-200">
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.25" strokeLinecap="round">
              <path d="M18 6L6 18M6 6l12 12" />
            </svg>
          </button>
        </div>
      )}

      <KeyCard
        title="OpenCode Go + Zen"
        hint="Shared API key"
        connected={opencodeConnected}
        pending={pending === OPENCODE}
        keyValue={keys[OPENCODE] ?? ""}
        onKeyChange={(v) => setKeys((k) => ({ ...k, [OPENCODE]: v }))}
        onConnect={() => connect(OPENCODE)}
        onDisconnect={() => disconnect(OPENCODE)}
      />
      <KeyCard
        title="NeuralWatt"
        hint="API key"
        connected={neuralwattConnected}
        pending={pending === NEURALWATT}
        keyValue={keys[NEURALWATT] ?? ""}
        onKeyChange={(v) => setKeys((k) => ({ ...k, [NEURALWATT]: v }))}
        onConnect={() => connect(NEURALWATT)}
        onDisconnect={() => disconnect(NEURALWATT)}
      />

      {/* Codex uses OAuth device-code login, not a pasteable key. */}
      <div className="rounded-2xl border border-white/[0.06] bg-[#17171A] p-4">
        <div className="flex items-center justify-between">
          <div>
            <div className="text-xs font-semibold text-white">OpenAI Codex</div>
            <div className="text-[10px] text-[#8E8E93]">Sign in with browser</div>
          </div>
          <span
            className={
              codexConnected
                ? "rounded-full bg-emerald-500/15 px-2 py-0.5 text-[10px] font-medium text-emerald-400"
                : "rounded-full bg-white/[0.06] px-2 py-0.5 text-[10px] font-medium text-[#8E8E93]"
            }
          >
            {codexConnected ? "Connected" : "Not connected"}
          </span>
        </div>
        {codexLogin ? (
          <div className="mt-3 space-y-2">
            <div className="text-[11px] text-[#8E8E93]">Open this URL and enter the code:</div>
            <a href={codexLogin.verify_url} target="_blank" rel="noreferrer" className="block break-all text-[11px] text-[#3B82F6] underline">
              {codexLogin.verify_url}
            </a>
            <div className="select-all font-mono text-base tracking-[0.3em] text-white">{codexLogin.user_code}</div>
            <div className="text-[10px] text-[#8E8E93]">Waiting for browser sign-in…</div>
            <button
              onClick={onCancelCodexLogin}
              className="w-full rounded-lg border border-white/10 bg-[#1c1c1f] px-3 py-2 text-xs text-white transition-colors hover:bg-white/[0.06]"
            >
              Cancel
            </button>
          </div>
        ) : codexConnected ? (
          <button
            onClick={() => disconnect(CODEX)}
            disabled={pending === CODEX}
            className="mt-3 w-full rounded-lg border border-white/10 bg-[#1c1c1f] px-3 py-2 text-xs text-white transition-colors hover:bg-white/[0.06] disabled:opacity-50"
          >
            {pending === CODEX ? "Disconnecting…" : "Disconnect"}
          </button>
        ) : (
          <button
            onClick={() => {
              setPending(CODEX)
              onStartCodexLogin()
            }}
            disabled={pending === CODEX}
            className="mt-3 w-full rounded-lg bg-[#3B82F6] px-3 py-2 text-xs font-medium text-white transition-colors hover:bg-[#3B82F6]/90 disabled:opacity-50"
          >
            {pending === CODEX ? "Starting…" : "Connect Codex"}
          </button>
        )}
      </div>

      {/* Active model */}
      <div className="space-y-3 border-t border-white/[0.06] pt-5">
        <SettingsCard>
          <div className="flex items-center justify-between gap-6 py-4">
            <div>
              <div className="text-[15px] font-semibold text-white">Provider</div>
              <p className="mt-1 text-[13px] text-[#8E8E93]">
                {anyConnected ? "Switch the active provider for new prompts." : "Connect a provider above to switch models."}
              </p>
            </div>
            <PillSelect
              width={150}
              value={provider}
              onChange={setProvider}
              options={providers.map((p) => ({ value: p, label: p }))}
            />
          </div>
          <div className="flex items-center justify-between gap-6 border-t border-white/[0.06] py-4">
            <div className="text-[15px] font-semibold text-white">Model</div>
            <PillSelect
              width={180}
              value={currentModel?.provider === provider ? currentModelId ?? "" : ""}
              onChange={(v) => {
                const m = models.find((mm) => mm.id === v)
                if (m) onSelectModel(m)
              }}
              options={[
                ...(currentModel?.provider !== provider ? [{ value: "", label: "Select a model…" }] : []),
                ...providerModels.map((m) => ({ value: m.id, label: m.name })),
              ]}
            />
          </div>
        </SettingsCard>
      </div>
    </div>
  )
}