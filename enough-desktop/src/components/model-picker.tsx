import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { Search, Star } from "lucide-react"
import type { AgentModel, ModelCatalog } from "../agent/rpc"
import {
  isOpenCodeProvider,
  ProviderBrandIcon,
  providerToSidebarTab,
} from "./provider-icons"
import { PickerButton } from "./picker-button"
import { ThinkingPicker } from "./thinking-picker"
import {
  formatThinkingBadge,
} from "../lib/thinking"
import { cn } from "../lib/utils"

const FAVORITES_KEY = "enough-favorite-models"

type SidebarTab = "favorites" | "opencode" | "openai-codex"

interface ModelPickerProps {
  catalog: ModelCatalog | null
  disabled?: boolean
  isStreaming?: boolean
  onSelect: (provider: string, modelId: string, thinkingLevel: string) => void
}

function loadFavorites(): Set<string> {
  try {
    const raw = localStorage.getItem(FAVORITES_KEY)
    return new Set(raw ? (JSON.parse(raw) as string[]) : [])
  } catch {
    return new Set()
  }
}

function saveFavorites(favs: Set<string>) {
  localStorage.setItem(FAVORITES_KEY, JSON.stringify([...favs]))
}

function providerShortName(id: string) {
  if (id === "openai-codex") return "Codex"
  if (id === "opencode-zen") return "Zen"
  if (id === "opencode-go") return "Go"
  return id
}

function defaultThinking(model: AgentModel) {
  const levels = model.thinkingLevels ?? []
  if (levels.length <= 1) return ""
  if (levels.includes("medium")) return "medium"
  return levels.find((l) => l !== "off") ?? levels[0]
}

function modelBadgeThinking(
  model: AgentModel,
  isActive: boolean,
  isHighlight: boolean,
  pickerThinking: string,
  catalogThinking: string,
): string {
  const levels = model.thinkingLevels ?? []
  if (levels.length > 1) {
    if (isHighlight || isActive) return pickerThinking || defaultThinking(model)
    if (catalogThinking && levels.includes(catalogThinking)) return catalogThinking
    return defaultThinking(model)
  }
  return levels[0] ?? ""
}

function modelMatchesTab(model: AgentModel, tab: SidebarTab) {
  if (tab === "favorites") return false
  if (tab === "opencode") return isOpenCodeProvider(model.provider)
  return model.provider === tab
}

export function ModelPicker({ catalog, disabled, isStreaming, onSelect }: ModelPickerProps) {
  const state = catalog?.state
  const [open, setOpen] = useState(false)
  const [sidebarTab, setSidebarTab] = useState<SidebarTab>(
    state?.provider ? providerToSidebarTab(state.provider) : "opencode",
  )
  const [query, setQuery] = useState("")
  const [highlight, setHighlight] = useState(0)
  const [thinking, setThinking] = useState(state?.thinkingLevel ?? "")
  const [favorites, setFavorites] = useState<Set<string>>(loadFavorites)
  const rootRef = useRef<HTMLDivElement>(null)
  const searchRef = useRef<HTMLInputElement>(null)

  const providers = catalog?.providers ?? []
  const models = catalog?.models ?? []

  useEffect(() => {
    if (!state?.provider) return
    setSidebarTab(providerToSidebarTab(state.provider))
    setThinking(state.thinkingLevel)
  }, [state?.provider, state?.thinkingLevel])

  const opencodeConnected = useMemo(
    () => providers.some((p) => isOpenCodeProvider(p.id) && p.connected),
    [providers],
  )
  const codexConnected = useMemo(
    () => providers.some((p) => p.id === "openai-codex" && p.connected),
    [providers],
  )

  const listModels = useMemo(() => {
    let items: AgentModel[]
    if (sidebarTab === "favorites") {
      items = models.filter((m) => favorites.has(m.id))
    } else {
      items = models.filter((m) => modelMatchesTab(m, sidebarTab))
    }
    const q = query.trim().toLowerCase()
    if (q) {
      items = items.filter(
        (m) =>
          m.name.toLowerCase().includes(q) ||
          m.id.toLowerCase().includes(q) ||
          providerShortName(m.provider).toLowerCase().includes(q),
      )
    }
    return items
  }, [sidebarTab, models, favorites, query])

  const activeModel = useMemo(
    () => models.find((m) => m.id === state?.modelId && m.provider === state?.provider),
    [models, state?.modelId, state?.provider],
  )

  const thinkingLevels = activeModel?.thinkingLevels ?? []

  const apply = useCallback(
    (model: AgentModel, nextThinking?: string) => {
      const p = providers.find((item) => item.id === model.provider)
      if (p && !p.connected) return
      const level = nextThinking ?? defaultThinking(model)
      setThinking(level)
      onSelect(model.provider, model.id, level)
      setOpen(false)
      setQuery("")
    },
    [onSelect, providers],
  )

  const toggleFavorite = (id: string, e: React.MouseEvent) => {
    e.stopPropagation()
    setFavorites((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      saveFavorites(next)
      return next
    })
  }

  useEffect(() => {
    setHighlight(0)
  }, [sidebarTab, query])

  useEffect(() => {
    if (!open) return
    const t = window.setTimeout(() => searchRef.current?.focus(), 30)
    return () => window.clearTimeout(t)
  }, [open])

  useEffect(() => {
    if (!open) return
    const onDoc = (e: MouseEvent) => {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener("mousedown", onDoc)
    return () => document.removeEventListener("mousedown", onDoc)
  }, [open])

  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        setOpen(false)
        return
      }
      if (e.ctrlKey || e.metaKey) {
        const num = parseInt(e.key, 10)
        if (num >= 1 && num <= 9 && listModels[num - 1]) {
          e.preventDefault()
          apply(listModels[num - 1])
        }
        return
      }
      if (e.key === "ArrowDown") {
        e.preventDefault()
        setHighlight((i) => Math.min(i + 1, Math.max(listModels.length - 1, 0)))
      } else if (e.key === "ArrowUp") {
        e.preventDefault()
        setHighlight((i) => Math.max(i - 1, 0))
      } else if (e.key === "Enter" && listModels[highlight]) {
        e.preventDefault()
        apply(listModels[highlight])
      }
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [open, listModels, highlight, apply])

  if (!catalog || providers.length === 0) {
    return (
      <div className="flex items-center py-1 text-[12px] text-muted-foreground">
        Loading models…
      </div>
    )
  }

  const triggerLabel = state?.modelName ?? "Model"
  const triggerBrand = state?.provider ? providerToSidebarTab(state.provider) : "opencode"

  return (
    <div className="flex items-center gap-2 py-1">
      <div ref={rootRef} className="relative shrink-0">
        <PickerButton
          icon={<ProviderBrandIcon id={triggerBrand} className="h-3.5 w-3.5" />}
          label={triggerLabel}
          open={open}
          disabled={disabled}
          onClick={() => setOpen((o) => !o)}
        />

        {open && (
          <div className="absolute bottom-full left-0 z-50 mb-2 w-[min(100vw-3rem,420px)] overflow-hidden rounded-xl border border-border-strong bg-[#121211] shadow-2xl">
            <div className="flex h-[360px]">
              <aside className="flex w-[48px] shrink-0 flex-col items-center gap-1 border-r border-border bg-[#0d0d0c] py-2">
                <SidebarBtn
                  active={sidebarTab === "favorites"}
                  onClick={() => setSidebarTab("favorites")}
                  title="Favorites"
                >
                  <Star className="h-4 w-4 text-muted-foreground" strokeWidth={1.75} />
                </SidebarBtn>
                <div className="my-1 h-px w-5 bg-border" />
                <SidebarBtn
                  active={sidebarTab === "opencode"}
                  onClick={() => setSidebarTab("opencode")}
                  title="OpenCode"
                  dimmed={!opencodeConnected}
                >
                  <ProviderBrandIcon id="opencode" className={!opencodeConnected ? "opacity-40" : undefined} />
                </SidebarBtn>
                <SidebarBtn
                  active={sidebarTab === "openai-codex"}
                  onClick={() => setSidebarTab("openai-codex")}
                  title="OpenAI Codex"
                  dimmed={!codexConnected}
                >
                  <ProviderBrandIcon id="openai-codex" className={!codexConnected ? "opacity-40" : undefined} />
                </SidebarBtn>
              </aside>

              <div className="flex min-w-0 flex-1 flex-col">
                <div className="flex items-center gap-2 border-b border-border px-3 py-2">
                  <Search className="h-3.5 w-3.5 shrink-0 text-muted-foreground" strokeWidth={2} />
                  <input
                    ref={searchRef}
                    value={query}
                    onChange={(e) => setQuery(e.target.value)}
                    placeholder="Search models..."
                    className="w-full bg-transparent text-[12px] text-foreground placeholder:text-muted-foreground/70 focus:outline-none"
                  />
                </div>

                <div className="min-h-0 flex-1 overflow-y-auto">
                  {listModels.length === 0 ? (
                    <div className="px-4 py-8 text-center text-[12px] text-muted-foreground">
                      {sidebarTab === "favorites"
                        ? "No favorites yet"
                        : sidebarTab === "opencode" && !opencodeConnected
                          ? "Connect OpenCode first"
                          : sidebarTab === "openai-codex" && !codexConnected
                            ? "Connect Codex first"
                            : "No models found"}
                    </div>
                  ) : (
                    listModels.map((model, index) => {
                      const provider = providers.find((p) => p.id === model.provider)
                      const isActive = model.id === state?.modelId && model.provider === state?.provider
                      const isHighlight = index === highlight
                      const shortcut = index < 9 ? `Ctrl+${index + 1}` : null
                      const badge = formatThinkingBadge(
                        model,
                        modelBadgeThinking(
                          model,
                          isActive,
                          isHighlight,
                          thinking,
                          state?.thinkingLevel ?? "",
                        ),
                      )

                      return (
                        <div
                          key={`${model.provider}:${model.id}`}
                          role="button"
                          tabIndex={0}
                          onMouseEnter={() => setHighlight(index)}
                          onClick={() => provider?.connected !== false && apply(model)}
                          onKeyDown={(e) => {
                            if (e.key === "Enter" || e.key === " ") {
                              e.preventDefault()
                              if (provider?.connected !== false) apply(model)
                            }
                          }}
                          className={cn(
                            "flex w-full cursor-pointer items-center gap-2 border-b border-border/40 px-3 py-2 text-left transition-colors",
                            "hover:bg-surface-hover/80",
                            provider && !provider.connected && "cursor-not-allowed opacity-40",
                            (isActive || isHighlight) && "bg-surface-hover/60",
                          )}
                        >
                          <button
                            type="button"
                            onClick={(e) => toggleFavorite(model.id, e)}
                            className="flex h-5 w-5 shrink-0 items-center justify-center rounded text-muted-foreground hover:text-foreground"
                            aria-label={favorites.has(model.id) ? "Remove favorite" : "Add favorite"}
                          >
                            <Star
                              className={cn(
                                "h-3 w-3",
                                favorites.has(model.id) ? "fill-amber-400 text-amber-400" : "",
                              )}
                              strokeWidth={1.75}
                            />
                          </button>

                          <div className="min-w-0 flex-1">
                            <div className="truncate text-[13px] font-medium text-foreground">{model.name}</div>
                            <div className="mt-0.5 flex items-center gap-1 text-[11px] text-muted-foreground">
                              <ProviderBrandIcon id={model.provider} className="h-3 w-3" />
                              <span>{providerShortName(model.provider)}</span>
                              {model.contextLabel && (
                                <>
                                  <span>·</span>
                                  <span>{model.contextLabel}</span>
                                </>
                              )}
                              {badge && (
                                <>
                                  <span>·</span>
                                  <span>{badge}</span>
                                </>
                              )}
                            </div>
                          </div>

                          {shortcut && (
                            <kbd className="hidden shrink-0 rounded border border-border-strong bg-surface px-1 py-0.5 font-mono text-[9px] text-muted-foreground sm:inline">
                              {shortcut}
                            </kbd>
                          )}
                        </div>
                      )
                    })
                  )}
                </div>
              </div>
            </div>
          </div>
        )}
      </div>

      {activeModel && (
        <ThinkingPicker
          model={activeModel}
          levels={thinkingLevels}
          value={thinking}
          disabled={disabled}
          onChange={(level) => {
            setThinking(level)
            onSelect(activeModel.provider, activeModel.id, level)
          }}
        />
      )}

      {isStreaming && (
        <span className="ml-auto block h-3.5 w-3.5 shrink-0 rounded-full border-2 border-muted-foreground/30 border-t-foreground animate-spin [animation-duration:1.2s]" />
      )}
    </div>
  )
}

function SidebarBtn({
  active,
  onClick,
  title,
  dimmed,
  children,
}: {
  active: boolean
  onClick: () => void
  title: string
  dimmed?: boolean
  children: React.ReactNode
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      title={title}
      className={cn(
        "relative flex h-8 w-8 items-center justify-center rounded-lg transition-colors",
        active ? "bg-surface text-foreground" : "text-muted-foreground hover:bg-surface/60 hover:text-foreground",
        dimmed && "opacity-50",
      )}
    >
      {active && (
        <span className="absolute right-0 top-1/2 h-4 w-[2px] -translate-y-1/2 rounded-full bg-info" />
      )}
      {children}
    </button>
  )
}
