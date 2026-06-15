type ClassValue =
  | string
  | number
  | boolean
  | null
  | undefined
  | ClassValue[]
  | Record<string, boolean | null | undefined>

export function cn(...inputs: ClassValue[]) {
  const result: string[] = []

  const push = (value: ClassValue) => {
    if (!value) return
    if (typeof value === 'string' || typeof value === 'number') {
      result.push(String(value))
      return
    }
    if (Array.isArray(value)) {
      value.forEach(push)
      return
    }
    if (typeof value === 'object') {
      Object.entries(value).forEach(([key, enabled]) => {
        if (enabled) result.push(key)
      })
    }
  }

  inputs.forEach(push)
  return result.join(' ')
}
