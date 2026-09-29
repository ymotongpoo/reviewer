import { useEffect, useState } from 'preact/hooks'
import { api } from '../api'
import {
  annotationRequests,
  annotations,
  comments,
  editing,
  info,
  refreshAnnotationRequests,
  refreshAnnotations,
  setShowDismissedAnnotations,
  showDismissedAnnotations,
  toast,
} from '../state'
import { statusText, lineRange } from '../labels'
import { fileHref, roundHref } from '../router'
import type { AnnotationRequestView, Comment, RequestDiff } from '../types'
import { Composer } from './Composer'
import { Thread } from './Thread'
import { CopyPrompt } from './Header'
import { AgentPanel, RunView } from './Agent'

type Filter = 'unresolved' | 'draft' | 'all'

export function Overview() {
  const i = info.value
  const [filter, setFilter] = useState<Filter>('unresolved')
  if (!i) return null
  const ed = editing.value
  const newProject = ed?.kind === 'new' && ed.scope === 'project' ? ed : null
  const hiddenId = newProject?.createdId
  const allProject = comments.value.filter((c) => c.scope === 'project' && c.id !== hiddenId)
  const project = allProject.filter((c) => c.status !== 'resolved')
  const resolvedProject = allProject.length - project.length
  const others = comments.value.filter((c) => c.scope !== 'project')
  const shown = others.filter((c) =>
    filter === 'all' ? true : filter === 'draft' ? c.status === 'draft' || c.replies.some((r) => r.draft) : c.status !== 'resolved',
  )
  const byPath = new Map<string, Comment[]>()
  for (const c of shown) byPath.set(c.path!, [...(byPath.get(c.path!) ?? []), c])
  for (const list of byPath.values()) list.sort((a, b) => (a.scope === 'file' ? -1 : (a.loc?.start ?? 0) - (b.loc?.start ?? 0)))

  const latestRound = i.rounds.find((r) => r.round === i.latest?.round)

  return (
    <div class="overview">
      <h1>{i.name}</h1>
      <div class="root-path">{i.root}</div>

      {(i.warnings ?? []).map((w) => (
        <div class="banner warn">⚠ {w}</div>
      ))}

      <section class="card">
        <h2>ラウンド {i.round}</h2>
        {i.roundStatus === 'open' ? (
          <p>
            コメントを書いています。書き終えたら右上の「レビューを提出」を押すと、エージェント用のフィードバックファイルが書き出されます。
          </p>
        ) : (
          <p>提出済みです。エージェントの修正と返答を待っています。新しいコメントや返信を書くと、次のラウンドが自動で始まります。</p>
        )}
        {i.latest && (
          <div class="latest">
            <h3>ラウンド{i.latest.round}のフィードバック</h3>
            <dl>
              <dt>feedback.md</dt>
              <dd>
                <code>{i.latest.feedbackPath}</code>
              </dd>
              <dt>返答先</dt>
              <dd>
                <code>{i.latest.responsePath}</code>
              </dd>
            </dl>
            <CopyPrompt prompt={i.latest.prompt} />
            <ResponseStatus round={i.latest.round} response={latestRound?.response} />
          </div>
        )}
      </section>

      {i.latest && <AgentPanel round={i.latest.round} />}

      <AnnotationRequestsPanel />

      <section class="card">
        <div class="section-head">
          <h2>全体コメント</h2>
          <span class="spacer" />
          {!newProject && (
            <button class="btn small" onClick={() => (editing.value = { kind: 'new', scope: 'project' })}>
              ＋ 全体コメントを追加
            </button>
          )}
        </div>
        {project.length === 0 && !newProject && <p class="muted">未解決の全体コメントはありません。</p>}
        {resolvedProject > 0 && <p class="muted small">解決済みの全体コメント{resolvedProject}件は、ラウンド履歴の「差分を見る」で確認できます。</p>}
        {project.map((c) => (
          <Thread key={c.id} comment={c} />
        ))}
        {newProject && <Composer key="new-project" target={newProject} />}
      </section>

      <section class="card">
        <div class="section-head">
          <h2>ファイルへのコメント</h2>
          <span class="spacer" />
          <div class="seg">
            {(['unresolved', 'draft', 'all'] as Filter[]).map((f) => (
              <button class={filter === f ? 'on' : ''} onClick={() => setFilter(f)}>
                {{ unresolved: '未解決', draft: '下書き', all: 'すべて' }[f]}
              </button>
            ))}
          </div>
        </div>
        {byPath.size === 0 && <p class="muted">該当するコメントはありません。左のファイルを開き、行番号をクリックしてコメントできます。</p>}
        {[...byPath.entries()]
          .sort(([a], [b]) => a.localeCompare(b))
          .map(([path, list]) => (
            <div class="summary-file">
              <a class="summary-path" href={fileHref(path)}>
                {path}
              </a>
              {list.map((c) => (
                <a class={`summary-row status-${c.status}`} href={fileHref(path, c.scope === 'line' ? c.loc?.start : undefined)}>
                  <span class="cid">{c.id}</span>
                  <span class="where">{c.scope === 'file' ? 'ファイル' : lineRange(c.loc?.start, c.loc?.end)}</span>
                  <span class={`chip label-${c.label}`}>{c.label}</span>
                  <span class={`chip status status-${c.status}`}>{statusText[c.status]}</span>
                  {c.loc?.state === 'outdated' && <span class="chip anchor-outdated">位置不明</span>}
                  <span class="snippet">{c.body.split('\n')[0]}</span>
                  {c.replies.length > 0 && <span class="muted">💬 {c.replies.length}</span>}
                </a>
              ))}
            </div>
          ))}
      </section>

      {i.rounds.length > 0 && (
        <section class="card">
          <h2>ラウンド履歴</h2>
          <table class="rounds">
            <thead>
              <tr>
                <th>ラウンド</th>
                <th>開始</th>
                <th>提出</th>
                <th>コメント</th>
                <th>返答</th>
                <th>修正の差分</th>
              </tr>
            </thead>
            <tbody>
              {[...i.rounds].reverse().map((r) => (
                <tr>
                  <td>{r.round}</td>
                  <td>{fmt(r.openedAt)}</td>
                  <td>{r.submittedAt ? fmt(r.submittedAt) : '—'}</td>
                  <td>{r.submittedAt ? r.comments : '—'}</td>
                  <td>{r.response ? (r.response.error ? 'エラー' : `${r.response.count}件`) : r.submittedAt ? '未着' : '—'}</td>
                  <td>{r.submittedAt ? <a href={roundHref(r.round)}>差分を見る</a> : '—'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </section>
      )}
    </div>
  )
}

function AnnotationRequestsPanel() {
  const requests = annotationRequests.value
  return (
    <section class="card annotation-requests" id="annotation-requests">
      <div class="section-head">
        <h2>🔍 AI確認依頼</h2>
        <span class="spacer" />
        <label class="toggle">
          <input
            type="checkbox"
            checked={showDismissedAnnotations.value}
            onChange={(e) => setShowDismissedAnnotations(e.currentTarget.checked)}
          />{' '}
          却下済みも表示
        </label>
      </div>
      {requests.length === 0 && <p class="muted">確認依頼はまだありません。右上の「AIに確認を依頼」から送信できます。</p>}
      {requests.map((request) => (
        <AnnotationRequestCard key={request.id} request={request} />
      ))}
    </section>
  )
}

function AnnotationRequestCard({ request: r }: { request: AnnotationRequestView }) {
  const [diffPath, setDiffPath] = useState<string | null>(null)
  const all = annotations.value.filter((a) => a.request === r.id)
  const count = (state: 'pending' | 'adopted' | 'dismissed') => all.filter((a) => a.state === state).length
  const unlocated = all.filter((a) => a.state === 'pending' && (!a.loc || a.loc.state === 'outdated')).length
  const target =
    r.target === 'new'
      ? '新規セッション'
      : r.target === 'bound'
        ? `バインド済みセッション${r.sessionId ? `（${r.sessionId}）` : ''}`
        : `既存セッション${r.sessionId ? `（${r.sessionId}）` : ''}`

  async function toggleHidden() {
    try {
      await api.updateAnnotationRequest(r.id, !r.hidden)
      await Promise.all([refreshAnnotationRequests(), refreshAnnotations()])
      toast(r.hidden ? `${r.id} を表示しました` : `${r.id} を非表示にしました`, 'success')
    } catch (e) {
      toast((e as Error).message, 'error')
    }
  }

  async function discard() {
    if (!confirm(`${r.id} の未採用AI指摘 ${count('pending')}件をすべて破棄しますか？`)) return
    try {
      await api.discardAnnotationRequest(r.id)
      await refreshAnnotations()
      toast(`${r.id} の未採用AI指摘を破棄しました`, 'success')
    } catch (e) {
      toast((e as Error).message, 'error')
    }
  }

  return (
    <div class={`annotation-request ${r.hidden ? 'hidden-request' : ''}`}>
      <div class="annotation-request-head">
        <span class="cid request-id">{r.id}</span>
        {r.preset && <span class="chip">{r.preset}</span>}
        {r.runs[r.runs.length - 1] && (
          <span class={`chip run-status ${r.runs[r.runs.length - 1].status}`}>
            {r.runs[r.runs.length - 1].status === 'running'
              ? '対応中'
              : r.runs[r.runs.length - 1].status === 'completed'
                ? '完了'
                : r.runs[r.runs.length - 1].status}
          </span>
        )}
        {r.hidden && <span class="chip">非表示</span>}
        <span class="muted small">{fmt(r.createdAt)}</span>
        <span class="spacer" />
        <span class="annotation-counts">
          未採用 {count('pending')} · 採用 {count('adopted')} · 却下 {count('dismissed')}
        </span>
      </div>
      <div class="muted small">送信先: {target}{r.sessionTitle && `（${r.sessionTitle}）`}</div>
      <details class="request-prompt">
        <summary>確認内容</summary>
        <pre>{r.prompt}</pre>
      </details>
      {r.import?.summary && <blockquote class="request-summary">{r.import.summary}</blockquote>}
      {r.import?.error && <div class="banner error">annotations.json を読み込めませんでした: {r.import.error}</div>}
      {(r.import?.warnings ?? []).length > 0 && (
        <details class="warnings">
          <summary>取り込み警告 {r.import!.warnings!.length}件</summary>
          <ul>{r.import!.warnings!.map((w) => <li>{w}</li>)}</ul>
        </details>
      )}
      {unlocated > 0 && <div class="banner warn">位置を特定できないAI指摘が{unlocated}件あります。対象ファイルで確認できます。</div>}
      {(r.changedPaths ?? []).length > 0 && (
        <div class="changed-paths banner warn">
          <strong>⚠ 確認中に対象ファイルが変更されました。</strong>
          <span>AI指摘は依頼時点の内容を基準にしています。</span>
          <div class="changed-links">
            {r.changedPaths!.map((path) => (
              <button class="link" onClick={() => setDiffPath(diffPath === path ? null : path)}>{path} の差分</button>
            ))}
          </div>
        </div>
      )}
      {diffPath && <AnnotationRequestDiff request={r.id} path={diffPath} />}
      {[...r.runs].reverse().map((run, i) => <RunView key={run.id} run={run} defaultOpen={i === 0 && run.status === 'running'} />)}
      <div class="annotation-request-actions">
        <button class="btn small" onClick={() => void toggleHidden()}>{r.hidden ? '表示する' : '非表示にする'}</button>
        <button class="btn small danger-text" disabled={count('pending') === 0} onClick={() => void discard()}>未採用を破棄</button>
      </div>
    </div>
  )
}

function AnnotationRequestDiff({ request, path }: { request: string; path: string }) {
  const [diff, setDiff] = useState<RequestDiff | null>(null)
  const [error, setError] = useState<string | null>(null)
  useEffect(() => {
    setDiff(null)
    setError(null)
    api.annotationRequestDiff(request, path).then(setDiff).catch((e) => setError((e as Error).message))
  }, [request, path])
  if (error) return <div class="banner error">差分を読み込めませんでした: {error}</div>
  if (!diff) return <div class="empty small">差分を読み込み中…</div>
  let oldNo = 1
  let no = 1
  return (
    <div class="request-diff">
      <div class="request-diff-head"><code>{path}</code><span class={`chip kind-${diff.kind}`}>{diff.kind}</span></div>
      <div class="code">
        {diff.ops.flatMap((op) => op.lines.map((text) => {
          const old = op.kind === 'ins' ? undefined : oldNo++
          const next = op.kind === 'del' ? undefined : no++
          return (
            <div class={`row ${op.kind}`}>
              <span class="ln old">{old ?? ''}</span>
              <span class="ln">{next ?? ''}</span>
              <span class="sign">{op.kind === 'ins' ? '+' : op.kind === 'del' ? '-' : ''}</span>
              <span class="text">{text || ' '}</span>
            </div>
          )
        }))}
      </div>
    </div>
  )
}

function ResponseStatus({ round, response }: { round: number; response?: import('../types').ResponseInfo }) {
  if (!response) return <p class="muted">⏳ エージェントの返答（response.json）はまだありません。</p>
  if (response.error)
    return (
      <div class="banner error">
        response.json を読み込めませんでした: {response.error}
      </div>
    )
  return (
    <div class="response">
      <p>
        ✅ ラウンド{round}の返答を取り込みました（{response.count}件、{fmt(response.importedAt)}）
      </p>
      {response.summary && <blockquote class="summary">{response.summary}</blockquote>}
      {(response.warnings ?? []).length > 0 && (
        <details class="warnings">
          <summary>警告 {response.warnings!.length}件</summary>
          <ul>
            {response.warnings!.map((w) => (
              <li>{w}</li>
            ))}
          </ul>
        </details>
      )}
    </div>
  )
}

export function fmt(s: string): string {
  const d = new Date(s)
  return d.toLocaleString('ja-JP', { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}
