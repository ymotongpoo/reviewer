import { formatRange } from './textrange'
import type { AnchorState, Comment, Status } from './types'

export const statusText: Record<Status, string> = {
  draft: '下書き',
  open: '未対応',
  addressed: '対応済み',
  wontfix: '対応しない',
  question: '質問あり',
  resolved: '解決済み',
}

export const anchorText: Record<AnchorState, string> = {
  exact: '',
  moved: '移動',
  fuzzy: '位置推定',
  outdated: '位置不明',
}

export const anchorHelp: Record<AnchorState, string> = {
  exact: '',
  moved: 'コメントした文章が別の行に移動しました',
  fuzzy: 'コメントした文章が書き換えられたため、近い内容の行を推定しています',
  outdated: 'コメントした文章が見つかりません',
}

export function lineRange(start?: number, end?: number): string {
  if (!start) return ''
  return start === end || !end ? `L${start}` : `L${start}-${end}`
}

/** Position of a line comment; that of a range comment includes its columns. */
export function commentLines(c: Comment): string {
  const r = c.range && (c.loc?.range ?? c.range)
  return r ? formatRange(r) : lineRange(c.loc?.start, c.loc?.end)
}
