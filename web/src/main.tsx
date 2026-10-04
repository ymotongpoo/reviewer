import { render } from 'preact'
import { useEffect } from 'preact/hooks'
import './style.css'
import './responsive.css'
import './theme'
import { connectEvents, fatal, info, refreshAll, toasts } from './state'
import { route } from './router'
import { Header } from './components/Header'
import { Tree } from './components/Tree'
import { FileView } from './components/FileView'
import { Overview } from './components/Overview'
import { RoundHistory } from './components/RoundHistory'
import { Settings } from './components/Settings'
import { Home } from './components/Home'
import { projectId } from './api'
import { initInputLog } from './inputlog'
import { initViewport } from './viewport'
import { isNarrow } from './media'
import { closeTransientUI, drawerOpen, headerMenuOpen } from './ui'
import { useDismiss } from './components/Sheet'

initInputLog()
const stopViewport = initViewport()
import.meta.hot?.dispose(stopViewport)

function App() {
  const narrow = isNarrow.value
  const drawer = narrow && drawerOpen.value
  const sidebar = useDismiss(drawer, closeTransientUI, { focus: true, returnTo: '.drawer-toggle' })
  useEffect(() => { if (!narrow) closeTransientUI() }, [narrow])
  useEffect(() => {
    refreshAll()
      .then(connectEvents)
      .catch((e) => (fatal.value = (e as Error).message))
  }, [])

  if (fatal.value) return <div class="empty error">{fatal.value}</div>
  if (!info.value) return <div class="empty">読み込み中…</div>
  const r = route.value
  return (
    <div class="layout">
      <Header />
      <div class="body">
        {drawer && <div class="drawer-backdrop" aria-hidden="true" onClick={closeTransientUI} />}
        <aside ref={sidebar} id="file-drawer" class={`sidebar${drawer ? ' open' : ''}`}
          role={drawer ? 'dialog' : undefined} aria-modal={drawer ? 'true' : undefined}
          aria-label={drawer ? 'ファイル一覧' : undefined} tabIndex={narrow ? -1 : undefined}
          inert={narrow && !drawer}
          onClick={(e) => { if (narrow && (e.target as Element).closest('a.tree-row')) closeTransientUI() }}>
          <Tree />
        </aside>
        <main class="main" inert={drawer || (narrow && headerMenuOpen.value)}>
          {r.page === 'settings' ? (
            <Settings />
          ) : r.page === 'file' ? (
            <FileView key="file" path={r.path} line={r.line} />
          ) : r.page === 'round' ? (
            <RoundHistory round={r.round} path={r.path} phase={r.phase} />
          ) : (
            <Overview />
          )}
        </main>
      </div>
      <div class="toasts">
        {toasts.value.map((t) => (
          <div class={`toast ${t.kind}`} key={t.id}>
            {t.text}
          </div>
        ))}
      </div>
    </div>
  )
}

render(projectId ? <App /> : <Home />, document.getElementById('app')!)
