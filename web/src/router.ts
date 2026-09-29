import { signal } from '@preact/signals'

export type Route = { page: 'overview' } | { page: 'file'; path: string; line?: number }

function parse(): Route {
  const h = decodeURIComponent(location.hash.replace(/^#/, ''))
  const m = h.match(/^\/file\/(.+?)(?::L(\d+))?$/)
  if (m) return { page: 'file', path: m[1], line: m[2] ? Number(m[2]) : undefined }
  return { page: 'overview' }
}

export const route = signal<Route>(parse())
window.addEventListener('hashchange', () => (route.value = parse()))

export function fileHref(path: string, line?: number): string {
  return `#/file/${path.split('/').map(encodeURIComponent).join('/')}${line ? `:L${line}` : ''}`
}
