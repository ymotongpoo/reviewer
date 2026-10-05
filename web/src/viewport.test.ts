import type { Signal } from '@preact/signals'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { isNarrow, isTouchUI } from './media'
import { ensureVisible, initViewport, kbInset } from './viewport'

vi.mock('./media', async () => {
  const { signal } = await import('@preact/signals')
  return { isNarrow: signal(false), isTouchUI: signal(false) }
})

const narrow = isNarrow as Signal<boolean>
const touch = isTouchUI as Signal<boolean>
let stop: (() => void) | undefined

beforeEach(() => { narrow.value = false; touch.value = false })
afterEach(() => { stop?.(); stop = undefined; vi.unstubAllGlobals() })

function browser() {
  const vv = Object.assign(new EventTarget(), { height: 500, offsetTop: 40 })
  const win = Object.assign(new EventTarget(), { innerHeight: 800, visualViewport: vv as typeof vv | null })
  const properties = new Map<string, string>()
  const style = {
    setProperty: vi.fn((key: string, value: string) => { properties.set(key, value) }),
    removeProperty: vi.fn((key: string) => { properties.delete(key) }),
  }
  vi.stubGlobal('window', win)
  vi.stubGlobal('document', { documentElement: { style } })
  return { vv, win, properties, style }
}

describe('keyboard inset', () => {
  it.each([
    [800, 500, 0, 300], [800, 500, 40, 260], [800, 800, 0, 0],
    [800, 900, 0, 0], [800, 700, 150, 0], [800, 500.5, 20.25, 279.25],
  ])('kbInset(%s, %s, %s) = %s', (inner, height, top, expected) => {
    expect(kbInset(inner, height, top)).toBe(expected)
  })
})

describe('viewport lifecycle', () => {
  it('leaves expanded fine untouched and responds to width and pointer changes', () => {
    const { vv, win, properties, style } = browser()
    stop = initViewport()
    vv.dispatchEvent(new Event('resize'))
    win.dispatchEvent(new Event('resize'))
    expect(style.setProperty).not.toHaveBeenCalled()
    narrow.value = true
    expect(properties.get('--kb-inset')).toBe('260px')
    expect(properties.get('--vvh')).toBe('500px')
    narrow.value = false
    expect(properties.size).toBe(0)
    touch.value = true
    expect(properties.get('--kb-inset')).toBe('260px')
    touch.value = false
    expect(properties.size).toBe(0)
    style.setProperty.mockClear()
    vv.dispatchEvent(new Event('scroll'))
    expect(style.setProperty).not.toHaveBeenCalled()
  })

  it('tracks viewport resize, scroll and window resize, and removes listeners on cleanup', () => {
    const { vv, win, properties, style } = browser()
    narrow.value = true
    stop = initViewport()
    vv.height = 400
    vv.dispatchEvent(new Event('resize'))
    expect(properties.get('--kb-inset')).toBe('360px')
    expect(properties.get('--vvh')).toBe('400px')
    vv.offsetTop = 100
    vv.dispatchEvent(new Event('scroll'))
    expect(properties.get('--kb-inset')).toBe('300px')
    win.innerHeight = 450
    win.dispatchEvent(new Event('resize'))
    expect(properties.get('--kb-inset')).toBe('0px')
    stop()
    expect(properties.size).toBe(0)
    style.setProperty.mockClear()
    vv.dispatchEvent(new Event('resize'))
    vv.dispatchEvent(new Event('scroll'))
    win.dispatchEvent(new Event('resize'))
    touch.value = true
    expect(style.setProperty).not.toHaveBeenCalled()
  })

  it('does not write viewport variables when visualViewport is unavailable', () => {
    const { win, style } = browser()
    win.visualViewport = null
    narrow.value = true
    stop = initViewport()
    win.dispatchEvent(new Event('resize'))
    expect(style.setProperty).not.toHaveBeenCalled()
  })
})

describe('ensureVisible', () => {
  it('scrolls only the supplied container by the overlap plus 8px', () => {
    browser()
    const scroller = { scrollTop: 30 } as HTMLElement
    const el = { getBoundingClientRect: () => ({ bottom: 600 }) } as HTMLElement
    ensureVisible(el, scroller)
    expect(scroller.scrollTop).toBe(98)
  })

  it.each([500, 540])('does not scroll an element already visible at %s', (bottom) => {
    browser()
    const scroller = { scrollTop: 30 } as HTMLElement
    const el = { getBoundingClientRect: () => ({ bottom }) } as HTMLElement
    ensureVisible(el, scroller)
    expect(scroller.scrollTop).toBe(30)
  })

  it('falls back to innerHeight without visualViewport', () => {
    const { win } = browser()
    win.visualViewport = null
    const scroller = { scrollTop: 0 } as HTMLElement
    const el = { getBoundingClientRect: () => ({ bottom: 900 }) } as HTMLElement
    ensureVisible(el, scroller)
    expect(scroller.scrollTop).toBe(108)
  })
})
