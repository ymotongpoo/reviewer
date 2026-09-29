import { useState } from 'preact/hooks'
import { comments, editing, info } from '../state'
import { statusText, lineRange } from '../labels'
import { fileHref, roundHref } from '../router'
import type { Comment } from '../types'
import { Composer } from './Composer'
import { Thread } from './Thread'
import { CopyPrompt } from './Header'
import { AgentPanel } from './Agent'

type Filter = 'unresolved' | 'draft' | 'all'

export function Overview() {
  const i = info.value
  const [filter, setFilter] = useState<Filter>('unresolved')
  if (!i) return null
  const ed = editing.value
  const newProject = ed?.kind === 'new' && ed.scope === 'project' ? ed : null
  const hiddenId = newProject?.createdId
  const project = comments.value.filter((c) => c.scope === 'project' && c.id !== hiddenId)
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
        {project.length === 0 && !newProject && <p class="muted">プロジェクト全体に対するコメントはまだありません。</p>}
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
