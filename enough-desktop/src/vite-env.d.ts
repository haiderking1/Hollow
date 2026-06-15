/// <reference types="vite/client" />

interface DirListing {
  path: string
  parent: string | null
  entries: { name: string; path: string }[]
  home: string
  error?: string
}

interface FlameBridge {
  isElectron: true
  setZoom: (factor: number) => void
  pickDirectory: () => Promise<string | null>
  listDir: (path?: string) => Promise<DirListing>
  agent: {
    send: (command: Record<string, unknown>) => void
    setCwd: (cwd: string) => void
    onEvent: (callback: (event: any) => void) => () => void
  }
}

interface Window {
  flame?: FlameBridge
}
