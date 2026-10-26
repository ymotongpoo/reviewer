import { Portal } from './Portal'
import { useEffect, useState } from 'preact/hooks'
import { api, projectId, sessionId } from '../api'
import { copyText } from '../clipboard'
import { agentInfo, comments, dirtyReplies, draftCount, flushComposers, refreshComments, dismissResponseBanner, gitStatus, info, refreshAll, refreshGit, refreshInfo, refreshSessions, responseBanner, sessions, toast } from '../state'
import { connState } from '../connection'
import { AgentChip } from './Agent'
import { AnnotateButton } from './Annotate'
import { GitButton } from './Git'
import { isCompact, isNarrow } from '../media'
import { closeTransientUI, drawerOpen, headerMenuOpen } from '../ui'
import { SheetBackdrop, useDismiss } from './Sheet'
import type { GitStatus, ProjectSummary, SessionSummary, SubmitResult } from '../types'

export function CopyPrompt({ prompt, label = '指示文をコピー' }: { prompt: string; label?: string }) {
  const [done, setDone] = useState(false)
  return (
    <div class="copy-prompt">
      <pre>{prompt}</pre>
      <button
        class={`btn ${done ? 'success' : 'primary'}`}
        onClick={async () => {
          try {
            await copyText(prompt)
            setDone(true)
            setTimeout(() => setDone(false), 2000)
          } catch {
            toast('コピーできませんでした。テキストを選択してコピーしてください', 'error')
          }
        }}
      >
        {done ? '✓ コピーしました' : `📋 ${label}`}
      </button>
    </div>
  )
}

/** Project name with a menu of recent projects. */
function ProjectSwitcher({ name, root }: { name: string; root: string }) {
  const [open, setOpen] = useState(false)
  const dismiss = useDismiss(open, () => setOpen(false))
  const [list, setList] = useState<ProjectSummary[] | null>(null)
  const session = info.value?.session
  const narrow = isNarrow.value
  useEffect(() => {
    document.title = session && session.id !== 'main' ? `${session.name} · ${name} - reviewer` : `${name} - reviewer`
  }, [name, session?.id, session?.name])
  async function toggle() {
    if (!open) api.projects().then(setList).catch(() => setList([]))
    setOpen(!open)
  }
  return (
    <span class="switcher" ref={dismiss}>
      <button class="project" title={session && session.id !== 'main' ? `${root}（セッション: ${session.name}）` : root}
        aria-expanded={open} aria-controls="project-menu" onClick={() => void toggle()}>
        {narrow && session && session.id !== 'main' ? `📑 ${session.name}` : name} ▾
      </button>
      {open && (
        <div id="project-menu" class="switcher-menu" onMouseLeave={narrow ? undefined : () => setOpen(false)}>
          {narrow && <>
            <div class="switcher-heading">{name} のレビューセッション</div>
            <SessionMenu />
            <div class="switcher-heading">ディレクトリ</div>
          </>}
          {list === null && <div class="muted small">読み込み中…</div>}
          {list
            ?.filter((p) => p.exists)
            .map((p) => (
              <a class={`switcher-item ${p.id === projectId ? 'on' : ''}`} href={`/p/${p.id}/`}>
                <span class="project-name">{p.name}</span>
                <span class="muted small">{p.display}</span>
              </a>
            ))}
          <div class="switcher-foot">
            <a href="/">別のディレクトリを開く…</a>
            {projectId && (
              <button
                class="link"
                onClick={async () => {
                  try {
                    await api.closeProject(projectId!)
                    location.href = '/'
                  } catch (e) {
                    toast((e as Error).message, 'error')
                  }
                }}
              >
                このプロジェクトを閉じる
              </button>
            )}
          </div>
        </div>
      )}
    </span>
  )
}

function sessionStatus(s: SessionSummary) {
  if (!s.initialized || !s.round) return 'まだレビューしていません'
  return `ラウンド${s.round}・${s.roundStatus === 'submitted' ? '提出済み' : '下書き中'}`
}

/**
 * The sessions of the directory with actions to add, rename and archive
 * them. A directory holding several books or talks gets one session for
 * each, each with its own rounds, comments and agent.
 */
function SessionMenu() {
  const current = info.value!.session
  const [mode, setMode] = useState<'list' | 'create' | 'rename' | 'branch'>('list')
  const [name, setName] = useState('')
  const [branch, setBranch] = useState('')
  const [git, setGit] = useState<GitStatus | null>(null)
  const [loadingBranches, setLoadingBranches] = useState(false)
  const [formError, setFormError] = useState<string | null>(null)
  const [showArchived, setShowArchived] = useState(false)
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    void refreshSessions().catch(() => {})
    if (info.value?.git) void refreshGit().catch(() => {})
  }, [])
  useEffect(() => {
    setFormError(null)
    setLoadingBranches(false)
    if ((mode !== 'create' && mode !== 'branch') || !info.value?.git) return
    let active = true
    setLoadingBranches(true)
    setGit(null)
    setBranch('')
    api.gitStatus().then((next) => {
      if (!active) return
      setGit(next)
      setBranch(mode === 'branch' ? current.branch ?? '' : next.branches.includes(next.branch) ? next.branch : '')
    }).catch((e) => {
      if (active) setFormError(`ローカルブランチを取得できませんでした: ${(e as Error).message}`)
    }).finally(() => { if (active) setLoadingBranches(false) })
    return () => { active = false }
  }, [mode])
  const all = sessions.value
  const visible = all.filter((s) => !s.archived || s.id === sessionId)
  const archived = all.filter((s) => s.archived && s.id !== sessionId)

  async function activateBranch(selected: string) {
    if (!selected) return
    const active = await api.gitStatus()
    if (active.branch !== selected) await api.gitSwitch(selected, '', active.head ?? '')
    await refreshGit()
  }

  async function create() {
    setBusy(true)
    setFormError(null)
    try {
      await activateBranch(branch)
      const r = await api.createSession(name, branch || undefined)
      location.href = r.url
    } catch (e) {
      setFormError((e as Error).message)
      toast((e as Error).message, 'error')
      setBusy(false)
    }
  }
  async function update(patch: { name?: string; archived?: boolean; branch?: string }) {
    setBusy(true)
    setFormError(null)
    try {
      if (patch.branch !== undefined) await activateBranch(patch.branch)
      await api.updateSession(patch)
      await Promise.all([refreshInfo(), refreshSessions()])
      setMode('list')
      if (patch.archived) toast(`「${current.name}」をアーカイブしました`, 'success')
      if (patch.archived === false) toast(`「${current.name}」を元に戻しました`, 'success')
      if (patch.branch !== undefined) toast(patch.branch ? `ローカルブランチを「${patch.branch}」に設定しました` : 'ローカルブランチの設定を解除しました', 'success')
    } catch (e) {
      setFormError((e as Error).message)
      toast((e as Error).message, 'error')
    } finally {
      setBusy(false)
    }
  }

  if (mode !== 'list') return (
    <form class="session-form" onSubmit={(e) => {
      e.preventDefault()
      if (mode === 'create') void create()
      else if (mode === 'branch') void update({ branch })
      else void update({ name })
    }}>
      {formError && <div class="banner error" role="alert">{formError}</div>}
      {mode !== 'branch' && (
        <label class="field">
          <span>{mode === 'create' ? '新しいセッションの名前' : 'セッションの名前'}</span>
          <input type="text" value={name} autoFocus maxLength={80}
            placeholder="例: tinygo-otel-esp32 の第3章"
            onInput={(e) => setName(e.currentTarget.value)} />
        </label>
      )}
      {(mode === 'create' || mode === 'branch') && info.value?.git && (
        <>
          <label class="field">
            <span>このセッションのローカルブランチ</span>
            <select value={branch} disabled={busy || loadingBranches || !git}
              onChange={(e) => setBranch(e.currentTarget.value)}>
              <option value="">未設定（現在の作業ブランチを使用）</option>
              {branch && git && !git.branches.includes(branch) && <option value={branch} disabled>{branch}（見つかりません）</option>}
              {git?.branches.map((b) => <option value={b}>{b}{b === git.branch ? '（現在）' : ''}</option>)}
            </select>
          </label>
          {loadingBranches ? <p class="muted small">ローカルブランチを読み込み中…</p> : git && (
            <p class="muted small">現在の作業ブランチ: <code>{git.branch || 'detached HEAD'}</code></p>
          )}
          <p class="muted small">選んだブランチをセッションに保存して切り替えます。ファイルと作業ブランチは同じディレクトリの全セッションで共有します。</p>
        </>
      )}
      {mode === 'create' && (
        <p class="muted small">
          セッションごとにラウンド、コメント、AI指摘、エージェントの送信先を別々に持ちます。ファイルとプリセットは共有します。
        </p>
      )}
      <div class="modal-actions">
        <button type="button" class="btn small" onClick={() => setMode('list')}>キャンセル</button>
        <button type="submit" class="btn small primary" disabled={busy || loadingBranches || (mode !== 'branch' && !name.trim()) || (mode === 'branch' && !git)}>
          {mode === 'create' ? '作成して開く' : mode === 'branch' && branch ? '保存して切り替える' : '保存'}
        </button>
      </div>
    </form>
  )
  return (
    <div class="session-menu">
      {visible.map((s) => (
        <a class={`switcher-item ${s.id === sessionId ? 'on' : ''}`} href={s.url}>
          <span class="project-name">
            {s.name}
            {s.busy && <span class="dot busy" title="エージェントが対応中" />}
          </span>
          <span class="muted small">
            {sessionStatus(s)}
            {s.branch && <span class="session-branch-name">⎇ {s.branch}</span>}
            {s.agent && ` · 🤖 ${s.agent.title}`}
          </span>
        </a>
      ))}
      {archived.length > 0 && (
        <button class="link small archived-toggle" onClick={() => setShowArchived(!showArchived)}>
          {showArchived ? '▾' : '▸'} アーカイブ済み（{archived.length}）
        </button>
      )}
      {showArchived && archived.map((s) => (
        <a class="switcher-item archived" href={s.url}>
          <span class="project-name">{s.name}</span>
          <span class="muted small">{sessionStatus(s)}</span>
        </a>
      ))}
      <div class="switcher-foot">
        {info.value?.git && <div class="session-branch-setting">
          <span class="muted small">このセッションのローカルブランチ</span>
          <span class="session-branch-name">{current.branch ? `⎇ ${current.branch}` : '未設定'}</span>
          {current.branch && gitStatus.value && current.branch !== gitStatus.value.branch && <>
            <span class="muted small">現在の作業ブランチ: {gitStatus.value.branch || 'detached HEAD'}</span>
            <button class="btn small" disabled={busy} onClick={() => void update({ branch: current.branch })}>設定したブランチに切り替える</button>
          </>}
          <button class="link" disabled={busy} onClick={() => setMode('branch')}>ローカルブランチを設定・変更</button>
        </div>}
        <button class="link" disabled={busy} onClick={() => { setName(''); setMode('create') }}>＋ 新しいセッション</button>
        <button class="link" disabled={busy} onClick={() => { setName(current.name); setMode('rename') }}>名前を変更</button>
        {current.id !== 'main' && (
          current.archived
            ? <button class="link" disabled={busy} onClick={() => void update({ archived: false })}>元に戻す</button>
            : <button class="link" disabled={busy} onClick={() => {
              if (confirm(`「${current.name}」をアーカイブしますか？ 一覧から隠れますが、データは残ります。`)) void update({ archived: true })
            }}>アーカイブ</button>
        )}
      </div>
    </div>
  )
}

/** The review session with its menu. Narrow headers have no room for it; there the directory menu holds the sessions. */
function SessionSwitcher() {
  const current = info.value!.session
  const [open, setOpen] = useState(false)
  const dismiss = useDismiss(open, () => setOpen(false))
  return (
    <span class="switcher session-switcher" ref={dismiss}>
      <button class="session-button" title={`レビューセッション: ${current.name}`}
        aria-expanded={open} aria-controls="session-menu" onClick={() => setOpen(!open)}>
        📑 {current.name}{current.archived ? '（アーカイブ済み）' : ''} ▾
      </button>
      {open && (
        <div id="session-menu" class="switcher-menu">
          <SessionMenu />
        </div>
      )}
    </span>
  )
}

export function ConnectionError({ fallback = '' }: { fallback?: string }) {
  if (connState.value === 'unavailable') return (
    <div class="banner error top" role="alert">
      このプロジェクトはサーバーで閉じられたか、見つかりません。
      <a href="/">ディレクトリの選択画面へ</a>
    </div>
  )
  if (connState.value === 'unauthorized') return (
    <div class="banner error top" role="alert">
      認証が切れました。起動時に表示された URL（?token= 付き）を開き直してください。
    </div>
  )
  return <>{fallback}</>
}

export function Header() {
  const i = info.value
  const [dialog, setDialog] = useState<'confirm' | SubmitResult | null>(null)
  const narrow = isNarrow.value
  const menu = narrow && headerMenuOpen.value
  const actions = useDismiss(menu, closeTransientUI, { focus: true, returnTo: '.header-menu-toggle' })
  if (!i) return null
  const submitted = i.roundStatus === 'submitted'

  return (
    <>
      <header class="app-header" inert={narrow && drawerOpen.value}>
        <button class="btn icon drawer-toggle" style={{ display: narrow ? undefined : 'none' }}
          aria-label="ファイル一覧" aria-controls="file-drawer" aria-expanded={narrow && drawerOpen.value}
          onClick={() => { closeTransientUI(); drawerOpen.value = true }}>☰</button>
        <a class="brand" href="/" title="ディレクトリの選択画面へ">
          reviewer
        </a>
        <ProjectSwitcher name={i.name} root={i.root} />
        {!narrow && <SessionSwitcher />}
        <span class={`round-chip ${i.roundStatus}`}>
          <span class="long" hidden={isCompact.value}>ラウンド {i.round} · {submitted ? '提出済み' : '下書き中'}</span>
          <span class="short" hidden={!isCompact.value} title={submitted ? '提出済み' : '下書き中'}>R{i.round}</span>
        </span>
        <span class="spacer" />
        {menu && <SheetBackdrop onClose={closeTransientUI} />}
        <span ref={actions} id="header-actions" class={`header-actions${menu ? ' open' : ''}`}
          style={{ display: narrow ? undefined : 'contents' }}
          role={menu ? 'dialog' : undefined} aria-modal={menu ? 'true' : undefined}
          aria-label={menu ? 'その他の操作' : undefined} tabIndex={narrow ? -1 : undefined}
          onClick={(e) => {
            if (narrow && e.currentTarget.contains(e.target as Node) && (e.target as Element).closest('button, a')) closeTransientUI()
          }}>
        <a class="btn small settings-link" href="#/settings" title="設定">⚙ 設定</a>
        <GitButton />
        <AnnotateButton />
        <AgentChip />
        {submitted && (
          <button
            class="btn"
            onClick={async () => {
              try {
                await api.openRound()
                await refreshAll()
              } catch (e) {
                toast((e as Error).message, 'error')
              }
            }}
          >
            次のラウンドを開始
          </button>
        )}
        {i.latest && submitted && (
          <button
            class="btn"
            onClick={async () => {
              try {
                await copyText(i.latest!.prompt)
                toast('指示文をコピーしました', 'success')
              } catch {
                toast('コピーできませんでした', 'error')
              }
            }}
          >
            📋 指示文をコピー
          </button>
        )}
        </span>
        <button class="btn icon header-menu-toggle" style={{ display: narrow ? undefined : 'none' }}
          aria-label="その他の操作" aria-controls="header-actions" aria-expanded={menu}
          onClick={() => { closeTransientUI(); headerMenuOpen.value = true }}>⋯</button>
        {!submitted && (
          <button class="btn primary" onClick={() => setDialog('confirm')}>
            レビューを提出
            {draftCount.value > 0 && <span class="badge">{draftCount.value}</span>}
          </button>
        )}
        {connState.value !== 'connected' && (
          <span class={`conn-chip ${connState.value}`} role="status">
            {connState.value === 'connecting' || connState.value === 'syncing' ? '同期中…' :
              connState.value === 'reconnecting' ? '再接続中…' : connState.value === 'unavailable' ? '接続できません' : '認証が必要です'}
          </span>
        )}
      </header>
      <ConnectionError />
      {responseBanner.value !== null && (
        <div class="banner success top">
          エージェントがラウンド{responseBanner.value}に返答しました。各コメントを確認し、解決するか返信してください。
          <span class="spacer" />
          <a href="#/" onClick={dismissResponseBanner}>
            概要を見る
          </a>
          <button class="link" onClick={dismissResponseBanner} aria-label="閉じる">
            ×
          </button>
        </div>
      )}
      {dialog && <SubmitDialog state={dialog} setState={setDialog} />}
    </>
  )
}

function SubmitDialog({
  state,
  setState,
}: {
  state: 'confirm' | SubmitResult
  setState: (s: 'confirm' | SubmitResult | null) => void
}) {
  const [busy, setBusy] = useState(false)
  const [preparing, setPreparing] = useState(state === 'confirm')
  const [saveError, setSaveError] = useState('')
  useEffect(() => {
    if (state !== 'confirm') return
    let active = true
    setPreparing(true)
    void (async () => {
      try {
        const result = await flushComposers()
        await refreshComments()
        if (active && !result.ok) setSaveError('保存できていないコメントがあります。コメント欄で再試行するか、端末に保存してください。')
      } catch (e) {
        if (active) setSaveError(`コメントを確認できませんでした: ${(e as Error).message}`)
      } finally { if (active) setPreparing(false) }
    })()
    return () => { active = false }
  }, [state])
  const ag = agentInfo.value
  const canSend = !!ag?.available && !!ag.binding
  const [send, setSend] = useState(canSend && !!ag?.autoSend)
  const i = info.value!
  const all = comments.value
  const drafts = all.filter((c) => c.status === 'draft')
  const replies = all.reduce((n, c) => n + c.replies.filter((r) => r.draft).length, 0)
  const carried = all.filter((c) => c.round < i.round && c.status !== 'resolved' && c.status !== 'draft')
  const untouched = carried.filter((c) => (c.status === 'addressed' || c.status === 'wontfix') && !c.replies.some((r) => r.draft))
  const empty = drafts.length === 0 && carried.length === 0

  async function submit() {
    if (preparing || saveError) return
    setBusy(true)
    try {
      const res = await api.submit(canSend && send)
      await refreshAll()
      setState(res)
      if (!res.agentRun) copyText(res.prompt).catch(() => {})
    } catch (e) {
      toast((e as Error).message, 'error')
    } finally {
      setBusy(false)
    }
  }

  return (
    <Portal onClose={() => setState(null)}>
    <div class="modal-backdrop" onClick={(e) => e.target === e.currentTarget && setState(null)}>
      <div class="modal" role="dialog" aria-modal="true" aria-label={`ラウンド${i.round}を提出`}>
        {state === 'confirm' ? (
          <>
            <h2>ラウンド{i.round}を提出</h2>
            {preparing ? <p role="status">保存中…</p> : <ul class="submit-summary">
              <li>
                新しいコメント: <b>{drafts.length}</b>件
              </li>
              <li>
                返信: <b>{replies}</b>件
              </li>
              <li>
                前ラウンドから持ち越す未解決コメント: <b>{carried.length}</b>件
              </li>
            </ul>}
            {saveError && <div class="banner error" role="alert">{saveError}</div>}
            {dirtyReplies.value > 0 && <div class="banner warn">保存していない返信が {dirtyReplies.value} 件あります（提出に含まれません）</div>}
            {!preparing && untouched.length > 0 && (
              <div class="banner warn">
                エージェントが「対応済み」「対応しない」と返答したまま、解決も返信もしていないコメントが{untouched.length}
                件あります（{untouched.map((c) => c.id).join(', ')}）。納得できるものは「解決」にすると、持ち越されません。
              </div>
            )}
            {!preparing && empty && <div class="banner warn">提出するコメントがありません。</div>}
            <p class="muted">
              提出すると、この時点のファイル内容を保存し、<code>feedback.md</code> と <code>feedback.json</code> を書き出します。
            </p>
            {canSend && (
              <label class="send-agent">
                <input type="checkbox" checked={send} onChange={(e) => setSend(e.currentTarget.checked)} />
                <span>
                  {ag!.name} に送信する（<b>{ag!.binding!.title}</b>）
                </span>
              </label>
            )}
            {ag?.available && !ag.binding && (
              <p class="muted small">右上の「🤖 {ag.name}」で送信先を選ぶと、提出と同時にエージェントへ送れます。</p>
            )}
            <div class="modal-actions">
              <button class="btn" onClick={() => setState(null)}>
                キャンセル
              </button>
              <button class="btn primary" disabled={busy || preparing || !!saveError || empty} onClick={() => void submit()}>
                {busy ? '提出中…' : '提出する'}
              </button>
            </div>
          </>
        ) : (
          <>
            <h2>ラウンド{state.round}を提出しました</h2>
            {state.agentRun ? (
              <div class="banner success">
                {state.count}件のコメントを {agentInfo.value?.name} に送りました。進捗は概要ページで見られます。
              </div>
            ) : (
              <p>{state.count}件のコメントを書き出しました。次の指示文をエージェントに渡してください。</p>
            )}
            {state.agentError && <div class="banner error">送信できませんでした: {state.agentError}</div>}
            <CopyPrompt prompt={state.prompt} label={state.agentRun ? '指示文をコピー（手動で渡す場合）' : undefined} />
            <dl>
              <dt>feedback.md</dt>
              <dd>
                <code>{state.feedbackPath}</code>
              </dd>
              <dt>feedback.json</dt>
              <dd>
                <code>{state.feedbackJsonPath}</code>
              </dd>
              <dt>返答先</dt>
              <dd>
                <code>{state.responsePath}</code>
              </dd>
            </dl>
            <div class="modal-actions">
              <button class="btn primary" onClick={() => setState(null)}>
                閉じる
              </button>
            </div>
          </>
        )}
      </div>
    </div>
    </Portal>
  )
}
