import { copyText } from './clipboard'

export interface InputLogEntry {
  t: number
  type: string
  key?: string
  code?: string
  keyCode?: number
  isComposing?: boolean
  inputType?: string
  data?: string | null
  cancelable?: boolean
  defaultPrevented?: boolean
  target?: string
  selStart?: number
  selEnd?: number
  valueLen?: number
  vv?: { h: number; top: number }
  inner?: { h: number }
}

export function formatEntry(entry: InputLogEntry): InputLogEntry {
  // Copy only diagnostic fields, never an event target, value, or clipboard body.
  const { t, type, key, code, keyCode, isComposing, inputType, data, cancelable,
    defaultPrevented, target, selStart, selEnd, valueLen, vv, inner } = entry
  return { t, type, key, code, keyCode, isComposing, inputType,
    data: typeof data === 'string' ? Array.from(data).slice(0, 20).join('') : data,
    cancelable, defaultPrevented, target, selStart, selEnd, valueLen,
    vv: vv && { h: vv.h, top: vv.top }, inner: inner && { h: inner.h } }
}

export function createInputLog() {
  const entries: InputLogEntry[] = []
  let next = 0
  return {
    push(entry: InputLogEntry) {
      entries[next] = formatEntry(entry)
      next = (next + 1) % 3000
    },
    entries: () => entries.length < 3000 ? entries.slice() : entries.slice(next).concat(entries.slice(0, next)),
    clear() { entries.length = 0; next = 0 },
    get size() { return entries.length },
  }
}

type StorageLike = Pick<Storage, 'getItem' | 'setItem'>
export function inputLogEnabled(search: string, storage: () => StorageLike): boolean {
  const flag = new URLSearchParams(search).get('inputlog')
  if (flag === '0' || flag === '1') {
    try { storage().setItem('reviewer.inputlog', flag) } catch { /* Explicit URL still works without storage. */ }
    return flag === '1'
  }
  try { return storage().getItem('reviewer.inputlog') === '1' } catch { return false }
}

export function initInputLog(options: {
  search?: string; storage?: () => StorageLike; start?: () => void; force?: boolean
} = {}) {
  const enabled = options.force || inputLogEnabled(options.search ?? window.location.search, options.storage ?? (() => localStorage))
  if (enabled) (options.start ?? startInputLog)()
}

declare global {
  interface Window { __reviewerInputLog?: ReturnType<typeof createInputLog> & { dispose(): void } }
}

function startInputLog() {
  if (window.__reviewerInputLog) return
  const log = createInputLog()
  const root = document.createElement('div')
  root.dataset.inputlog = ''
  root.style.cssText = 'position:fixed;left:8px;bottom:8px;z-index:10000;font:12px sans-serif;color:#111;background:#fff;border:1px solid #777;padding:4px;border-radius:4px;max-width:calc(100vw - 16px)'
  const toggle = document.createElement('button')
  const menu = document.createElement('div')
  menu.hidden = true
  toggle.setAttribute('aria-expanded', 'false')
  toggle.onclick = () => { menu.hidden = !menu.hidden; toggle.setAttribute('aria-expanded', String(!menu.hidden)) }
  const update = () => { toggle.textContent = `入力ログ ${log.size}件` }
  const json = () => JSON.stringify(log.entries(), null, 2)
  const button = (label: string, action: () => void) => {
    const el = document.createElement('button')
    el.textContent = label
    el.onclick = action
    menu.append(el)
  }
  button('JSON をダウンロード', () => downloadJSON(json(), 'reviewer-inputlog.json'))
  const status = document.createElement('span')
  status.setAttribute('role', 'status')
  button('コピー', () => {
    void copyText(json()).then(() => { status.textContent = 'コピーしました' }, () => { status.textContent = 'コピーできませんでした。JSON をダウンロードしてください。' })
  })
  button('クリア', () => { log.clear(); update(); status.textContent = '' })
  menu.append(status)
  root.append(toggle, menu)
  document.body.append(root)
  update()

  const record = (event: Event) => {
    const el = event.type === 'selectionchange' ? document.activeElement : event.target
    if (el instanceof Element && root.contains(el)) return
    let target: string | undefined
    if (el instanceof Element) {
      target = el.matches('[data-inputlog-target="block"]') ? 'block'
        : el.matches('.inline-input') ? 'inline-input'
        : el.closest('.composer') ? 'composer'
        : el.closest('.reply-form') ? 'reply' : el.tagName.toLowerCase()
    }
    const input = el instanceof HTMLTextAreaElement || el instanceof HTMLInputElement ? el : undefined
    const key = event instanceof KeyboardEvent ? event : undefined
    const data = event instanceof InputEvent || event instanceof CompositionEvent ? event.data : undefined
    log.push({ t: performance.now(), type: event.type, target,
      key: key?.key, code: key?.code, keyCode: key?.keyCode,
      isComposing: key?.isComposing ?? (event instanceof InputEvent ? event.isComposing : undefined),
      inputType: event instanceof InputEvent ? event.inputType : undefined, data,
      cancelable: event.cancelable, defaultPrevented: event.defaultPrevented,
      selStart: input?.selectionStart ?? undefined, selEnd: input?.selectionEnd ?? undefined,
      valueLen: input?.value.length,
      vv: window.visualViewport ? { h: visualViewport!.height, top: visualViewport!.offsetTop } : undefined,
      inner: { h: innerHeight } })
    update()
  }
  const removers: (() => void)[] = []
  const listen = (target: EventTarget, names: string[]) => {
    for (const name of names) {
      target.addEventListener(name, record)
      removers.push(() => target.removeEventListener(name, record))
    }
  }
  listen(window, ['keydown', 'keyup', 'beforeinput', 'input', 'compositionstart', 'compositionupdate', 'compositionend', 'focusin', 'focusout', 'paste', 'pagehide', 'pageshow', 'online', 'offline'])
  listen(document, ['selectionchange', 'visibilitychange'])
  if (window.visualViewport) listen(visualViewport!, ['resize'])
  window.__reviewerInputLog = { ...log, get size() { return log.size }, clear() { log.clear(); update() },
    dispose() { removers.forEach((remove) => remove()); root.remove(); delete window.__reviewerInputLog } }
}

export function downloadJSON(json: string, filename: string) {
  const url = URL.createObjectURL(new Blob([json], { type: 'application/json' }))
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.append(a)
  a.click()
  a.remove()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}
