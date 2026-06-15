/** Capitalize the first letter of assistant prose (skip code, lists, headings). */
export function capitalizeProseStart(text: string): string {
  const trimmed = text.trimStart()
  if (!trimmed) return text

  const skip =
    trimmed.startsWith("```") ||
    trimmed.startsWith("#") ||
    trimmed.startsWith("-") ||
    trimmed.startsWith("*") ||
    trimmed.startsWith(">") ||
    trimmed.startsWith("[") ||
    /^\d/.test(trimmed)

  if (skip) return text

  return text.replace(/^(\s*)([a-z])/, (_, ws, c) => ws + c.toUpperCase())
}
