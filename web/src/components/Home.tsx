import { useEffect, useRef, useState } from 'preact/hooks'
import { api } from '../api'
import type { DirEntry, DirListing, ProjectSummary, ServerInfo } from '../types'
import { fmt } from './Overview'
import { ThemeToggle } from './ThemeToggle'

function statusText(p: ProjectSummary) {
  if (!p.exists) return 'ディレクトリがありません'
  if (!p.initialized) return 'まだレビューしていません'
  return `ラウンド${p.round}・${p.roundStatus === 'submitted' ? '提出済み' : '下書き中'}`
}

function parentOf(path: string) {
  const i = path.replace(/\/+$/, '').lastIndexOf('/')
  return i <= 0 ? '/' : path.slice(0, i)
}

/** Directory picker shown at "/". */
export function Home() {
  const [info, setInfo] = useState<ServerInfo | null>(null)
  const [projects, setProjects] = useState<ProjectSummary[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [confirm, setConfirm] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  async function refresh() {
    try {
      const [s, ps] = await Promise.all([api.server(), api.projects()])
      setInfo(s)
      setProjects(ps)
    } catch (e) {
      setError((e as Error).message)
    }
  }
  useEffect(() => {
    document.title = 'reviewer'
    void refresh()
  }, [])

  /** Opens path, asking first when it has no review data yet. */
  async function open(path: string, known?: boolean) {
    setError(null)
    if (known === undefined) {
      try {
        known = (await api.listDir(path)).hasReviewer
      } catch (e) {
        setError((e as Error).message)
        return
      }
    }
    if (!known && confirm !== path) {
      setConfirm(path)
      return
    }
    setBusy(true)
    try {
      const r = await api.openProject(path)
      location.href = r.url
    } catch (e) {
      setError((e as Error).message)
      setBusy(false)
    }
  }

  return (
    <div class="home">
      <header class="app-header">
        <span class="brand">reviewer</span>
        <span class="muted small">{info?.version}</span>
        <span class="spacer" />
        <ThemeToggle />
      </header>
      <div class="home-body">
        {error && <div class="banner error">{error}</div>}
        {confirm && (
          <div class="banner warn">
            <span>
              <code>{confirm}</code> ではまだレビューしていません。ここでレビューを始めると <code>.reviewer/</code> が作られます。
            </span>
            <span class="spacer" />
            <button class="btn small primary" disabled={busy} onClick={() => void open(confirm, false)}>
              ここでレビューを始める
            </button>
            <button class="btn small" onClick={() => setConfirm(null)}>
              やめる
            </button>
          </div>
        )}

        <section class="card">
          <h2>ディレクトリを開く</h2>
          {info && <PathInput onOpen={(p) => void open(p)} initial={(info.roots[0] ?? '~') + '/'} />}
          {info && <p class="muted small">開けるのは {info.roots.join(', ')} の中のディレクトリです。</p>}
        </section>

        <section class="card">
          <h2>最近開いたディレクトリ</h2>
          {projects === null && <p class="muted">読み込み中…</p>}
          {projects?.length === 0 && <p class="muted">まだありません。上の入力欄かブラウザからディレクトリを開いてください。</p>}
          <div class="project-list">
            {projects?.map((p) => (
              <div class={`project-row ${p.exists ? '' : 'missing'}`}>
                <a class="project-main" href={p.exists ? `/p/${p.id}/` : undefined} onClick={(e) => {
                  if (!p.exists) e.preventDefault()
                }}>
                  <span class="project-name">
                    {p.name}
                    {p.open && <span class={`dot ${p.busy ? 'busy' : ''}`} title={p.busy ? 'エージェントが対応中' : '開いています'} />}
                  </span>
                  <span class="project-path">{p.display}</span>
                  <span class="project-meta">
                    <span class={`chip ${p.roundStatus === 'submitted' ? 'status-addressed' : ''}`}>{statusText(p)}</span>
                    {p.agent && <span class="chip">🤖 {p.agent.title}</span>}
                    <span class="muted small">{fmt(p.lastOpened)}</span>
                  </span>
                </a>
                <div class="project-actions">
                  {p.open && !p.busy && (
                    <button class="btn small" onClick={async () => {
                      await api.closeProject(p.id).catch((e) => setError((e as Error).message))
                      void refresh()
                    }}>
                      閉じる
                    </button>
                  )}
                  <button class="btn small danger-text" title="一覧から外します（レビューのデータは残ります）" disabled={p.busy} onClick={async () => {
                    await api.forgetProject(p.id).catch((e) => setError((e as Error).message))
                    void refresh()
                  }}>
                    一覧から外す
                  </button>
                </div>
              </div>
            ))}
          </div>
        </section>

        <section class="card">
          <h2>ブラウズ</h2>
          <Browser onOpen={(e) => void open(e.path, e.hasReviewer)} />
        </section>
      </div>
    </div>
  )
}

/** Path input with completion from the server's directory listing. */
function PathInput({ onOpen, initial }: { onOpen: (path: string) => void; initial: string }) {
  const [value, setValue] = useState(initial)
  const [suggestions, setSuggestions] = useState<DirEntry[]>([])
  const [sel, setSel] = useState(-1)
  const timer = useRef<ReturnType<typeof setTimeout>>()

  function complete(v: string) {
    clearTimeout(timer.current)
    timer.current = setTimeout(async () => {
      const dir = v.endsWith('/') ? v : parentOf(v)
      const prefix = v.endsWith('/') ? '' : v.slice(v.lastIndexOf('/') + 1).toLowerCase()
      try {
        const l = await api.listDir(dir || '/')
        setSuggestions(l.entries.filter((e) => e.name.toLowerCase().startsWith(prefix)).slice(0, 12))
        setSel(-1)
      } catch {
        setSuggestions([])
      }
    }, 150)
  }

  function pick(e: DirEntry) {
    const v = e.display + '/'
    setValue(v)
    complete(v)
  }

  return (
    <div class="path-input">
      <div class="path-row">
        <input
          type="text"
          value={value}
          spellcheck={false}
          placeholder="~/repos/..."
          onFocus={() => complete(value)}
          onBlur={() => setTimeout(() => setSuggestions([]), 150)}
          onInput={(e) => {
            setValue(e.currentTarget.value)
            complete(e.currentTarget.value)
          }}
          onKeyDown={(e) => {
            if (e.key === 'ArrowDown') {
              e.preventDefault()
              setSel((s) => Math.min(s + 1, suggestions.length - 1))
            } else if (e.key === 'ArrowUp') {
              e.preventDefault()
              setSel((s) => Math.max(s - 1, -1))
            } else if (e.key === 'Tab' && suggestions.length > 0) {
              e.preventDefault()
              pick(suggestions[Math.max(sel, 0)])
            } else if (e.key === 'Enter') {
              e.preventDefault()
              if (sel >= 0) pick(suggestions[sel])
              else {
                setSuggestions([])
                onOpen(value.replace(/\/+$/, '') || '/')
              }
            } else if (e.key === 'Escape') {
              setSuggestions([])
            }
          }}
        />
        <button class="btn primary" onClick={() => onOpen(value.replace(/\/+$/, '') || '/')}>
          開く
        </button>
      </div>
      {suggestions.length > 0 && (
        <ul class="suggestions">
          {suggestions.map((s, i) => (
            <li class={i === sel ? 'on' : ''} onMouseDown={(e) => {
              e.preventDefault()
              pick(s)
            }}>
              📁 {s.name}
              {s.hasReviewer && <span class="chip small-chip">レビュー中</span>}
            </li>
          ))}
        </ul>
      )}
      <p class="muted small">Tab で補完、Enter で開きます。</p>
    </div>
  )
}

function Browser({ onOpen }: { onOpen: (e: DirEntry) => void }) {
  const [path, setPath] = useState('')
  const [listing, setListing] = useState<DirListing | null>(null)
  const [error, setError] = useState<string | null>(null)
  useEffect(() => {
    setError(null)
    api
      .listDir(path)
      .then(setListing)
      .catch((e) => setError((e as Error).message))
  }, [path])
  if (error) return <div class="banner error">{error}</div>
  if (!listing) return <p class="muted">読み込み中…</p>
  return (
    <div class="browser">
      <div class="browser-head">
        {listing.path ? (
          <>
            <button class="btn small" onClick={() => setPath(listing.parent ?? '')}>
              ↑ 上へ
            </button>
            <code>{listing.display}</code>
            <span class="spacer" />
            <button class="btn small primary" onClick={() => onOpen({ name: '', path: listing.path!, display: listing.display!, hasReviewer: listing.hasReviewer })}>
              ここを開く
            </button>
          </>
        ) : (
          <span class="muted small">開けるディレクトリ</span>
        )}
      </div>
      <ul class="dir-list">
        {listing.entries.map((e) => (
          <li>
            <button class="dir-name" onClick={() => setPath(e.path)}>
              📁 {e.name}
            </button>
            {e.hasReviewer && <span class="chip small-chip">レビュー中</span>}
            <span class="spacer" />
            <button class="btn small" onClick={() => onOpen(e)}>
              開く
            </button>
          </li>
        ))}
        {listing.entries.length === 0 && <li class="muted small">サブディレクトリはありません</li>}
      </ul>
    </div>
  )
}
