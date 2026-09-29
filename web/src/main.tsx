import { render } from 'preact'
import { useEffect } from 'preact/hooks'
import './style.css'
import { connectEvents, fatal, info, refreshAll, toasts } from './state'
import { route } from './router'
import { Header } from './components/Header'
import { Tree } from './components/Tree'
import { FileView } from './components/FileView'
import { Overview } from './components/Overview'
import { RoundHistory } from './components/RoundHistory'
import { Home } from './components/Home'
import { projectId } from './api'

function App() {
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
        <aside class="sidebar">
          <Tree />
        </aside>
        <main class="main">
          {r.page === 'file' ? (
            <FileView key="file" path={r.path} line={r.line} />
          ) : r.page === 'round' ? (
            <RoundHistory round={r.round} path={r.path} />
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
