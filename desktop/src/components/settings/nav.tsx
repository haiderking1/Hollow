import {
  Archive,
  ArrowLeft,
  Cpu,
  GitBranch,
  Keyboard,
  Link2,
  SlidersHorizontal,
  type LucideIcon,
} from "lucide-react"
import { cn } from "../../lib/utils"

export type SectionId =
  | "general"
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
    <nav className="flex w-60 shrink-0 flex-col bg-[#0E0E10] py-4">
      <div className="flex flex-col gap-0.5 px-3">
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
                  ? "font-medium text-white"
                  : "font-normal text-[#8E8E93] hover:bg-white/[0.04] hover:text-white",
              )}
            >
              <Icon
                className="h-[18px] w-[18px]"
                strokeWidth={1.75}
                style={{ color: isActive ? "#FFFFFF" : "#6B6B70" }}
              />
              {item.label}
            </button>
          )
        })}
      </div>

      {/* Bottom action, separated by space */}
      <div className="mt-auto flex flex-col gap-0.5 px-3">
        <button
          onClick={onBack}
          className="flex items-center gap-2.5 rounded-lg px-3 py-2 text-[13px] text-[#8E8E93] transition-colors hover:bg-white/[0.04] hover:text-white"
        >
          <ArrowLeft className="h-[18px] w-[18px]" strokeWidth={1.75} style={{ color: "#6B6B70" }} />
          Back
        </button>
      </div>
    </nav>
  )
}