import { useState } from "react"
import { CornerDownLeft, Square } from "lucide-react"

interface PromptInputProps {
  onSend?: (text: string) => void
  isStreaming?: boolean
  onAbort?: () => void
}

export function PromptInput({ onSend, isStreaming, onAbort }: PromptInputProps) {
  const [value, setValue] = useState("")

  const submit = () => {
    const text = value.trim()
    if (!text || isStreaming) return
    onSend?.(text)
    setValue("")
  }

  return (
    <div className="rounded-2xl border border-border-strong bg-surface px-4 py-3.5 transition-colors focus-within:border-muted-foreground/40">
      <div className="flex items-center gap-3">
        <textarea
          value={value}
          onChange={(e) => setValue(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey) {
              e.preventDefault()
              submit()
            }
          }}
          rows={1}
          placeholder="Type / for commands"
          className="block max-h-40 w-full resize-none bg-transparent text-[15px] leading-relaxed text-foreground placeholder:text-muted-foreground focus:outline-none"
        />
        {isStreaming ? (
          <button
            onClick={onAbort}
            aria-label="Stop"
            className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-surface-hover hover:text-foreground"
          >
            <Square className="h-[15px] w-[15px] fill-current" strokeWidth={1.75} />
          </button>
        ) : (
          <button
            onClick={submit}
            aria-label="Send"
            className="flex shrink-0 items-center justify-center text-muted-foreground transition-colors hover:text-foreground"
          >
            <CornerDownLeft className="h-[18px] w-[18px]" strokeWidth={1.75} />
          </button>
        )}
      </div>
    </div>
  )
}
