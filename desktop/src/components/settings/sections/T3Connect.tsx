import { LogIn } from "lucide-react"
import { EmptyState } from "../controls"

export function T3Connect() {
  return (
    <div className="space-y-4">
      <EmptyState icon={<LogIn className="h-8 w-8" strokeWidth={1.5} />} title="Cloud sync isn't available yet">
        Sign-in and cloud sync aren't part of Hollow today. This is a placeholder for a future
        connected-account feature.
      </EmptyState>
    </div>
  )
}