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
        checked ? "bg-accent" : "bg-toggle-off",
      )}
    >
      <span
        className={cn(
          "absolute top-1/2 h-[18px] w-[18px] -translate-y-1/2 rounded-full transition-all duration-150",
          checked ? "left-[23px] bg-foreground" : "left-[3px] bg-icon-inactive",
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
        className="h-9 w-full cursor-pointer appearance-none rounded-[10px] border border-border-strong bg-surface-hover pl-3 pr-8 text-sm text-foreground outline-none transition-colors hover:border-border-strong focus-visible:border-accent"
      >
        {options.map((o) => (
          <option key={o.value} value={o.value} className="bg-surface text-foreground">
            {o.label}
          </option>
        ))}
      </select>
      <ChevronDown className="pointer-events-none absolute right-2.5 h-4 w-4 text-muted-foreground" strokeWidth={2} />
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
    <div className={cn("flex items-center justify-between gap-6 py-5", !isLast && "border-b border-border")}>
      <div className="min-w-0">
        <div className="flex items-center gap-2">
          <span className="text-[15px] font-semibold text-foreground">{label}</span>
          {icon}
        </div>
        {subtitle && <p className="mt-1 truncate text-[13px] text-muted-foreground">{subtitle}</p>}
      </div>
      <div className="shrink-0">{children}</div>
    </div>
  )
}

/* The rounded card that holds settings rows. */
export function SettingsCard({ children, className }: { children: React.ReactNode; className?: string }) {
  return (
    <div className={cn("rounded-2xl border border-border bg-surface px-8", className)}>{children}</div>
  )
}

/* Section heading: small uppercase label. */
export function SectionHeader({ children }: { children: React.ReactNode }) {
  return (
    <div className="mb-5">
      <h2 className="text-[11px] font-semibold uppercase tracking-[0.14em] text-muted-foreground">{children}</h2>
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
    <div className="flex flex-col items-center justify-center rounded-2xl border border-border bg-surface px-8 py-16 text-center">
      <div className="mb-3 text-icon-inactive">{icon}</div>
      <div className="text-sm font-semibold text-foreground">{title}</div>
      {children && <p className="mt-1.5 max-w-sm text-[13px] text-muted-foreground">{children}</p>}
    </div>
  )
}