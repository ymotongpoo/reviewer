import { downloadJSON } from '../inputlog'

// C3 introduces media.ts. Keep these probe-only copies aligned with section 4.1.
const queries = {
  hoverNone: '(hover: none)', pointerCoarse: '(pointer: coarse)',
  anyPointerCoarse: '(any-pointer: coarse)', anyHoverHover: '(any-hover: hover)',
  COMPACT: '(max-width: 599.98px)', MEDIUM: '(min-width: 600px) and (max-width: 839.98px)',
  NARROW: '(max-width: 839.98px)', TOUCH: '(hover: none) and (pointer: coarse)',
}
const mode = new URLSearchParams(location.search).get('interactive-widget')
const widget = mode === 'resizes-content' ? mode : 'resizes-visual'
document.querySelector('meta[name="viewport"]')!.setAttribute('content', `width=device-width, initial-scale=1, viewport-fit=cover, interactive-widget=${widget}`)
for (const [id, value] of [['visual', 'resizes-visual'], ['content', 'resizes-content']]) {
  document.getElementById(id)!.onclick = () => {
    const url = new URL(location.href)
    url.searchParams.set('interactive-widget', value)
    location.assign(url)
  }
}
function measure(style: string) {
  const el = document.createElement('div')
  el.className = 'measure'
  el.style.cssText = style
  document.body.append(el)
  return el
}
const heights = Object.fromEntries(['vh', 'svh', 'lvh', 'dvh'].map((unit) => [unit, measure(`height:100${unit}`)]))
const safe = measure('padding:env(safe-area-inset-top) env(safe-area-inset-right) env(safe-area-inset-bottom) env(safe-area-inset-left)')
const rem = measure('height:1rem')
function snapshot(event: string) {
  const vv = window.visualViewport
  const css = getComputedStyle(safe)
  return {
    t: performance.now(), event, widget,
    innerWidth, innerHeight, visualViewport: vv && { width: vv.width, height: vv.height, offsetTop: vv.offsetTop, scale: vv.scale },
    heights: Object.fromEntries(Object.entries(heights).map(([unit, el]) => [unit, el.getBoundingClientRect().height])),
    safeArea: { top: parseFloat(css.paddingTop), right: parseFloat(css.paddingRight), bottom: parseFloat(css.paddingBottom), left: parseFloat(css.paddingLeft) },
    media: Object.fromEntries(Object.entries(queries).map(([name, query]) => [name, { query, matches: matchMedia(query).matches }])),
    devicePixelRatio, rem: rem.getBoundingClientRect().height, ua: navigator.userAgent,
  }
}
const samples: ReturnType<typeof snapshot>[] = []
function record(event: string) {
  const sample = snapshot(event)
  samples.push(sample)
  document.getElementById('current')!.textContent = JSON.stringify(sample, null, 2)
  document.getElementById('history')!.append(document.createTextNode(JSON.stringify(sample) + '\n'))
}
for (const name of ['resize', 'orientationchange']) window.addEventListener(name, () => record(name))
window.visualViewport?.addEventListener('resize', () => record('visualViewport.resize'))
document.getElementById('save')!.onclick = () => downloadJSON(JSON.stringify(samples, null, 2), 'viewport-measurements.json')
record('load')
