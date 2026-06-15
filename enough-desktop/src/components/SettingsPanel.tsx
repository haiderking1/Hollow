import { useEffect, useState } from "react";
import type { AgentModel } from "../agent/rpc";
import { cn } from "../lib/utils";

interface SettingsPanelProps {
  open: boolean;
  onClose: () => void;
  models: AgentModel[];
  currentModelId: string | null;
  onSelectModel: (model: AgentModel) => void;
}

function SettingsSelect({ children, ...props }: React.SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <div className="relative flex w-full items-center">
      <select
        {...props}
        className="w-full cursor-pointer appearance-none rounded-lg border border-border bg-surface px-3 py-2 text-xs text-foreground outline-none transition-colors hover:border-border-strong hover:bg-surface-hover focus-visible:border-accent"
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

export default function SettingsPanel({ open, onClose, models, currentModelId, onSelectModel }: SettingsPanelProps) {
  const currentModel = models.find((m) => m.id === currentModelId);
  const providers = Array.from(new Set(models.map((m) => m.provider))).sort();
  const [provider, setProvider] = useState<string>(currentModel?.provider ?? "");

  // Follow the active model's provider when it changes from outside.
  useEffect(() => {
    if (currentModel) setProvider(currentModel.provider);
  }, [currentModel?.provider]);

  const providerModels = models.filter((m) => m.provider === provider);

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
          <div>
            <FieldLabel>Provider</FieldLabel>
            <SettingsSelect
              value={provider}
              onChange={(e) => setProvider(e.target.value)}
            >
              {providers.map((p) => (
                <option key={p} value={p} className="bg-surface text-foreground">{p}</option>
              ))}
            </SettingsSelect>
          </div>

          <div>
            <FieldLabel>Model</FieldLabel>
            <SettingsSelect
              value={currentModel?.provider === provider ? currentModelId ?? "" : ""}
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
