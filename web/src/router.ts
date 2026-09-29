import { signal } from '@preact/signals'

export type Route =
  | { page: 'overview' }
  | { page: 'file'; path: string; line?: number }
  | { page: 'round'; round: number; path?: string }

function parse(): Route {
  const h = decodeURIComponent(location.hash.replace(/^#/, ''))
  const m = h.match(/^\/file\/(.+?)(?::L(\d+))?$/)
  if (m) return { page: 'file', path: m[1], line: m[2] ? Number(m[2]) : undefined }
  const r = h.match(/^\/round\/(\d+)(?:\/(.+))?$/)
  if (r) return { page: 'round', round: Number(r[1]), path: r[2] }
  return { page: 'overview' }
}

export const route = signal<Route>(parse())
window.addEventListener('hashchange', () => (route.value = parse()))

export function fileHref(path: string, line?: number): string {
  return `#/file/${path.split('/').map(encodeURIComponent).join('/')}${line ? `:L${line}` : ''}`
}

export function roundHref(round: number, path?: string): string {
  return `#/round/${round}${path ? '/' + path.split('/').map(encodeURIComponent).join('/') : ''}`
}
