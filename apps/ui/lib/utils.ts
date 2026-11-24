export { cn } from "cn"

// localStorage throws in some private modes and is absent during prerender.
export function readStorage(key: string): string | null {
  try {
    return window.localStorage.getItem(key)
  } catch {
    return null
  }
}

export function writeStorage(key: string, value: string): void {
  try {
    window.localStorage.setItem(key, value)
  } catch {
    // ignore: the choice just won't persist
  }
}
