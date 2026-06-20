import { ChevronDown } from "lucide-react"
import { cn } from "../../lib/utils"

/* Toggle switch — pill 44×24, fully rounded. Off track #2C2C2E with a darker
   gray thumb; on track #3B82F6 (the single blue accent) with a white thumb. */
export function Toggle({ checked, onChange }: { checked: boolean; onChange: (v: boolean) => void }) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      onClick={() => onChange(!checked)}
      className={cn(
        "relative h-6 w-11 shrink-0 rounded-full transition-colors duration-150",
        checked ? "bg-[#3B82F6]" : "bg-[#2C2C2E]",
      )}
    >
      <span
        className={cn(
          "absolute top-1/2 h-[18px] w-[18px] -translate-y-1/2 rounded-full transition-all duration-150",
          checked ? "left-[23px] bg-white" : "left-[3px] bg-[#6B6B70]",
        )}
      />
    </button>
  )
}

/* Pill dropdown — ~150px wide, ~36px tall, radius 10px, dark fill, white text,
   1px rgba(255,255,255,0.10) border + chevron. Uses a styled native select. */
export function PillSelect({
  value,
  onChange,
  options,
  width = 150,
}: {
  value: string
  onChange: (v: string) => void
  options: { value: string; label: string }[]
  width?: number
}) {
  return (
    <div className="relative inline-flex items-center" style={{ width }}>
      <select
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="h-9 w-full cursor-pointer appearance-none rounded-[10px] border border-white/10 bg-[#1c1c1f] pl-3 pr-8 text-sm text-white outline-none transition-colors hover:border-white/20 focus-visible:border-[#3B82F6]"
      >
        {options.map((o) => (
          <option key={o.value} value={o.value} className="bg-[#17171A] text-white">
            {o.label}
          </option>
        ))}
      </select>
      <ChevronDown className="pointer-events-none absolute right-2.5 h-4 w-4 text-[#8E8E93]" strokeWidth={2} />
    </div>
  )
}

/* A single settings row: label (+ optional inline icon) and subtitle on the
   left, the control on the right. Hairline divider below unless it's the last. */
export function SettingRow({
  label,
  subtitle,
  icon,
  isLast,
  children,
}: {
  label: string
  subtitle?: string
  icon?: React.ReactNode
  isLast?: boolean
  children: React.ReactNode
}) {
  return (
    <div className={cn("flex items-center justify-between gap-6 py-5", !isLast && "border-b border-white/[0.06]")}>
      <div className="min-w-0">
        <div className="flex items-center gap-2">
          <span className="text-[15px] font-semibold text-white">{label}</span>
          {icon}
        </div>
        {subtitle && <p className="mt-1 truncate text-[13px] text-[#8E8E93]">{subtitle}</p>}
      </div>
      <div className="shrink-0">{children}</div>
    </div>
  )
}

/* The rounded card that holds settings rows. */
export function SettingsCard({ children, className }: { children: React.ReactNode; className?: string }) {
  return (
    <div className={cn("rounded-2xl border border-white/[0.06] bg-[#17171A] px-8", className)}>{children}</div>
  )
}

/* Section heading: thin full-width divider, then a small uppercase label. */
export function SectionHeader({ children }: { children: React.ReactNode }) {
  return (
    <div className="mb-5">
      <div className="h-px w-full bg-white/[0.06]" />
      <h2 className="mt-4 text-[11px] font-semibold uppercase tracking-[0.14em] text-[#8E8E93]">{children}</h2>
    </div>
  )
}

/* Centered empty/placeholder state inside a card. */
export function EmptyState({
  icon,
  title,
  children,
}: {
  icon: React.ReactNode
  title: string
  children?: React.ReactNode
}) {
  return (
    <div className="flex flex-col items-center justify-center rounded-2xl border border-white/[0.06] bg-[#17171A] px-8 py-16 text-center">
      <div className="mb-3 text-[#6B6B70]">{icon}</div>
      <div className="text-sm font-semibold text-white">{title}</div>
      {children && <p className="mt-1.5 max-w-sm text-[13px] text-[#8E8E93]">{children}</p>}
    </div>
  )
}

export function StatusBadge({ connected }: { connected: boolean }) {
  return (
    <span
      className={cn(
        "rounded-full px-2 py-0.5 text-[10px] font-medium",
        connected ? "bg-emerald-500/15 text-emerald-400" : "bg-white/[0.06] text-[#8E8E93]",
      )}
    >
      {connected ? "Connected" : "Not connected"}
    </span>
  )
}

/* Provider key card — restyled to the spec card aesthetic. */
export function KeyCard({
  title,
  hint,
  connected,
  pending,
  keyValue,
  onKeyChange,
  onConnect,
  onDisconnect,
}: {
  title: string
  hint: string
  connected: boolean
  pending: boolean
  keyValue: string
  onKeyChange: (v: string) => void
  onConnect: () => void
  onDisconnect: () => void
}) {
  return (
    <div className="rounded-2xl border border-white/[0.06] bg-[#17171A] p-4">
      <div className="flex items-center justify-between">
        <div>
          <div className="text-xs font-semibold text-white">{title}</div>
          <div className="text-[10px] text-[#8E8E93]">{hint}</div>
        </div>
        <StatusBadge connected={connected} />
      </div>
      {connected ? (
        <button
          onClick={onDisconnect}
          disabled={pending}
          className="mt-3 w-full rounded-lg border border-white/10 bg-[#1c1c1f] px-3 py-2 text-xs text-white transition-colors hover:bg-white/[0.06] disabled:opacity-50"
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
            className="min-w-0 flex-1 rounded-lg border border-white/10 bg-[#1c1c1f] px-3 py-2 text-xs text-white outline-none focus-visible:border-[#3B82F6]"
          />
          <button
            onClick={onConnect}
            disabled={pending || keyValue.trim() === ""}
            className="shrink-0 rounded-lg bg-[#3B82F6] px-3 py-2 text-xs font-medium text-white transition-colors hover:bg-[#3B82F6]/90 disabled:opacity-50"
          >
            {pending ? "Connecting…" : "Connect"}
          </button>
        </div>
      )}
    </div>
  )
}