import { signal } from '@preact/signals'

export type Route =
  | { page: 'overview' }
  | { page: 'file'; path: string; line?: number }
  | { page: 'round'; round: number; path?: string; phase?: 'review' | 'agent' }
  | { page: 'settings' }

function parse(): Route {
  const h = decodeURIComponent(location.hash.replace(/^#/, ''))
  const m = h.match(/^\/file\/(.+?)(?::L(\d+))?$/)
  if (m) return { page: 'file', path: m[1], line: m[2] ? Number(m[2]) : undefined }
  const r = h.match(/^\/round\/(\d+)(?:\/(.+?))?(?:\?view=(review|agent))?$/)
  if (r) return { page: 'round', round: Number(r[1]), path: r[2], phase: r[3] as 'review' | 'agent' | undefined }
  if (h === '/settings') return { page: 'settings' }
  return { page: 'overview' }
}

export const route = signal<Route>(parse())

/** Returns false to keep the current page, e.g. when unsaved edits would be lost. */
export type NavigationGuard = (next: Route) => boolean
const guards = new Set<NavigationGuard>()

/** Registers a guard consulted before the route changes; returns a function that removes it. */
export function addNavigationGuard(g: NavigationGuard): () => void {
  guards.add(g)
  return () => guards.delete(g)
}

let currentHash = location.hash
window.addEventListener('hashchange', () => {
  const next = parse()
  for (const g of guards) {
    if (!g(next)) {
      // The URL already changed; put it back without another hashchange.
      history.replaceState(history.state, '', currentHash || location.pathname + location.search)
      return
    }
  }
  currentHash = location.hash
  route.value = next
})

export function fileHref(path: string, line?: number): string {
  return `#/file/${path.split('/').map(encodeURIComponent).join('/')}${line ? `:L${line}` : ''}`
}

export function roundHref(round: number, path?: string, phase: 'review' | 'agent' = 'agent'): string {
  const base = `#/round/${round}${path ? '/' + path.split('/').map(encodeURIComponent).join('/') : ''}`
  return `${base}?view=${phase}`
}
