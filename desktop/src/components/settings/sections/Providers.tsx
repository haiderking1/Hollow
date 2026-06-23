import { useEffect, useState } from "react"
import { ChevronDown, ExternalLink } from "lucide-react"
import type { CodexLoginState, ConnectionInfo } from "../../../agent/rpc"
import { cn } from "../../../lib/utils"
import OpenCodeIcon from "../../../assets/icons/OpenCode_dark.svg"
import OpenAIIcon from "../../../assets/icons/OpenAI_dark.svg"
import NeuralWattIcon from "../../../assets/icons/neuralwatt.svg"

// Provider ids (mirror backend/config/config.ts constants).
const OPENCODE = "opencode-go" // opencode + zen share this key slot
const NEURALWATT = "neuralwatt"
const CODEX = "openai-codex"

export interface ProvidersProps {
  connections: ConnectionInfo[]
  codexLogin: CodexLoginState | null
  settingsError: string | null
  onClearError: () => void
  onConnectKey: (provider: string, key: string) => void
  onRemoveKey: (provider: string) => void
  onStartCodexLogin: () => void
  onCancelCodexLogin: () => void
}

/* Expandable rectangle card for one provider. Icon + name top-left, a colored
   status dot (green connected / red not connected) on the right, and a chevron
   that flips when the body is expanded. Click the header to expand and reveal
   the key input / OAuth flow. `forceExpand` keeps the body open while a Codex
   device login is in flight so the code stays visible. */
function ProviderCard({
  icon,
  title,
  subtitle,
  connected,
  forceExpand,
  children,
}: {
  icon: React.ReactNode
  title: string
  subtitle: string
  connected: boolean
  forceExpand?: boolean
  children: React.ReactNode
}) {
  const [open, setOpen] = useState(false)
  const expanded = forceExpand || open
  return (
    <div
      className={cn(
        "overflow-hidden rounded-2xl border bg-surface transition-colors",
        connected ? "border-success/30" : "border-border",
      )}
    >
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex w-full items-center gap-3 px-4 py-3.5 text-left"
      >
        <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-[10px] bg-surface-hover">
          {icon}
        </span>
        <div className="min-w-0 flex-1">
          <div className="truncate text-[13px] font-semibold text-foreground">{title}</div>
          <div className="truncate text-[11px] text-muted-foreground">{subtitle}</div>
        </div>
        <span className="flex shrink-0 items-center gap-1.5">
          <span className={cn("h-2 w-2 rounded-full", connected ? "bg-success" : "bg-danger")} />
          <span className={cn("text-[11px] font-medium", connected ? "text-success" : "text-danger")}>
            {connected ? "Connected" : "Not connected"}
          </span>
        </span>
        <ChevronDown
          className={cn(
            "h-4 w-4 shrink-0 text-muted-foreground transition-transform duration-150",
            expanded && "rotate-180",
          )}
        />
      </button>
      {expanded && <div className="border-t border-border px-4 pb-4 pt-3.5">{children}</div>}
    </div>
  )
}

/* Shared "key saved" footer for a connected key provider + Disconnect button. */
function ConnectedRow({
  note,
  pending,
  onDisconnect,
}: {
  note: string
  pending: boolean
  onDisconnect: () => void
}) {
  return (
    <div className="flex items-center justify-between gap-3">
      <span className="text-[11px] text-muted-foreground">{note}</span>
      <button
        onClick={onDisconnect}
        disabled={pending}
        className="shrink-0 rounded-lg border border-border-strong bg-surface-hover px-3 py-2 text-xs text-foreground transition-colors hover:bg-surface-hover/80 disabled:opacity-50"
      >
        {pending ? "Disconnecting…" : "Disconnect"}
      </button>
    </div>
  )
}

/* Key input + Connect button shown when a key provider isn't connected yet. */
function KeyInput({
  value,
  pending,
  onKeyChange,
  onConnect,
}: {
  value: string
  pending: boolean
  onKeyChange: (v: string) => void
  onConnect: () => void
}) {
  return (
    <div className="flex gap-2">
      <input
        type="password"
        value={value}
        onChange={(e) => onKeyChange(e.target.value)}
        placeholder="Paste API key"
        autoComplete="off"
        spellCheck={false}
        className="min-w-0 flex-1 rounded-lg border border-border-strong bg-surface-hover px-3 py-2 text-xs text-foreground outline-none focus-visible:border-accent"
      />
      <button
        onClick={onConnect}
        disabled={pending || value.trim() === ""}
        className="shrink-0 rounded-lg bg-foreground px-3 py-2 text-xs font-medium text-background transition-colors hover:bg-foreground/90 disabled:opacity-50"
      >
        {pending ? "Connecting…" : "Connect"}
      </button>
    </div>
  )
}

export function Providers({
  connections,
  codexLogin,
  settingsError,
  onClearError,
  onConnectKey,
  onRemoveKey,
  onStartCodexLogin,
  onCancelCodexLogin,
}: ProvidersProps) {
  const [keys, setKeys] = useState<Record<string, string>>({})
  const [pending, setPending] = useState<string | null>(null)

  const conn = (id: string) => connections.find((c) => c.provider === id)
  const opencodeConnected = conn(OPENCODE)?.connected ?? false
  const neuralwattConnected = conn(NEURALWATT)?.connected ?? false
  const codexConnected = conn(CODEX)?.connected ?? false

  const connect = (p: string) => {
    setPending(p)
    onConnectKey(p, keys[p] ?? "")
    setKeys((k) => ({ ...k, [p]: "" }))
  }
  const disconnect = (p: string) => {
    setPending(p)
    onRemoveKey(p)
  }

  // Clear the pending spinner once a connection result/error lands.
  useEffect(() => {
    setPending(null)
  }, [connections, settingsError])

  return (
    <div className="space-y-3">
      {settingsError && (
        <div className="flex items-start gap-2 rounded-lg border border-danger/40 bg-danger/10 p-2.5 text-[11px] text-danger">
          <span className="flex-1">{settingsError}</span>
          <button onClick={onClearError} className="text-danger/70 hover:text-danger/80">
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.25" strokeLinecap="round">
              <path d="M18 6L6 18M6 6l12 12" />
            </svg>
          </button>
        </div>
      )}

      <ProviderCard
        icon={<img src={OpenCodeIcon} alt="" className="h-6 w-6" />}
        title="OpenCode Go + Zen"
        subtitle="Shared API key"
        connected={opencodeConnected}
      >
        {opencodeConnected ? (
          <ConnectedRow
            note="Key saved locally in ~/.hollow"
            pending={pending === OPENCODE}
            onDisconnect={() => disconnect(OPENCODE)}
          />
        ) : (
          <KeyInput
            value={keys[OPENCODE] ?? ""}
            pending={pending === OPENCODE}
            onKeyChange={(v) => setKeys((k) => ({ ...k, [OPENCODE]: v }))}
            onConnect={() => connect(OPENCODE)}
          />
        )}
      </ProviderCard>

      <ProviderCard
        icon={<img src={NeuralWattIcon} alt="" className="h-6 w-6" />}
        title="NeuralWatt"
        subtitle="API key"
        connected={neuralwattConnected}
      >
        {neuralwattConnected ? (
          <ConnectedRow
            note="Key saved locally in ~/.hollow"
            pending={pending === NEURALWATT}
            onDisconnect={() => disconnect(NEURALWATT)}
          />
        ) : (
          <KeyInput
            value={keys[NEURALWATT] ?? ""}
            pending={pending === NEURALWATT}
            onKeyChange={(v) => setKeys((k) => ({ ...k, [NEURALWATT]: v }))}
            onConnect={() => connect(NEURALWATT)}
          />
        )}
      </ProviderCard>

      {/* Codex uses OAuth device-code login, not a pasteable key. */}
      <ProviderCard
        icon={<img src={OpenAIIcon} alt="" className="h-6 w-6" />}
        title="OpenAI Codex"
        subtitle="Sign in with browser"
        connected={codexConnected}
        forceExpand={!!codexLogin}
      >
        {codexLogin ? (
          <div className="space-y-3">
            <div className="rounded-lg bg-surface-hover px-3 py-2.5">
              <div className="text-[10px] text-muted-foreground">Enter this code in the browser</div>
              <div className="select-all font-mono text-base tracking-[0.3em] text-foreground">
                {codexLogin.user_code}
              </div>
            </div>
            <a
              href={codexLogin.verify_url}
              target="_blank"
              rel="noreferrer"
              className="flex w-full items-center justify-center gap-2 rounded-lg bg-foreground px-3 py-2.5 text-xs font-medium text-background transition-colors hover:bg-foreground/90"
            >
              <ExternalLink className="h-3.5 w-3.5" />
              Open in browser
            </a>
            <div className="flex items-center justify-between">
              <span className="text-[10px] text-muted-foreground">Waiting for sign-in…</span>
              <button
                onClick={onCancelCodexLogin}
                className="rounded-lg border border-border-strong bg-surface-hover px-3 py-1.5 text-[11px] text-foreground transition-colors hover:bg-surface-hover/80"
              >
                Cancel
              </button>
            </div>
          </div>
        ) : codexConnected ? (
          <ConnectedRow
            note="Signed in with OpenAI"
            pending={pending === CODEX}
            onDisconnect={() => disconnect(CODEX)}
          />
        ) : (
          <div className="space-y-2">
            <button
              onClick={() => {
                setPending(CODEX)
                onStartCodexLogin()
              }}
              disabled={pending === CODEX}
              className="w-full rounded-lg bg-foreground px-3 py-2.5 text-xs font-medium text-background transition-colors hover:bg-foreground/90 disabled:opacity-50"
            >
              {pending === CODEX ? "Starting…" : "Connect Codex"}
            </button>
            <p className="text-[10px] text-muted-foreground">Opens your browser to sign in with OpenAI.</p>
          </div>
        )}
      </ProviderCard>
    </div>
  )
}