import { describe, expect, it, vi } from 'vitest'
import { createDraftStore, memoryDraftStore } from './localstore'
import type { Comment, TextRange } from './types'

vi.stubGlobal('location', { pathname: '/p/abc/' })
const { ApiError, NetworkError } = await import('./api')
const { composerKey, shouldDropJournal, classifySaveError, matchesDraft } = await import('./drafts')
const range: TextRange = { startLine: 3, startColumn: 1, endLine: 4, endColumn: 2, text: '選択' }
const target = { kind: 'new' as const, scope: 'line' as const, path: 'guide.md', start: 3, end: 4 }
const journal = { key: 'abc:new:line:guide.md:3-4:', target, body: '本文', label: 'must', rev: 2, savedAt: 1 }

describe('composer journals', () => {
  it('distinguishes projects, scopes, paths, lines, ranges and existing comments', () => {
    expect(composerKey('abc', target)).toBe(journal.key)
    expect(composerKey('abc', { ...target, commentId: 'C-1' })).toBe('abc:comment:C-1')
    expect(composerKey('abc', { ...target, range })).toBe('abc:new:line:guide.md:3-4:3,1-4,2')
    for (const other of [{ ...target, scope: 'file' }, { ...target, path: 'a:b' }, { ...target, end: 5 }, { ...target, range }]) {
      expect(composerKey('abc', other)).not.toBe(journal.key)
    }
    expect(composerKey('abcd', target)).not.toBe(journal.key)
    expect(composerKey('abc', { scope: 'project' })).toBe('abc:new:project::-:')
  })
  it('only deletes the exact saved revision', () => {
    expect(shouldDropJournal(journal, 2)).toBe(true)
    expect(shouldDropJournal(journal, 1)).toBe(false)
    expect(shouldDropJournal(journal, 3)).toBe(false)
    expect(shouldDropJournal(undefined, 2)).toBe(false)
  })
  it('classifies network and server failures conservatively, including undecodable responses', () => {
    for (const error of [new NetworkError('offline'), new ApiError('server', 500), new ApiError('gateway', 502), new SyntaxError('json')]) {
      expect(classifySaveError(error)).toBe('unknown')
    }
    for (const status of [400, 401, 403, 404, 409, 422]) expect(classifySaveError(new ApiError('rejected', status))).toBe('failed')
  })
  it('reconciles only draft comments at the exact target, preserving character ranges', () => {
    const c = { ...target, origStart: 3, origEnd: 4, body: '本文', status: 'draft' } as unknown as Comment
    expect(matchesDraft(c, target, '本文')).toBe(true)
    for (const patch of [{ status: 'open' }, { body: '別の本文' }, { path: 'other.md' }, { origEnd: 5 }, { range }]) {
      expect(matchesDraft({ ...c, ...patch } as Comment, target, '本文')).toBe(false)
    }
    expect(matchesDraft({ ...c, range }, { ...target, range }, '本文')).toBe(true)
    expect(matchesDraft({ ...c, range: { ...range, startColumn: 2 } }, { ...target, range }, '本文')).toBe(false)
  })
})

describe('local store', () => {
  it('isolates stores and projects and snapshots values on write/read', async () => {
    const store = memoryDraftStore()
    await store.put('composer', journal.key, journal)
    await store.put('edit', journal.key, { body: 'edit' })
    await store.put('composer', 'abcd:new', journal)
    const read = await store.get<typeof journal>('composer', journal.key)
    read!.body = 'changed'
    expect(await store.list('composer', 'abc:')).toEqual([journal])
    await store.del('composer', journal.key)
    expect(await store.get('composer', journal.key)).toBeUndefined()
    expect(await store.get('edit', journal.key)).toEqual({ body: 'edit' })
  })
  it('falls back on open errors, warns once and preserves operation order', async () => {
    vi.stubGlobal('indexedDB', { open: () => { throw new Error('denied') } })
    const warn = vi.fn()
    const store = createDraftStore(warn)
    await Promise.all([store.put('composer', journal.key, journal), store.del('composer', journal.key)])
    expect(await store.list('composer', 'abc:')).toEqual([])
    await store.put('composer', journal.key, journal)
    expect(await store.get('composer', journal.key)).toEqual(journal)
    expect(store.persistent).toBe(false)
    expect(warn).toHaveBeenCalledTimes(1)
  })
})
