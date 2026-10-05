import { batch, signal } from '@preact/signals'

export const drawerOpen = signal(false)
export const headerMenuOpen = signal(false)

export function closeTransientUI() {
  batch(() => {
    drawerOpen.value = false
    headerMenuOpen.value = false
  })
}
