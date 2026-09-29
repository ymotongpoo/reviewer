import { nextTheme, setTheme, theme, themeIcon, themeText } from '../theme'

/** Cycles auto → light → dark. */
export function ThemeToggle() {
  const t = theme.value
  const next = nextTheme(t)
  return (
    <button
      class="btn small theme-toggle"
      onClick={() => setTheme(next)}
      title={`テーマ: ${themeText[t]}（クリックで${themeText[next]}に切り替え）`}
      aria-label={`テーマ: ${themeText[t]}`}
    >
      {themeIcon[t]} {themeText[t]}
    </button>
  )
}
