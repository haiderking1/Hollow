import { CheckCircle2, Circle } from "lucide-react"
import type { ConnectionInfo } from "../../../agent/rpc"
import { SettingsCard } from "../controls"

const ORDER = ["opencode-go", "opencode-zen", "neuralwatt", "openai-codex"]
const LABELS: Record<string, string> = {
  "opencode-go": "OpenCode Go",
  "opencode-zen": "OpenCode Zen",
  neuralwatt: "NeuralWatt",
  "openai-codex": "OpenAI Codex",
}

export function Connections({ connections }: { connections: ConnectionInfo[] }) {
  const rows = ORDER.map((id) => connections.find((c) => c.provider === id)).filter(Boolean) as ConnectionInfo[]
  const list = rows.length ? rows : ORDER.map((id) => ({ provider: id, displayName: LABELS[id], kind: "key", connected: false }))

  return (
    <SettingsCard>
      {list.map((c, i) => (
        <div
          key={c.provider}
          className={`flex items-center justify-between gap-6 py-4 ${i < list.length - 1 ? "border-b border-white/[0.06]" : ""}`}
        >
          <div>
            <div className="text-[14px] font-semibold text-white">{c.displayName || LABELS[c.provider] || c.provider}</div>
            <div className="mt-0.5 text-[12px] text-[#8E8E93]">{c.kind === "oauth" ? "OAuth sign-in" : "API key"}</div>
          </div>
          {c.connected ? (
            <span className="flex items-center gap-1.5 text-[12px] text-emerald-400">
              <CheckCircle2 className="h-4 w-4" strokeWidth={2} />
              Connected
            </span>
          ) : (
            <span className="flex items-center gap-1.5 text-[12px] text-[#8E8E93]">
              <Circle className="h-4 w-4" strokeWidth={2} />
              Not connected
            </span>
          )}
        </div>
      ))}
    </SettingsCard>
  )
}