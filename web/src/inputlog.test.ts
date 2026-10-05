import { describe, expect, it, vi } from 'vitest'
import { createInputLog, formatEntry, initInputLog, inputLogEnabled } from './inputlog'

describe('input log', () => {
  it('retains the newest 3000 entries in order and clears the ring', () => {
    const log = createInputLog()
    for (let t = 0; t < 6501; t++) log.push({ t, type: 'input' })
    expect(log.size).toBe(3000)
    expect(log.entries().map((e) => e.t)).toEqual(Array.from({ length: 3000 }, (_, i) => i + 3501))
    log.clear()
    expect(log.entries()).toEqual([])
    log.push({ t: 7000, type: 'input' })
    expect(log.entries()).toEqual([formatEntry({ t: 7000, type: 'input' })])
  })

  it('limits data to 20 code points and excludes unlisted content', () => {
    const source = { t: 1, type: 'input', data: '🍣'.repeat(21), value: 'private body', clipboardData: 'private paste', valueLen: 12 }
    const entry = formatEntry(source)
    expect(entry.data).toBe('🍣'.repeat(20))
    expect(entry.valueLen).toBe(12)
    expect(entry).not.toHaveProperty('value')
    expect(entry).not.toHaveProperty('clipboardData')
    expect(formatEntry({ t: 2, type: 'input', data: null }).data).toBeNull()
  })

  it('does not invoke registration when disabled, including explicit opt-out', () => {
    const start = vi.fn()
    const storage = () => ({ getItem: () => '0', setItem: vi.fn() })
    initInputLog({ search: '', storage, start })
    initInputLog({ search: '?inputlog=0', storage: () => ({ getItem: () => '1', setItem: vi.fn() }), start })
    expect(start).not.toHaveBeenCalled()
    initInputLog({ search: '?inputlog=1', storage, start })
    expect(start).toHaveBeenCalledOnce()
  })

  it('persists explicit flags and tolerates unavailable storage', () => {
    const values = new Map<string, string>()
    const storage = () => ({ getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => { values.set(key, value) } })
    expect(inputLogEnabled('?inputlog=1', storage)).toBe(true)
    expect(inputLogEnabled('', storage)).toBe(true)
    expect(inputLogEnabled('?inputlog=0', storage)).toBe(false)
    expect(inputLogEnabled('', storage)).toBe(false)
    const unavailable = () => { throw new Error('denied') }
    expect(inputLogEnabled('', unavailable)).toBe(false)
    expect(inputLogEnabled('?inputlog=1', unavailable)).toBe(true)
    expect(inputLogEnabled('?inputlog=0', unavailable)).toBe(false)
  })
})
