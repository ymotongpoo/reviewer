import { projectId } from './api'

// Annotations the reviewer has already looked at, per project and browser.
// New annotations open by default; seen ones start collapsed.
const KEY = `reviewer.seenAnnotations.${projectId ?? 'none'}`
const MAX = 2000

function load(): string[] {
  try {
    const v = JSON.parse(localStorage.getItem(KEY) ?? '[]')
    return Array.isArray(v) ? v : []
  } catch {
    return []
  }
}

const seen = new Set<string>(load())

export function isSeen(id: string): boolean {
  return seen.has(id)
}

export function markSeen(ids: string[]) {
  let changed = false
  for (const id of ids) {
    if (!seen.has(id)) {
      seen.add(id)
      changed = true
    }
  }
  if (!changed) return
  try {
    localStorage.setItem(KEY, JSON.stringify([...seen].slice(-MAX)))
  } catch {
    // storage unavailable
  }
}
