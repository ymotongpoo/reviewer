import { computed, signal } from '@preact/signals'

export type Theme = 'auto' | 'light' | 'dark'

const KEY = 'reviewer.theme'

function load(): Theme {
  try {
    const v = localStorage.getItem(KEY)
    return v === 'light' || v === 'dark' ? v : 'auto'
  } catch {
    return 'auto'
  }
}

const mq = typeof window !== 'undefined' ? window.matchMedia?.('(prefers-color-scheme: dark)') : undefined

/** The theme picked by the user; "auto" follows the OS. */
export const theme = signal<Theme>(load())
const osDark = signal<boolean>(mq?.matches ?? false)
mq?.addEventListener('change', (e) => (osDark.value = e.matches))

/** Whether the page is currently dark. */
export const isDark = computed(() => (theme.value === 'auto' ? osDark.value : theme.value === 'dark'))

function apply() {
  const root = document.documentElement
  if (theme.value === 'auto') delete root.dataset.theme
  else root.dataset.theme = theme.value
}
apply()

export function setTheme(t: Theme) {
  theme.value = t
  try {
    if (t === 'auto') localStorage.removeItem(KEY)
    else localStorage.setItem(KEY, t)
  } catch {
    // storage unavailable
  }
  apply()
}

const order: Theme[] = ['auto', 'light', 'dark']
export const themeText: Record<Theme, string> = { auto: '自動', light: 'ライト', dark: 'ダーク' }
export const themeIcon: Record<Theme, string> = { auto: '🖥', light: '☀', dark: '🌙' }

export function nextTheme(t: Theme): Theme {
  return order[(order.indexOf(t) + 1) % order.length]
}
