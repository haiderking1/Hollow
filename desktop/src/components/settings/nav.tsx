import {
  Archive,
  ArrowLeft,
  Cpu,
  GitBranch,
  Keyboard,
  Link2,
  Palette,
  SlidersHorizontal,
  type LucideIcon,
} from "lucide-react"
import { cn } from "../../lib/utils"
import { TrafficLights } from "../TrafficLights"

export type SectionId =
  | "general"
  | "appearance"
  | "keybindings"
  | "providers"
  | "sourceControl"
  | "connections"
  | "archive"

interface NavItem {
  id: SectionId
  label: string
  icon: LucideIcon
}

const NAV_ITEMS: NavItem[] = [
  { id: "general", label: "General", icon: SlidersHorizontal },
  { id: "appearance", label: "Appearance", icon: Palette },
  { id: "keybindings", label: "Keybindings", icon: Keyboard },
  { id: "providers", label: "Providers", icon: Cpu },
  { id: "sourceControl", label: "Source Control", icon: GitBranch },
  { id: "connections", label: "Connections", icon: Link2 },
  { id: "archive", label: "Archive", icon: Archive },
]

export function SettingsNav({
  active,
  onNavigate,
  onBack,
}: {
  active: SectionId
  onNavigate: (id: SectionId) => void
  onBack: () => void
}) {
  return (
    <nav className="flex w-60 shrink-0 flex-col bg-background">
      {/* Top-left window controls — same spot as the main window's dots. */}
      <header className="app-drag flex h-11 shrink-0 items-center px-4">
        <TrafficLights />
      </header>

      <div className="flex flex-col gap-0.5 px-3 py-2">
        {NAV_ITEMS.map((item) => {
          const Icon = item.icon
          const isActive = item.id === active
          return (
            <button
              key={item.id}
              onClick={() => onNavigate(item.id)}
              className={cn(
                "flex items-center gap-2.5 rounded-lg px-3 py-2 text-[13px] transition-colors",
                isActive
                  ? "font-medium text-foreground"
                  : "font-normal text-muted-foreground hover:bg-surface-hover hover:text-foreground",
              )}
            >
              <Icon
                className={cn("h-[18px] w-[18px]", isActive ? "text-foreground" : "text-icon-inactive")}
                strokeWidth={1.75}
              />
              {item.label}
            </button>
          )
        })}
      </div>

      {/* Bottom action, separated by space */}
      <div className="mt-auto flex flex-col gap-0.5 px-3 pb-4">
        <button
          onClick={onBack}
          className="flex items-center gap-2.5 rounded-lg px-3 py-2 text-[13px] text-muted-foreground transition-colors hover:bg-surface-hover hover:text-foreground"
        >
          <ArrowLeft className="h-[18px] w-[18px] text-icon-inactive" strokeWidth={1.75} />
          Back
        </button>
      </div>
    </nav>
  )
}