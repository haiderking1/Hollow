import { GitBranch } from "lucide-react"
import { EmptyState } from "../controls"

export function SourceControl() {
  return (
    <EmptyState icon={<GitBranch className="h-8 w-8" strokeWidth={1.5} />} title="Source control preferences">
      Git integration settings will appear here. For now, Hollow works directly in your project's
      working tree.
    </EmptyState>
  )
}