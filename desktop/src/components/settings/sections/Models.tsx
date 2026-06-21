import { useMemo, useState } from "react"
import { ExternalLink, RefreshCw, Search, Sparkles } from "lucide-react"
import type { AgentModel, ModelCatalog } from "../../../agent/rpc"
import { cn } from "../../../lib/utils"
import { formatThinkingBadge } from "../../../lib/thinking"
import { EmptyState, SectionHeader, SettingsCard } from "../controls"
import OpenCodeIcon from "../../../assets/icons/OpenCode_dark.svg"
import OpenAIIcon from "../../../assets/icons/OpenAI_dark.svg"
import NeuralWattIcon from "../../../assets/icons/neuralwatt.svg"

export interface ModelsProps {
  catalog: ModelCatalog | null
  onSelect: (provider: string, modelId: string, thinkingLevel: string) => void
  onToggleEnabled?: (modelId: string) => void
  onOpenProviders?: () => void
  onRefreshCatalog?: () => void
}

function speedLabel(model: AgentModel, level: string): string {
  const badge = formatThinkingBadge(model, level)
  const tiers: Record<string, string> = {
    off: "Fast",
    minimal: "Fast",
    low: "Fast",
    medium: "Balanced",
    high: "Slow",
    xhigh: "Slow",
    max: "Slow",
  }
  return tiers[badge.toLowerCase()] ?? "Fast"
}

function labelFor(model: AgentModel, level: string): string {
  return `${model.name} ${speedLabel(model, level)}`
}

function providerIcon(provider: string): string | null {
  switch (provider) {
    case "opencode-go":
    case "opencode-zen":
      return OpenCodeIcon
    case "neuralwatt":
      return NeuralWattIcon
    case "openai-codex":
      return OpenAIIcon
    default:
      return null
  }
}

function providerBadge(provider: string): { label: string; color: string } | null {
  switch (provider) {
    case "opencode-go":
      return { label: "GO", color: "#3b82f6" }
    case "opencode-zen":
      return { label: "ZEN", color: "#a855f7" }
    default:
      return null
  }
}

export function Models({
  catalog,
  onSelect,
  onToggleEnabled,
  onOpenProviders,
  onRefreshCatalog,
}: ModelsProps) {
  const [query, setQuery] = useState("")

  const models = catalog?.models ?? []
  const state = catalog?.state

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return models
    return models.filter(
      (m) =>
        m.name.toLowerCase().includes(q) ||
        m.id.toLowerCase().includes(q) ||
        m.provider.toLowerCase().includes(q),
    )
  }, [models, query])

  if (models.length === 0) {
    return (
      <>
        <SectionHeader>Models</SectionHeader>
        <EmptyState
          icon={<Sparkles className="h-8 w-8" strokeWidth={1.5} />}
          title="No models available"
        >
          Connect a provider in Settings to load the available models. Models added there will
          appear in the composer picker.
          {onOpenProviders && (
            <button
              onClick={onOpenProviders}
              className="mt-4 inline-flex items-center gap-1.5 rounded-lg bg-foreground px-3 py-2 text-xs font-medium text-background transition-colors hover:bg-foreground/90"
            >
              <ExternalLink className="h-3.5 w-3.5" />
              Open Providers
            </button>
          )}
        </EmptyState>
      </>
    )
  }

  return (
    <div className="space-y-5">
      <SettingsCard className="p-4">
        {/* Search / add bar. */}
        <div className="flex items-center gap-2">
          <div className="relative flex flex-1 items-center">
            <Search
              className="pointer-events-none absolute left-3 h-4 w-4 text-muted-foreground"
              strokeWidth={2}
            />
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Add or search model"
              className="h-9 w-full rounded-lg border border-border-strong bg-surface-hover pl-9 pr-3 text-sm text-foreground outline-none placeholder:text-muted-foreground focus-visible:border-accent"
            />
          </div>
          <button
            onClick={onRefreshCatalog}
            className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-surface-hover hover:text-foreground"
            aria-label="Refresh models"
          >
            <RefreshCw className="h-4 w-4" strokeWidth={2} />
          </button>
        </div>

        <div className="my-3 h-px bg-border" />

        {/* Model rows. */}
        <div className="space-y-0.5">
          {filtered.map((model) => {
            const isActive = model.id === state?.modelId && model.provider === state?.provider
            const level = model.thinkingLevels?.length
              ? model.thinkingLevels.includes("medium")
                ? "medium"
                : model.thinkingLevels.find((l) => l !== "off") ?? model.thinkingLevels[0]
              : ""
            return (
              <div
                key={`${model.provider}:${model.id}`}
                className={cn(
                  "group flex w-full items-center justify-between rounded-xl px-3 py-3 transition-colors",
                  isActive ? "bg-surface-hover" : "hover:bg-surface-hover/60",
                )}
              >
                <button
                  type="button"
                  onClick={() => onSelect(model.provider, model.id, level)}
                  className="flex min-w-0 flex-1 cursor-pointer items-center gap-2.5 text-left"
                >
                  {(() => {
                    const icon = providerIcon(model.provider)
                    if (!icon) return null
                    const badge = providerBadge(model.provider)
                    return (
                      <span className="relative shrink-0">
                        <img src={icon} alt="" className="h-5 w-5 rounded-sm opacity-90" />
                        {badge && (
                          <span
                            className="absolute -top-1.5 -left-1.5 flex h-3 min-w-[16px] items-center justify-center rounded-full px-[3px] text-[7px] font-bold leading-none text-background"
                            style={{ background: badge.color }}
                          >
                            {badge.label}
                          </span>
                        )}
                      </span>
                    )
                  })()}
                  <span className="text-[15px] font-medium text-foreground">{model.name}</span>
                </button>
                <button
                  type="button"
                  onClick={(e) => {
                    e.stopPropagation()
                    onToggleEnabled?.(model.id)
                  }}
                  className="flex h-full cursor-pointer items-center px-2 py-1"
                  aria-label={model.enabled === false ? "Enable model" : "Disable model"}
                >
                  <Toggle checked={model.enabled !== false} />
                </button>
              </div>
            )
          })}
        </div>

        {filtered.length === 0 && (
          <div className="px-3 py-6 text-center text-[13px] text-muted-foreground">
            No models match your search.
          </div>
        )}

        <div className="my-2 h-px bg-border" />

        <button
          onClick={() => setQuery("")}
          className="px-3 text-[13px] font-medium text-accent transition-colors hover:text-accent/80"
        >
          View All Models
        </button>
      </SettingsCard>
    </div>
  )
}

function Toggle({ checked }: { checked: boolean }) {
  return (
    <span
      className={cn(
        "relative flex h-[22px] w-[38px] shrink-0 items-center rounded-full transition-colors",
        checked ? "bg-success" : "bg-toggle-off",
      )}
      aria-hidden
    >
      <span
        className={cn(
          "absolute top-1/2 h-[18px] w-[18px] -translate-y-1/2 rounded-full bg-foreground transition-all",
          checked ? "left-[18px]" : "left-[2px]",
        )}
      />
    </span>
  )
}
