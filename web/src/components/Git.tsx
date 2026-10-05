import { Portal } from './Portal'
import { useEffect, useState } from 'preact/hooks'
import { api, ApiError } from '../api'
import { copyText } from '../clipboard'
import { gitStatus, gitVersion, info, refreshGit, toast } from '../state'
import type { GitChange, GitCommitResult, GitHint, GitLanguage, GitPushResult, GitRemote, GitStatus } from '../types'

const kindText: Record<GitChange['kind'], string> = {
  added: '追加',
  modified: '変更',
  deleted: '削除',
  renamed: '名前変更',
  typechange: '種類変更',
  conflict: 'コンフリクト',
}

const languageText: Record<GitLanguage, string> = { ja: '日本語', en: 'English' }

/** Mirrors gitops.ValidateMessage so that the dialog can explain before sending. */
const headerRe = /^[a-z][a-z0-9-]*(\([^()\r\n]+\))?!?: \S/

function messageProblem(message: string): string | null {
  const m = message.replace(/\r\n/g, '\n').trim()
  if (!m) return 'コミットメッセージを入力してください'
  const [header, ...rest] = m.split('\n')
  if (!headerRe.test(header)) return '1行目を Conventional Commits の形式（例: docs(ch1): 導入を短くする）にしてください'
  if (rest.length > 0 && rest[0] !== '') return '1行目と本文の間に空行を入れてください'
  return null
}

/** Paths that often hold secrets; they are only flagged, never hidden. */
const sensitiveRe =
  /(^|\/)(\.env(\..*)?|\.netrc|\.npmrc|\.pypirc|id_(rsa|dsa|ecdsa|ed25519)|credentials(\.\w+)?|.*secret.*|.*\.(pem|key|p12|pfx|kdbx))$/i

const short = (sha?: string) => (sha ?? '').slice(0, 7)

/** Header button showing uncommitted changes and commits to push. */
export function GitButton() {
  const [open, setOpen] = useState(false)
  if (!info.value?.git) return null
  const st = gitStatus.value
  const changes = st?.changes.length ?? 0
  const ahead = st && st.target.remote && (st.target.ahead > 0 || (!st.target.tracked && !st.unborn)) ? st.target.ahead : 0
  const title = st
    ? `${st.branch || 'detached HEAD'} · 未コミット ${changes}件${st.target.remote ? ` · ${st.target.remote}/${st.target.branch} より ${st.target.ahead}件先行` : ''}`
    : 'Git'
  return (
    <>
      <button class="btn git-button" title={title} onClick={() => setOpen(true)}>
        ⎇ {st?.branch || 'Git'}
        {changes > 0 && <span class="git-count changes">●{changes}</span>}
        {ahead > 0 && <span class="git-count ahead">↑{ahead}</span>}
      </button>
      {open && <GitDialog onClose={() => setOpen(false)} />}
    </>
  )
}

type Step =
  | { kind: 'edit' }
  | { kind: 'confirmCommit'; snap: GitStatus; message: string; thenPush: boolean }
  | { kind: 'committed'; commit: GitCommitResult }
  | { kind: 'confirmPush'; snap: GitStatus; commit?: GitCommitResult }
  | { kind: 'pushed'; result: GitPushResult; snap: GitStatus; commit?: GitCommitResult }

function GitDialog({ onClose }: { onClose: () => void }) {
  const [st, setSt] = useState<GitStatus | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  /** One-off push target; empty fields use the saved defaults. */
  const [pick, setPick] = useState({ remote: '', branch: '' })
  const [lang, setLang] = useState<GitLanguage | null>(null)
  const [message, setMessage] = useState('')
  const [edited, setEdited] = useState(false)
  const [step, setStep] = useState<Step>({ kind: 'edit' })
  const [busy, setBusy] = useState<'' | 'load' | 'commit' | 'push' | 'save'>('load')

  async function load(remote = pick.remote, branch = pick.branch): Promise<GitStatus | null> {
    setBusy((b) => b || 'load')
    try {
      const next = await api.gitStatus(remote, branch)
      setSt(next)
      setLoadError(null)
      return next
    } catch (e) {
      // A one-off target may have disappeared (e.g. the remote was removed).
      if (e instanceof ApiError && e.status === 400 && (remote || branch)) {
        toast(`${e.message}。既定の push 先に戻します`, 'error')
        setPick({ remote: '', branch: '' })
        return load('', '')
      }
      setLoadError((e as Error).message)
      return null
    } finally {
      setBusy((b) => (b === 'load' ? '' : b))
    }
  }

  // Refetch while editing when files or the repository change.
  useEffect(() => {
    if (step.kind === 'edit' || !st) void load()
  }, [gitVersion.value])

  const language: GitLanguage = lang ?? st?.settings.language ?? 'ja'
  const generated = st?.messages[language] ?? ''
  // Follow the generated message until the user edits it.
  useEffect(() => {
    if (!edited) setMessage(generated)
  }, [generated, edited])

  function chooseLanguage(l: GitLanguage) {
    if (l === language) return
    if (edited && message.trim() !== generated.trim() && !confirm('編集したメッセージを破棄して、選んだ言語で生成し直しますか？')) return
    setLang(l)
    setEdited(false)
    setMessage(st?.messages[l] ?? '')
  }

  function chooseTarget(remote: string, branch: string) {
    setPick({ remote, branch })
    void load(remote, branch)
  }

  async function saveDefaults(remote: string, branch: string) {
    setBusy('save')
    try {
      await api.saveGitSettings({ language, remote, branch })
      setLang(null)
      setPick({ remote: '', branch: '' })
      await load('', '')
      toast('Git の既定の設定を保存しました', 'success')
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy('')
    }
  }

  async function commit(s: Extract<Step, { kind: 'confirmCommit' }>) {
    setBusy('commit')
    setError(null)
    try {
      const res = await api.gitCommit(s.message, s.snap.fingerprint)
      setEdited(false)
      void refreshGit().catch(() => {})
      const next = await load()
      if (s.thenPush && next && next.pushBlockers.length === 0) setStep({ kind: 'confirmPush', snap: next, commit: res })
      else setStep({ kind: 'committed', commit: res })
    } catch (e) {
      setError((e as Error).message)
      // The changes moved on: show them again before another attempt.
      if (e instanceof ApiError && e.status === 409) {
        setStep({ kind: 'edit' })
        void load()
      }
    } finally {
      setBusy('')
    }
  }

  async function push(snap: GitStatus, commit?: GitCommitResult) {
    setBusy('push')
    setError(null)
    try {
      const result = await api.gitPush(snap.target.remote, snap.target.branch)
      setStep({ kind: 'pushed', result, snap, commit })
      void refreshGit().catch(() => {})
      void load()
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy('')
    }
  }

  const working = busy === 'commit' || busy === 'push'
  return (
    <Portal onClose={() => { if (!working) onClose() }}>
    <div class="modal-backdrop" onClick={(e) => e.target === e.currentTarget && !working && onClose()}>
      <div class="modal git-modal" role="dialog" aria-modal="true" aria-labelledby="git-title">
        {loadError && <div class="banner error">{loadError}</div>}
        {error && <div class="banner error">{error}</div>}
        {!st ? (
          <>
            <h2 id="git-title">Git</h2>
            {!loadError && <div class="empty small">読み込み中…</div>}
            <div class="modal-actions">
              <button class="btn" onClick={onClose}>
                閉じる
              </button>
            </div>
          </>
        ) : step.kind === 'edit' ? (
          <EditStep
            st={st}
            busy={busy}
            language={language}
            message={message}
            onLanguage={chooseLanguage}
            onMessage={(m) => {
              setMessage(m)
              setEdited(true)
            }}
            onRegenerate={() => {
              setEdited(false)
              setMessage(generated)
            }}
            onTarget={chooseTarget}
            onSaveDefaults={saveDefaults}
            onCommit={(thenPush) => {
              setError(null)
              setStep({ kind: 'confirmCommit', snap: st, message, thenPush })
            }}
            onPush={() => {
              setError(null)
              setStep({ kind: 'confirmPush', snap: st })
            }}
            onClose={onClose}
            oneOff={!!(pick.remote || pick.branch) || (lang !== null && lang !== st.settings.language)}
          />
        ) : step.kind === 'confirmCommit' ? (
          <ConfirmCommit
            step={step}
            busy={busy === 'commit'}
            onThenPush={(thenPush) => setStep({ ...step, thenPush })}
            onBack={() => setStep({ kind: 'edit' })}
            onCommit={() => void commit(step)}
          />
        ) : step.kind === 'committed' ? (
          <>
            <h2 id="git-title">コミットしました</h2>
            <CommitDone commit={step.commit} />
            {st.pushBlockers.length > 0 && <Blockers title="push できません" items={st.pushBlockers} />}
            <div class="modal-actions">
              <button class="btn" onClick={() => setStep({ kind: 'edit' })}>
                Git の画面に戻る
              </button>
              <span class="spacer" />
              <button class="btn" onClick={onClose}>
                閉じる
              </button>
              <button
                class="btn primary"
                disabled={st.pushBlockers.length > 0}
                onClick={() => setStep({ kind: 'confirmPush', snap: st, commit: step.commit })}
              >
                push…
              </button>
            </div>
          </>
        ) : step.kind === 'confirmPush' ? (
          <ConfirmPush
            snap={step.snap}
            commit={step.commit}
            busy={busy === 'push'}
            onBack={() => setStep(step.commit ? { kind: 'committed', commit: step.commit } : { kind: 'edit' })}
            onPush={() => void push(step.snap, step.commit)}
          />
        ) : (
          <PushOutcome
            step={step}
            busy={busy === 'push'}
            onRetry={() => void push(step.snap, step.commit)}
            onEdit={() => setStep({ kind: 'edit' })}
            onClose={onClose}
          />
        )}
      </div>
    </div>
    </Portal>
  )
}

function EditStep({
  st,
  busy,
  language,
  message,
  onLanguage,
  onMessage,
  onRegenerate,
  onTarget,
  onSaveDefaults,
  onCommit,
  onPush,
  onClose,
  oneOff,
}: {
  st: GitStatus
  oneOff: boolean
  busy: string
  language: GitLanguage
  message: string
  onLanguage: (l: GitLanguage) => void
  onMessage: (m: string) => void
  onRegenerate: () => void
  onTarget: (remote: string, branch: string) => void
  onSaveDefaults: (remote: string, branch: string) => void
  onCommit: (thenPush: boolean) => void
  onPush: () => void
  onClose: () => void
}) {
  const problem = messageProblem(message)
  const subject = message.trim().split('\n')[0] ?? ''
  const canCommit = st.repo && st.changes.length > 0 && st.commitBlockers.length === 0 && !problem
  const t = st.target
  const canPush = st.pushBlockers.length === 0 && (t.ahead > 0 || !t.tracked)
  const s = st.settings

  return (
    <>
      <div class="git-head">
        <h2 id="git-title">Git</h2>
        {st.repo && (
          <span class="muted small">
            ブランチ <code>{st.branch || 'detached HEAD'}</code>
            {st.head && (
              <>
                {' '}
                · HEAD <code>{short(st.head)}</code>
              </>
            )}
            {st.unborn && ' · まだコミットがありません'}
          </span>
        )}
        <span class="spacer" />
        {busy === 'load' && <span class="spinner" />}
      </div>
      {st.repo && st.prefix && (
        <p class="muted small">
          リポジトリ <code>{st.toplevel}</code> のうち <code>{st.prefix}</code> の変更だけを扱います。データディレクトリとプロジェクトの外の変更はコミットしません。
        </p>
      )}

      <section class="git-section">
        <h3>
          未コミットの変更 <span class="muted">{st.changes.length}件</span>
        </h3>
        {st.changes.length === 0 ? (
          <p class="muted small">コミットする変更はありません。</p>
        ) : (
          <ul class="git-changes">
            {st.changes.map((c) => (
              <li class={`git-change kind-${c.kind}`}>
                <span class={`chip git-kind kind-${c.kind}`}>{kindText[c.kind] ?? c.kind}</span>
                <span class="git-path">
                  {c.from && (
                    <>
                      <span class="muted">{c.from}</span> →{' '}
                    </>
                  )}
                  {c.path}
                </span>
                {c.untracked && <span class="muted small">新規</span>}
                {c.staged && <span class="muted small">ステージ済み</span>}
                {sensitiveRe.test(c.path) && (
                  <span class="git-sensitive" title="秘密情報を含むことが多いファイル名です。コミットしてよいか確かめてください">
                    ⚠ 秘密情報?
                  </span>
                )}
              </li>
            ))}
          </ul>
        )}
        {st.commitBlockers.length > 0 && <Blockers title="コミットできません" items={st.commitBlockers} />}
      </section>

      {st.changes.length > 0 && (
        <section class="git-section">
          <div class="git-message-head">
            <h3>コミットメッセージ</h3>
            <span class="spacer" />
            <span class="muted small">自動生成の言語</span>
            <div class="seg" role="group" aria-label="コミットメッセージの言語">
              {(['ja', 'en'] as const).map((l) => (
                <button class={l === language ? 'on' : ''} aria-pressed={l === language} onClick={() => onLanguage(l)}>
                  {languageText[l]}
                </button>
              ))}
            </div>
            <button class="btn small" title="変更内容からメッセージを生成し直す" onClick={onRegenerate}>
              生成し直す
            </button>
          </div>
          <textarea
            class="git-message"
            rows={Math.min(12, Math.max(4, message.split('\n').length + 1))}
            value={message}
            spellcheck={false}
            onInput={(e) => onMessage(e.currentTarget.value)}
          />
          {problem ? (
            <div class="muted small danger-text">{problem}</div>
          ) : (
            [...subject].length > 72 && <div class="muted small">1行目が{[...subject].length}文字あります。72文字以内が目安です。</div>
          )}
        </section>
      )}

      <section class="git-section">
        <h3>push 先</h3>
        {st.remotes.length > 0 ? (
          <TargetPicker st={st} onTarget={onTarget} />
        ) : (
          <p class="muted small">remote がありません。</p>
        )}
        {st.remotes.length > 0 && <TargetSummary st={st} />}
        {st.pushBlockers.length > 0 && <Blockers title="push できません" items={st.pushBlockers} />}
        {st.remotes.length > 0 && (
          <div class="git-defaults">
            {oneOff ? (
              <>
                <span class="muted small">選んだ言語と push 先は今回だけ使います。</span>
                <button class="btn small" disabled={busy === 'save' || !t.remote} onClick={() => onSaveDefaults(t.remote, t.branch)}>
                  既定として保存
                </button>
              </>
            ) : (
              <>
                <span class="muted small">
                  既定: {languageText[s.language]}、push 先{' '}
                  {s.remote ? (
                    <code>
                      {s.remote}/{s.branch || '現在のブランチ'}
                    </code>
                  ) : (
                    '自動（upstream、なければ origin と現在のブランチ）'
                  )}
                </span>
                {!s.remote && t.remote && t.branch && (
                  <button class="btn small" disabled={busy === 'save'} onClick={() => onSaveDefaults(t.remote, t.branch)}>
                    この push 先を既定にする
                  </button>
                )}
              </>
            )}
            {(s.remote || s.branch) && (
              <button class="link small" disabled={busy === 'save'} onClick={() => onSaveDefaults('', '')}>
                既定の push 先を自動に戻す
              </button>
            )}
          </div>
        )}
      </section>

      <div class="modal-actions">
        <button class="btn" onClick={onClose}>
          閉じる
        </button>
        <span class="spacer" />
        <button class="btn" disabled={!canPush} onClick={onPush} title={pushTitle(st, canPush)}>
          push…
        </button>
        <button class="btn primary" disabled={!canCommit} onClick={() => onCommit(pushAfterCommit(st))}>
          コミット…
        </button>
      </div>
    </>
  )
}

/** Whether a push can follow the commit; before the first commit the only push blocker is the missing commit. */
function pushAfterCommit(st: GitStatus) {
  return st.pushBlockers.length === 0 || (st.unborn && st.pushBlockers.length === 1 && !!st.target.remote && !!st.target.branch)
}

function pushTitle(st: GitStatus, canPush: boolean) {
  if (st.pushBlockers.length > 0) return st.pushBlockers[0]
  if (!canPush) return 'push するコミットがありません'
  return `HEAD を ${st.target.remote}/${st.target.branch} に push します`
}

function TargetPicker({ st, onTarget }: { st: GitStatus; onTarget: (remote: string, branch: string) => void }) {
  const t = st.target
  const remote = st.remotes.find((r) => r.name === t.remote)
  const branches = [...new Set([st.branch, ...(remote?.branches ?? [])].filter(Boolean))]
  return (
    <div class="git-target">
      <label class="field">
        remote
        <select value={t.remote} onChange={(e) => onTarget(e.currentTarget.value, '')}>
          {st.remotes.map((r) => (
            <option value={r.name}>
              {r.name}
              {r.mirror ? '（mirror）' : ''}
              {r.url ? ` — ${r.url}` : ''}
            </option>
          ))}
        </select>
      </label>
      <label class="field">
        ブランチ
        <select value={t.branch} onChange={(e) => onTarget(t.remote, e.currentTarget.value)}>
          {!t.branch && <option value="">選んでください</option>}
          {branches.map((b) => (
            <option value={b}>
              {b}
              {b === st.branch ? '（現在のブランチ）' : ''}
            </option>
          ))}
        </select>
      </label>
    </div>
  )
}

function TargetSummary({ st }: { st: GitStatus }) {
  const t = st.target
  if (!t.remote || !t.branch || st.unborn) return null
  if (!t.tracked)
    return (
      <p class="muted small">
        <code>
          {t.remote}/{t.branch}
        </code>{' '}
        はまだ取得していないか、リモートにないブランチです。push すると {t.ahead}件のコミットを送ります。
      </p>
    )
  return (
    <p class={`small ${t.behind > 0 ? 'danger-text' : 'muted'}`}>
      <code>
        {t.remote}/{t.branch}
      </code>{' '}
      より {t.ahead}件先行
      {t.behind > 0
        ? `、${t.behind}件遅れています。このままでは push は拒否されます（reviewer は force push、pull、merge をしません）。`
        : t.ahead === 0
          ? '（push するコミットはありません）'
          : ''}
    </p>
  )
}

function Blockers({ title, items }: { title: string; items: string[] }) {
  return (
    <div class="banner warn git-blockers">
      <div>
        <b>{title}</b>
        <ul>
          {items.map((b) => (
            <li>{b}</li>
          ))}
        </ul>
      </div>
    </div>
  )
}

function ConfirmCommit({
  step,
  busy,
  onThenPush,
  onBack,
  onCommit,
}: {
  step: Extract<Step, { kind: 'confirmCommit' }>
  busy: boolean
  onThenPush: (v: boolean) => void
  onBack: () => void
  onCommit: () => void
}) {
  const { snap, message } = step
  const t = snap.target
  return (
    <>
      <h2 id="git-title">コミットの確認</h2>
      <p>
        次の{snap.changes.length}件の変更をすべてステージし、ブランチ <code>{snap.branch}</code> にコミットします。
      </p>
      <ul class="git-changes compact">
        {snap.changes.map((c) => (
          <li class="git-change">
            <span class={`chip git-kind kind-${c.kind}`}>{kindText[c.kind] ?? c.kind}</span>
            <span class="git-path">{c.from ? `${c.from} → ${c.path}` : c.path}</span>
            {sensitiveRe.test(c.path) && <span class="git-sensitive">⚠ 秘密情報?</span>}
          </li>
        ))}
      </ul>
      <pre class="git-message-preview">{message.trim()}</pre>
      {snap.identity && <p class="muted small">コミットする人: {snap.identity}</p>}
      {pushAfterCommit(snap) && (
        <label class="send-agent">
          <input type="checkbox" checked={step.thenPush} onChange={(e) => onThenPush(e.currentTarget.checked)} />
          <span>
            コミットしたら、続けて{' '}
            <code>
              {t.remote}/{t.branch}
            </code>{' '}
            への push を確認する
          </span>
        </label>
      )}
      <div class="modal-actions">
        <button class="btn" disabled={busy} onClick={onBack}>
          戻る
        </button>
        <button class="btn primary" disabled={busy} onClick={onCommit}>
          {busy ? 'コミット中…' : 'コミットする'}
        </button>
      </div>
    </>
  )
}

function CommitDone({ commit }: { commit: GitCommitResult }) {
  return (
    <div class="banner success">
      <span>
        <code>{short(commit.commit)}</code> {commit.subject}（{commit.branch}、{commit.files}件）
      </span>
    </div>
  )
}

function ConfirmPush({
  snap,
  commit,
  busy,
  onBack,
  onPush,
}: {
  snap: GitStatus
  commit?: GitCommitResult
  busy: boolean
  onBack: () => void
  onPush: () => void
}) {
  const t = snap.target
  const remote: GitRemote | undefined = snap.remotes.find((r) => r.name === t.remote)
  return (
    <>
      <h2 id="git-title">push の確認</h2>
      {commit && <CommitDone commit={commit} />}
      <dl class="git-push-summary">
        <dt>remote</dt>
        <dd>
          <code>{t.remote}</code> {remote?.url && <span class="muted small">{remote.url}</span>}
        </dd>
        <dt>送るもの</dt>
        <dd>
          <code>HEAD</code>（{snap.branch}、<code>{short(snap.head)}</code>）→ <code>refs/heads/{t.branch}</code>
        </dd>
        <dt>コミット</dt>
        <dd>
          {t.tracked ? `${t.ahead}件` : `${t.ahead}件（リモートのブランチを取得していないため、新しいブランチとして送る可能性があります）`}
        </dd>
      </dl>
      {t.branch !== snap.branch && (
        <div class="banner warn">
          現在のブランチ <code>{snap.branch}</code> とは別のブランチ <code>{t.branch}</code> に push します。
        </div>
      )}
      {!commit && snap.changes.length > 0 && (
        <p class="muted small">未コミットの変更が{snap.changes.length}件ありますが、push するのはコミット済みの HEAD だけです。</p>
      )}
      {t.tracked && t.behind > 0 && (
        <div class="banner warn">
          リモートに手元にないコミットが{t.behind}件あるため、push は拒否される見込みです。reviewer は force push をしません。
        </div>
      )}
      <p class="muted small">
        force push はしません。認証には ssh-agent に登録済みの鍵か、Git の credential helper に保存済みの認証情報だけを使い、パスワードやトークンの入力は求めません。
      </p>
      <div class="modal-actions">
        <button class="btn" disabled={busy} onClick={onBack}>
          戻る
        </button>
        <button class="btn primary" disabled={busy} onClick={onPush}>
          {busy ? (
            <>
              <span class="spinner" /> push 中…
            </>
          ) : (
            'push する'
          )}
        </button>
      </div>
    </>
  )
}

function PushOutcome({
  step,
  busy,
  onRetry,
  onEdit,
  onClose,
}: {
  step: Extract<Step, { kind: 'pushed' }>
  busy: boolean
  onRetry: () => void
  onEdit: () => void
  onClose: () => void
}) {
  const r = step.result
  const f = r.failure
  return (
    <>
      <h2 id="git-title">{r.ok ? 'push しました' : 'push できませんでした'}</h2>
      {step.commit && <CommitDone commit={step.commit} />}
      {r.ok ? (
        <>
          <div class="banner success">
            <span>
              <code>{short(r.commit)}</code> を{' '}
              <code>
                {r.remote}/{r.branch}
              </code>{' '}
              に push しました。
            </span>
          </div>
          {r.messages && (
            <div>
              <div class="muted small">リモートからのメッセージ</div>
              <pre class="git-detail">
                <Linkified text={r.messages} />
              </pre>
            </div>
          )}
        </>
      ) : (
        f && (
          <>
            {step.commit && <p class="small">コミットは作成済みで、手元に残っています。push だけが失敗しました。</p>}
            <div class="banner error">{f.message}</div>
            <ol class="git-hints">
              {f.hints.map((h) => (
                <HintItem hint={h} />
              ))}
            </ol>
            {f.detail && (
              <details class="git-detail-box">
                <summary>Git の出力</summary>
                <pre class="git-detail">{f.detail}</pre>
              </details>
            )}
          </>
        )
      )}
      <div class="modal-actions">
        {!r.ok && (
          <button class="btn" disabled={busy} onClick={onEdit}>
            push 先を変える
          </button>
        )}
        <span class="spacer" />
        <button class="btn" disabled={busy} onClick={onClose}>
          閉じる
        </button>
        {!r.ok && (
          <button class="btn primary" disabled={busy} onClick={onRetry}>
            {busy ? (
              <>
                <span class="spinner" /> push 中…
              </>
            ) : (
              '再試行'
            )}
          </button>
        )}
      </div>
    </>
  )
}

function HintItem({ hint }: { hint: GitHint }) {
  const [done, setDone] = useState(false)
  const commands = hint.commands ?? []
  return (
    <li>
      <div>{hint.text}</div>
      {commands.length > 0 && (
        <div class="git-hint-commands">
          <pre>{commands.join('\n')}</pre>
          <button
            class={`btn small ${done ? 'success' : ''}`}
            onClick={async () => {
              try {
                await copyText(commands.join('\n'))
                setDone(true)
                setTimeout(() => setDone(false), 2000)
              } catch {
                toast('コピーできませんでした。テキストを選択してコピーしてください', 'error')
              }
            }}
          >
            {done ? '✓ コピーしました' : '📋 コピー'}
          </button>
        </div>
      )}
    </li>
  )
}

/** Renders text with http(s) URLs as links, e.g. a pull request link from the remote. */
function Linkified({ text }: { text: string }) {
  const parts = text.split(/(https?:\/\/[^\s<>"']+)/g)
  return (
    <>
      {parts.map((p, i) =>
        i % 2 === 1 ? (
          <a href={p} target="_blank" rel="noopener noreferrer">
            {p}
          </a>
        ) : (
          p
        ),
      )}
    </>
  )
}
