import type { SelectionTarget } from './textrange'

export interface SelToken { path: string; hash: string; gen: number }
export type TouchSel =
  | { kind: 'none' }
  | { kind: 'lines'; anchor: number; focus?: number; token: SelToken }
  | { kind: 'text'; target: SelectionTarget; token: SelToken }

export function isValid(s: TouchSel, token: SelToken): boolean {
  return s.kind === 'none' || (s.token.path === token.path && s.token.hash === token.hash && s.token.gen === token.gen)
}

export function tapLine(s: TouchSel, no: number, token: SelToken): TouchSel {
  if (s.kind === 'lines' && s.focus === undefined && isValid(s, token)) return { ...s, focus: no }
  return { kind: 'lines', anchor: no, token }
}

export function lineRange(s: TouchSel): [number, number] | null {
  if (s.kind !== 'lines') return null
  const end = s.focus ?? s.anchor
  return [Math.min(s.anchor, end), Math.max(s.anchor, end)]
}
