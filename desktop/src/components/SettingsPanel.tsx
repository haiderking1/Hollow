import { useEffect, useState } from "react";
import type { AgentModel, CodexLoginState, ConnectionInfo } from "../agent/rpc";
import { cn } from "../lib/utils";

// Provider ids (mirror backend/config/config.ts constants).
const OPENCODE = "opencode-go"; // opencode + zen share this key slot
const NEURALWATT = "neuralwatt";
const CODEX = "openai-codex";

interface SettingsPanelProps {
  open: boolean;
  onClose: () => void;
  models: AgentModel[];
  currentModelId: string | null;
  onSelectModel: (model: AgentModel) => void;
  connections: ConnectionInfo[];
  codexLogin: CodexLoginState | null;
  settingsError: string | null;
  onClearError: () => void;
  onConnectKey: (provider: string, key: string) => void;
  onRemoveKey: (provider: string) => void;
  onStartCodexLogin: () => void;
  onCancelCodexLogin: () => void;
}

function SettingsSelect({ children, ...props }: React.SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <div className="relative flex w-full items-center">
      <select
        {...props}
        className="w-full cursor-pointer appearance-none rounded-lg border border-border bg-surface px-3 py-2 text-xs text-foreground outline-none transition-colors hover:border-border-strong hover:bg-surface-hover focus-visible:border-accent disabled:opacity-50"
      >
        {children}
      </select>
      <div className="pointer-events-none absolute right-3 flex items-center text-muted-foreground">
        <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round">
          <path d="M6 9l6 6 6-6" />
        </svg>
      </div>
    </div>
  );
}

function FieldLabel({ children }: { children: React.ReactNode }) {
  return (
    <label className="mb-2 block text-[10px] font-semibold uppercase tracking-wider text-muted-foreground">
      {children}
    </label>
  );
}

function StatusBadge({ connected }: { connected: boolean }) {
  return (
    <span
      className={cn(
        "rounded-full px-2 py-0.5 text-[10px] font-medium",
        connected ? "bg-emerald-500/15 text-emerald-400" : "bg-surface-hover text-muted-foreground",
      )}
    >
      {connected ? "Connected" : "Not connected"}
    </span>
  );
}

interface KeyCardProps {
  title: string;
  hint: string;
  connected: boolean;
  pending: boolean;
  keyValue: string;
  onKeyChange: (v: string) => void;
  onConnect: () => void;
  onDisconnect: () => void;
}

function KeyCard({ title, hint, connected, pending, keyValue, onKeyChange, onConnect, onDisconnect }: KeyCardProps) {
  return (
    <div className="rounded-xl border border-border bg-surface p-3.5">
      <div className="flex items-center justify-between">
        <div>
          <div className="text-xs font-semibold text-foreground">{title}</div>
          <div className="text-[10px] text-muted-foreground">{hint}</div>
        </div>
        <StatusBadge connected={connected} />
      </div>
      {connected ? (
        <button
          onClick={onDisconnect}
          disabled={pending}
          className="mt-3 w-full rounded-lg border border-border bg-surface px-3 py-2 text-xs text-foreground transition-colors hover:bg-surface-hover disabled:opacity-50"
        >
          {pending ? "Disconnecting…" : "Disconnect"}
        </button>
      ) : (
        <div className="mt-3 flex gap-2">
          <input
            type="password"
            value={keyValue}
            onChange={(e) => onKeyChange(e.target.value)}
            placeholder="Paste API key"
            autoComplete="off"
            spellCheck={false}
            className="min-w-0 flex-1 rounded-lg border border-border bg-surface px-3 py-2 text-xs text-foreground outline-none focus-visible:border-accent"
          />
          <button
            onClick={onConnect}
            disabled={pending || keyValue.trim() === ""}
            className="shrink-0 rounded-lg bg-accent px-3 py-2 text-xs font-medium text-white transition-colors hover:bg-accent/90 disabled:opacity-50"
          >
            {pending ? "Connecting…" : "Connect"}
          </button>
        </div>
      )}
    </div>
  );
}

export default function SettingsPanel({
  open,
  onClose,
  models,
  currentModelId,
  onSelectModel,
  connections,
  codexLogin,
  settingsError,
  onClearError,
  onConnectKey,
  onRemoveKey,
  onStartCodexLogin,
  onCancelCodexLogin,
}: SettingsPanelProps) {
  const currentModel = models.find((m) => m.id === currentModelId);
  const providers = Array.from(new Set(models.map((m) => m.provider))).sort();
  const [provider, setProvider] = useState<string>(currentModel?.provider ?? "");
  const [keys, setKeys] = useState<Record<string, string>>({});
  const [pending, setPending] = useState<string | null>(null);

  // Follow the active model's provider when it changes from outside.
  useEffect(() => {
    if (currentModel) setProvider(currentModel.provider);
  }, [currentModel?.provider]);

  // A connection update or an error means the pending action resolved.
  useEffect(() => {
    setPending(null);
  }, [connections, settingsError]);

  const conn = (id: string) => connections.find((c) => c.provider === id);
  const opencodeConnected = conn(OPENCODE)?.connected ?? false;
  const neuralwattConnected = conn(NEURALWATT)?.connected ?? false;
  const codexConnected = conn(CODEX)?.connected ?? false;
  const anyConnected = connections.some((c) => c.connected);
  const providerModels = models.filter((m) => m.provider === provider);

  const connect = (p: string) => {
    setPending(p);
    onConnectKey(p, keys[p] ?? "");
    setKeys((k) => ({ ...k, [p]: "" }));
  };
  const disconnect = (p: string) => {
    setPending(p);
    onRemoveKey(p);
  };

  return (
    <>
      <div
        className={cn(
          "fixed inset-0 z-40 bg-black/45 backdrop-blur-[3px] transition-opacity duration-200 ease-in-out",
          open ? "pointer-events-auto opacity-100" : "pointer-events-none opacity-0",
        )}
        onClick={onClose}
      />
      <div
        className={cn(
          "fixed top-0 right-0 bottom-0 z-50 flex w-80 flex-col border-l border-border bg-sidebar transition-transform duration-200 ease-out",
          open ? "translate-x-0" : "translate-x-full",
        )}
        style={{ boxShadow: open ? "-16px 0 40px rgba(0,0,0,0.4)" : "none" }}
      >
        <div className="flex h-12 shrink-0 items-center justify-between border-b border-border px-4">
          <span className="text-[14px] font-semibold text-foreground">Settings</span>
          <button
            onClick={onClose}
            className="flex h-6 w-6 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-surface-hover hover:text-foreground"
          >
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.25" strokeLinecap="round">
              <path d="M18 6L6 18M6 6l12 12" />
            </svg>
          </button>
        </div>

        <div className="flex-1 space-y-6 overflow-y-auto p-5">
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

          <div className="space-y-3">
            <FieldLabel>Providers</FieldLabel>
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
            <div className="rounded-xl border border-border bg-surface p-3.5">
              <div className="flex items-center justify-between">
                <div>
                  <div className="text-xs font-semibold text-foreground">OpenAI Codex</div>
                  <div className="text-[10px] text-muted-foreground">Sign in with browser</div>
                </div>
                <StatusBadge connected={codexConnected} />
              </div>
              {codexLogin ? (
                <div className="mt-3 space-y-2">
                  <div className="text-[11px] text-muted-foreground">
                    Open this URL and enter the code:
                  </div>
                  <a
                    href={codexLogin.verify_url}
                    target="_blank"
                    rel="noreferrer"
                    className="block break-all text-[11px] text-accent underline"
                  >
                    {codexLogin.verify_url}
                  </a>
                  <div className="select-all font-mono text-base tracking-[0.3em] text-foreground">
                    {codexLogin.user_code}
                  </div>
                  <div className="text-[10px] text-muted-foreground">Waiting for browser sign-in…</div>
                  <button
                    onClick={onCancelCodexLogin}
                    className="w-full rounded-lg border border-border bg-surface px-3 py-2 text-xs text-foreground transition-colors hover:bg-surface-hover"
                  >
                    Cancel
                  </button>
                </div>
              ) : codexConnected ? (
                <button
                  onClick={() => disconnect(CODEX)}
                  disabled={pending === CODEX}
                  className="mt-3 w-full rounded-lg border border-border bg-surface px-3 py-2 text-xs text-foreground transition-colors hover:bg-surface-hover disabled:opacity-50"
                >
                  {pending === CODEX ? "Disconnecting…" : "Disconnect"}
                </button>
              ) : (
                <button
                  onClick={() => {
                    setPending(CODEX);
                    onStartCodexLogin();
                  }}
                  disabled={pending === CODEX}
                  className="mt-3 w-full rounded-lg bg-accent px-3 py-2 text-xs font-medium text-white transition-colors hover:bg-accent/90 disabled:opacity-50"
                >
                  {pending === CODEX ? "Starting…" : "Connect Codex"}
                </button>
              )}
            </div>
          </div>

          <div>
            <FieldLabel>Provider</FieldLabel>
            <SettingsSelect
              value={provider}
              disabled={!anyConnected}
              onChange={(e) => setProvider(e.target.value)}
            >
              {providers.map((p) => (
                <option key={p} value={p} className="bg-surface text-foreground">{p}</option>
              ))}
            </SettingsSelect>
            {!anyConnected && (
              <p className="mt-1.5 text-[10px] text-muted-foreground">
                Connect a provider above to switch models.
              </p>
            )}
          </div>

          <div>
            <FieldLabel>Model</FieldLabel>
            <SettingsSelect
              value={currentModel?.provider === provider ? currentModelId ?? "" : ""}
              disabled={!anyConnected}
              onChange={(e) => {
                const m = models.find((mm) => mm.id === e.target.value);
                if (m) onSelectModel(m);
              }}
            >
              {currentModel?.provider !== provider && (
                <option value="" className="bg-surface text-muted-foreground">Select a model…</option>
              )}
              {providerModels.map((m) => (
                <option key={m.id} value={m.id} className="bg-surface text-foreground">{m.name}</option>
              ))}
            </SettingsSelect>
          </div>
        </div>

        <div className="shrink-0 border-t border-border p-5">
          <FieldLabel>About</FieldLabel>
          <div className="space-y-2 rounded-xl border border-border bg-surface p-3.5 text-xs text-foreground/80">
            <div className="flex justify-between">
              <span className="text-muted-foreground">Version</span>
              <span className="font-medium text-foreground">0.1.0</span>
            </div>
            <div className="flex justify-between">
              <span className="text-muted-foreground">Core</span>
              <span className="font-medium text-foreground">Electron</span>
            </div>
            <div className="flex justify-between">
              <span className="text-muted-foreground">Framework</span>
              <span className="font-medium text-foreground">React 19</span>
            </div>
          </div>
        </div>
      </div>
    </>
  );
}