// Keeps the source and the preview of a Markdown file at the same place.
// Positions are fractional source lines: line n covers [n, n + 1). Comments
// shown after a row take no lines, so the preview waits while the source
// scrolls past them.

/** A source line and where it starts in the preview, in px from the top of its content. */
export interface Anchor {
  line: number
  y: number
}

/** The longest run of anchors whose lines and positions both increase. */
export function monotonic(anchors: Anchor[]): Anchor[] {
  // Sorted by position, and by line descending for equal positions so that
  // at most one of them is kept.
  const list = anchors
    .filter((a) => Number.isFinite(a.line) && Number.isFinite(a.y))
    .sort((a, b) => a.y - b.y || b.line - a.line)
  const tails: number[] = []
  const prev: number[] = []
  list.forEach((a, i) => {
    let lo = 0
    let hi = tails.length
    while (lo < hi) {
      const mid = (lo + hi) >> 1
      if (list[tails[mid]].line < a.line) lo = mid + 1
      else hi = mid
    }
    prev[i] = lo > 0 ? tails[lo - 1] : -1
    tails[lo] = i
  })
  const out: Anchor[] = []
  for (let i = tails.length ? tails[tails.length - 1] : -1; i >= 0; i = prev[i]) out.push(list[i])
  return out.reverse().filter((a, i, all) => i === 0 || a.y > all[i - 1].y)
}

function interpolate(anchors: Anchor[], value: number, from: keyof Anchor, to: keyof Anchor): number {
  if (anchors.length === 0) return 0
  if (value <= anchors[0][from]) return anchors[0][to]
  for (let i = 1; i < anchors.length; i++) {
    const a = anchors[i - 1]
    const b = anchors[i]
    if (value < b[from]) return a[to] + ((value - a[from]) / (b[from] - a[from])) * (b[to] - a[to])
  }
  return anchors[anchors.length - 1][to]
}

/** Where a fractional line is in the preview. */
export function yAt(anchors: Anchor[], line: number): number {
  return interpolate(anchors, line, 'line', 'y')
}

/** The fractional line at a position in the preview. */
export function lineAt(anchors: Anchor[], y: number): number {
  return interpolate(anchors, y, 'y', 'line')
}

/** The vertical extent of a source row in client coordinates. */
export type RowRect = (line: number) => { top: number; bottom: number } | undefined

/** The fractional line at client y among rows 1..count. Anything after a row, such as its comments, is at the start of the next line. */
export function sourceLineAt(count: number, y: number, rect: RowRect): number {
  let lo = 1
  let hi = count
  let found = 0
  while (lo <= hi) {
    const mid = (lo + hi) >> 1
    const r = rect(mid)
    if (!r) return 1
    if (r.top <= y) {
      found = mid
      lo = mid + 1
    } else {
      hi = mid - 1
    }
  }
  if (found === 0) return 1
  const r = rect(found)!
  const h = r.bottom - r.top
  return y < r.bottom && h > 0 ? found + (y - r.top) / h : found + 1
}

/** The client y of a fractional line among rows 1..count. */
export function sourceYAt(count: number, line: number, rect: RowRect): number | undefined {
  if (count === 0) return undefined
  const n = Math.min(count, Math.max(1, Math.floor(line)))
  const r = rect(n)
  if (!r) return undefined
  return r.top + Math.min(1, Math.max(0, line - n)) * (r.bottom - r.top)
}

const rowRect: RowRect = (line) => document.getElementById(`L${line}`)?.getBoundingClientRect()

/**
 * Scrolls the source in .main and the preview pane together. In tabs, only
 * one of them is shown and both scroll with .main.
 */
export class ScrollSync {
  /** Rows of the source. */
  rows = 0
  /** Lines of the previewed content. */
  lines = 0
  pane: HTMLElement | null = null
  private anchors: Anchor[] | null = null
  // Positions set here, whose scroll events are not the user's.
  private set = new Map<HTMLElement, number>()

  main(): HTMLElement | null {
    return document.querySelector<HTMLElement>('.main')
  }

  /** Drops the measured preview, after it is rendered again or resized. */
  invalidate() {
    this.anchors = null
  }

  /** Reports whether a scroll event of el comes from a position set here. */
  own(el: HTMLElement): boolean {
    const at = this.set.get(el)
    this.set.delete(el)
    return at !== undefined && Math.abs(el.scrollTop - at) < 2
  }

  private scroll(el: HTMLElement, top: number) {
    const before = el.scrollTop
    el.scrollTop = top
    if (el.scrollTop !== before) this.set.set(el, el.scrollTop)
  }

  /** The client y below the sticky headers of the source. */
  private sourceTop(main: HTMLElement): number {
    let top = main.getBoundingClientRect().top
    for (const el of main.querySelectorAll<HTMLElement>('.file-head, .preview-tabs')) {
      if (getComputedStyle(el).position === 'sticky') top = Math.max(top, el.getBoundingClientRect().bottom)
    }
    return top
  }

  /** The client y of the top of the preview's content. */
  private contentTop(pane: HTMLElement): number {
    return pane.getBoundingClientRect().top - pane.scrollTop
  }

  private measure(pane: HTMLElement): Anchor[] {
    if (this.anchors) return this.anchors
    const top = this.contentTop(pane)
    const list: Anchor[] = [{ line: 1, y: 0 }]
    for (const el of pane.querySelectorAll<HTMLElement>('[data-source-line]')) {
      // Footnotes are gathered at the end, away from their lines.
      if (el.closest('.footnotes') || el.getClientRects().length === 0) continue
      list.push({ line: Number(el.dataset.sourceLine), y: el.getBoundingClientRect().top - top })
    }
    const body = pane.querySelector<HTMLElement>('.znc, .preview-md')
    if (body) list.push({ line: this.lines + 1, y: body.getBoundingClientRect().bottom - top })
    this.anchors = monotonic(list)
    return this.anchors
  }

  /** The height the sticky bar of the preview covers. */
  private inset(pane: HTMLElement): number {
    return pane.querySelector<HTMLElement>('.preview-bar')?.getBoundingClientRect().height ?? 0
  }

  /** Scrolls the preview to the source. */
  fromSource() {
    const main = this.main()
    const pane = this.pane
    if (!main || !pane || pane.hidden || this.rows === 0) return
    if (main.scrollTop <= 0) return this.scroll(pane, 0)
    const line = sourceLineAt(this.rows, this.sourceTop(main), rowRect)
    this.scroll(pane, yAt(this.measure(pane), line) - this.inset(pane))
  }

  /** Scrolls the source to the preview. */
  fromPreview() {
    const main = this.main()
    const pane = this.pane
    if (!main || !pane || this.rows === 0) return
    if (pane.scrollTop <= 0) return this.scroll(main, 0)
    const line = lineAt(this.measure(pane), pane.scrollTop + this.inset(pane))
    const y = sourceYAt(this.rows, line, rowRect)
    if (y !== undefined) this.scroll(main, main.scrollTop + y - this.sourceTop(main))
  }

  /** The line at the top of the source shown in .main, or 0 at the very top. */
  sourceLine(): number {
    const main = this.main()
    if (!main || main.scrollTop <= 0 || this.rows === 0) return 0
    return sourceLineAt(this.rows, this.sourceTop(main), rowRect)
  }

  /** The line at the top of the preview shown in .main, or 0 at the very top. */
  previewLine(): number {
    const main = this.main()
    const pane = this.pane
    if (!main || !pane || main.scrollTop <= 0) return 0
    // In tabs the preview is measured when shown.
    this.invalidate()
    return lineAt(this.measure(pane), this.sourceTop(main) - this.contentTop(pane))
  }

  /** Scrolls .main to show a line of the source at the top. */
  showSource(line: number) {
    const main = this.main()
    if (!main) return
    const y = line > 0 ? sourceYAt(this.rows, line, rowRect) : undefined
    main.scrollTop = y === undefined ? 0 : main.scrollTop + y - this.sourceTop(main)
  }

  /** Scrolls .main to show a line of the preview at the top. */
  showPreview(line: number) {
    const main = this.main()
    const pane = this.pane
    if (!main || !pane) return
    this.invalidate()
    main.scrollTop = line > 0 ? main.scrollTop + this.contentTop(pane) + yAt(this.measure(pane), line) - this.sourceTop(main) : 0
  }
}
