/// <reference types="node" />
import { afterEach, describe, expect, it, vi } from 'vitest'
import css from './responsive.css?raw'
import { COMPACT, MEDIUM, NARROW, TOUCH } from './media'

// Vitest disables CSS imports (including ?raw); load the actual file for the guard.
vi.mock('./responsive.css?raw', async () => {
  const { readFileSync } = await import('node:fs')
  return { default: readFileSync(new URL('./responsive.css', import.meta.url), 'utf8') }
})

afterEach(() => { vi.unstubAllGlobals(); vi.resetModules() })

describe('responsive CSS guard', () => {
  const source = css.replace(/\/\*[\s\S]*?\*\//g, '')

  it('uses only shared media conditions or prefers-* conditions, including comma lists', () => {
    const conditions = [...source.matchAll(/@media\s+([^{}]+)\{/g)]
    expect(conditions.length).toBeGreaterThan(0)
    for (const [, list] of conditions) {
      for (const condition of list.split(',').map((s) => s.trim())) {
        expect(
          [COMPACT, MEDIUM, NARROW, TOUCH].includes(condition) || /^\(prefers-[\w-]+:\s*[\w-]+\)$/.test(condition),
          condition,
        ).toBe(true)
      }
    }
    expect(source).not.toMatch(/any-(pointer|hover)/)
  })

  it('keeps every C3 foundation rule inside a media block', () => {
    const outside = source.replace(/@media\s+[^{}]+\{(?:[^{}]|\{[^{}]*\})*\}/g, '')
    expect(outside.trim()).toBe('')
  })
})

describe('media signals', () => {
  it('starts with matchMedia values and updates layout and touch independently', async () => {
    const queries = new Map<string, EventTarget & { matches: boolean }>()
    const matchMedia = vi.fn((query: string) => {
      const mq = Object.assign(new EventTarget(), { matches: query !== TOUCH })
      queries.set(query, mq)
      return mq
    })
    vi.stubGlobal('window', { matchMedia })
    vi.resetModules()
    const { isCompact, isNarrow, isTouchUI } = await import('./media')
    const values = () => [isCompact.value, isNarrow.value, isTouchUI.value]
    const change = (query: string, matches: boolean) => {
      const mq = queries.get(query)!
      mq.matches = matches
      mq.dispatchEvent(Object.assign(new Event('change'), { matches }))
    }
    expect(matchMedia.mock.calls.map(([query]) => query)).toEqual([COMPACT, NARROW, TOUCH])
    expect(values()).toEqual([true, true, false])
    change(COMPACT, false)
    expect(values()).toEqual([false, true, false])
    change(NARROW, false)
    change(TOUCH, true)
    expect(values()).toEqual([false, false, true])
    change(TOUCH, false)
    expect(values()).toEqual([false, false, false])
  })

  it('can be imported without a browser', async () => {
    vi.resetModules()
    const { isCompact, isNarrow, isTouchUI } = await import('./media')
    expect([isCompact.value, isNarrow.value, isTouchUI.value]).toEqual([false, false, false])
  })
})
