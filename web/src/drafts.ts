import { ApiError, NetworkError } from './api'
import type { EditTarget } from './state'
import type { Comment, TextRange } from './types'

export type NewTarget = Extract<EditTarget, { kind: 'new' }>
type Target = { commentId?: string; path?: string; scope: string; start?: number; end?: number; range?: TextRange }

export function composerKey(projectId: string, t: Target): string {
  if (t.commentId) return `${projectId}:comment:${t.commentId}`
  const r = t.range
  return `${projectId}:new:${t.scope}:${encodeURIComponent(t.path ?? '')}:${t.start ?? ''}-${t.end ?? ''}:${r ? `${r.startLine},${r.startColumn}-${r.endLine},${r.endColumn}` : ''}`
}

export type SaveOutcome = 'saved' | 'failed' | 'unknown'
export interface ComposerJournal {
  key: string
  target?: NewTarget
  commentId?: string
  label: string
  body: string
  rev: number
  savedAt: number
  outcome?: Exclude<SaveOutcome, 'saved'>
  /** The POST body may differ from newer input when its response is lost. */
  attemptedBody?: string
}

export function shouldDropJournal(j: ComposerJournal | undefined, savedRev: number): boolean {
  return j !== undefined && j.rev === savedRev
}

export function classifySaveError(e: unknown): Exclude<SaveOutcome, 'saved'> {
  if (e instanceof NetworkError || e instanceof ApiError && e.status >= 500) return 'unknown'
  if (e instanceof ApiError && e.status >= 400 && e.status < 500) return 'failed'
  // A response body can fail to decode after the server has already committed.
  return 'unknown'
}

export function matchesDraft(c: Comment, t: NewTarget, body: string): boolean {
  return c.status === 'draft' && c.scope === t.scope && c.path === t.path && c.body === body &&
    (t.scope !== 'line' || (c.origStart === t.start && c.origEnd === t.end)) &&
    composerKey('', { ...t, range: c.range }) === composerKey('', t)
}

export interface EditDraft {
  path: string
  baseHash: string
  baseContent: string
  lines: string[]
  savedAt: number
  revision: number
}

export function restorePlan(draft: Pick<EditDraft, 'baseHash'>, file: { hash: string }): 'same-base' | 'conflict' {
  return draft.baseHash === file.hash ? 'same-base' : 'conflict'
}

export function afterSave(sentRev: number, currentRev: number): 'clear' | 'rebase' {
  return sentRev === currentRev ? 'clear' : 'rebase'
}

export function shouldDropEditDraft(baseContent: string, content: string): boolean {
  return content === baseContent
}
