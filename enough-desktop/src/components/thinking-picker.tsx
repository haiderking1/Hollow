import { useEffect, useRef, useState } from "react"
import { Brain, Check } from "lucide-react"
import type { AgentModel } from "../agent/rpc"
import { thinkingLevelHint, thinkingLevelLabel } from "../lib/thinking"
import { PickerButton } from "./picker-button"
import { cn } from "../lib/utils"

interface ThinkingPickerProps {
  model?: AgentModel
  levels: string[]
  value: string
  disabled?: boolean
  onChange: (level: string) => void
}

export function ThinkingPicker({ model, levels, value, disabled, onChange }: ThinkingPickerProps) {
  const [open, setOpen] = useState(false)
  const [highlight, setHighlight] = useState(0)
  const rootRef = useRef<HTMLDivElement>(null)

  const valueIndex = Math.max(0, levels.indexOf(value))
  const valueLabel = thinkingLevelLabel(model, value, valueIndex)

  useEffect(() => {
    setHighlight(valueIndex)
  }, [levels, value, open, valueIndex])

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
      if (e.key === "ArrowDown") {
        e.preventDefault()
        setHighlight((i) => Math.min(i + 1, levels.length - 1))
      } else if (e.key === "ArrowUp") {
        e.preventDefault()
        setHighlight((i) => Math.max(i - 1, 0))
      } else if (e.key === "Enter" && levels[highlight]) {
        e.preventDefault()
        onChange(levels[highlight])
        setOpen(false)
      }
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [open, levels, highlight, onChange])

  if (levels.length <= 1) return null

  return (
    <div ref={rootRef} className="relative shrink-0">
      <PickerButton
        icon={<Brain className="h-3.5 w-3.5" strokeWidth={1.75} />}
        label={valueLabel}
        open={open}
        disabled={disabled}
        onClick={() => setOpen((o) => !o)}
      />

      {open && (
        <div className="absolute bottom-full left-0 z-50 mb-2 w-[220px] overflow-hidden rounded-xl border border-border-strong bg-[#121211] shadow-2xl ring-1 ring-white/5">
          <div className="flex items-center gap-2 border-b border-border bg-[#0d0d0c] px-3 py-2">
            <Brain className="h-3.5 w-3.5 text-muted-foreground" strokeWidth={1.75} />
            <span className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
              Thinking
            </span>
          </div>

          <div className="p-1">
            {levels.map((level, index) => {
              const active = level === value
              const highlighted = index === highlight
              const label = thinkingLevelLabel(model, level, index)
              const hint = thinkingLevelHint(model?.id ?? "", level)
              return (
                <button
                  key={level}
                  type="button"
                  onMouseEnter={() => setHighlight(index)}
                  onClick={() => {
                    onChange(level)
                    setOpen(false)
                  }}
                  className={cn(
                    "relative flex w-full items-center gap-2.5 rounded-lg px-2 py-2 text-left transition-colors",
                    "hover:bg-surface-hover/80",
                    (active || highlighted) && "bg-surface-hover/60",
                  )}
                >
                  <span className="flex w-4 shrink-0 items-center justify-center">
                    {active && <Check className="h-3.5 w-3.5 text-info" strokeWidth={2} />}
                  </span>

                  <div className="min-w-0 flex-1">
                    <div
                      className={cn(
                        "text-[13px] font-medium leading-tight capitalize",
                        active ? "text-foreground" : "text-foreground/90",
                      )}
                    >
                      {label}
                    </div>
                    <div className="mt-0.5 text-[11px] leading-tight text-muted-foreground">{hint}</div>
                  </div>

                  {active && (
                    <span className="absolute right-1 top-1/2 h-4 w-[2px] -translate-y-1/2 rounded-full bg-info" />
                  )}
                </button>
              )
            })}
          </div>
        </div>
      )}
    </div>
  )
}
