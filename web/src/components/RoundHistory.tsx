import { useEffect, useMemo, useState } from 'preact/hooks'
import { api, ApiError } from '../api'
import { comments, fileVersion } from '../state'
import { fileHref, roundHref } from '../router'
import type { Comment, RoundChanges, RoundComment, RoundDiff } from '../types'
import { Thread } from './Thread'

const kindText = { modified: '変更', added: '追加', deleted: '削除', unchanged: '変更なし' }

/** Pairs a round's comment positions with the live comment objects. */
function withComments(list: RoundComment[]): { at: RoundComment; c: Comment }[] {
  const byId = new Map(comments.value.map((c) => [c.id, c]))
  return list.flatMap((at) => {
    const c = byId.get(at.id)
    return c ? [{ at, c }] : []
  })
}

function rangeText(c: { round: number; toRound: number }, phase: 'review' | 'agent' = 'agent') {
  if (phase === 'review') return `ラウンド${c.round}の開始 → 提出`
  return c.toRound ? `ラウンド${c.round}の提出 → ラウンド${c.toRound}の開始` : `ラウンド${c.round}の提出 → 現在`
}

/** What changed in response to a round's feedback. */
export function RoundHistory({ round, path, phase = 'agent' }: { round: number; path?: string; phase?: 'review' | 'agent' }) {
  const [changes, setChanges] = useState<RoundChanges | null>(null)
  const [error, setError] = useState<string | null>(null)
  const fv = fileVersion.value.n

  useEffect(() => {
    setError(null)
    api
      .roundChanges(round, phase)
      .then(setChanges)
      .catch((e) => setError(e instanceof ApiError ? e.message : String(e)))
  }, [round, phase, fv])

  if (error) return <div class="empty error">{error}</div>
  if (!changes) return <div class="empty">読み込み中…</div>

  return (
    <div class="round-history">
      <div class="history-side">
        <div class="history-title">
          <a href="#/">← 概要</a>
          <h2>ラウンド{round}の{phase === 'review' ? 'レビュー前後の差分' : 'エージェント修正'}</h2>
          <div class="history-mode-links">
            <a class={`btn small ${phase === 'review' ? 'active' : ''}`} href={roundHref(round, path, 'review')}>レビュー前後</a>
            <a class={`btn small ${phase === 'agent' ? 'active' : ''}`} href={roundHref(round, path, 'agent')}>エージェント修正</a>
          </div>
          <div class="muted small">{rangeText(changes, phase)}</div>
        </div>
        {changes.files.length === 0 && <div class="empty small">変更されたファイルはありません</div>}
        {changes.files.map((f) => (
          <a class={`history-file ${f.path === path ? 'active' : ''}`} href={roundHref(round, f.path, phase)}>
            <span class={`chip kind-${f.kind}`}>{kindText[f.kind]}</span>
            <span class="name">{f.path}</span>
            {f.comments > 0 && <span class="muted small">💬{f.comments}</span>}
            {f.kind !== 'unchanged' && (
              <span class="stat">
                <span class="ins">+{f.insert}</span> <span class="del">−{f.delete}</span>
              </span>
            )}
          </a>
        ))}
      </div>
      <div class="history-main">
        {path ? (
          <DiffPane round={round} path={path} phase={phase} notes={changes.comments.filter((c) => c.path === path)} />
        ) : (
          <RoundSummary changes={changes} />
        )}
      </div>
    </div>
  )
}

function RoundSummary({ changes }: { changes: RoundChanges }) {
  const project = withComments(changes.comments.filter((c) => c.scope === 'project'))
  return (
    <div class="round-summary">
      <h3>ラウンド{changes.round}の全体コメント</h3>
      {project.length === 0 ? (
        <p class="muted">このラウンドの全体コメントはありません。</p>
      ) : (
        project.map(({ c }) => <Thread key={c.id} comment={c} historic />)
      )}
      <p class="muted small">左の一覧からファイルを選ぶと、差分とそのファイルへのコメントを表示します。</p>
    </div>
  )
}

function DiffPane({ round, path, phase, notes }: { round: number; path: string; phase: 'review' | 'agent'; notes: RoundComment[] }) {
  const [diff, setDiff] = useState<RoundDiff | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [context, setContext] = useState(true)
  const fv = fileVersion.value.n

  useEffect(() => {
    setDiff(null)
    setError(null)
    api
      .roundDiff(round, path, phase)
      .then(setDiff)
      .catch((e) => setError(e instanceof ApiError ? e.message : String(e)))
  }, [round, path, phase, fv])

  // Rows with old/new line numbers; unchanged runs far from changes fold.
  const rows = useMemo(() => {
    if (!diff) return []
    type R = { kind: 'eq' | 'ins' | 'del' | 'fold'; oldNo?: number; no?: number; text: string }
    const all: R[] = []
    let no = 1
    let oldNo = 1
    for (const op of diff.ops) {
      for (const text of op.lines) {
        if (op.kind === 'del') all.push({ kind: 'del', oldNo: oldNo++, text })
        else if (op.kind === 'ins') all.push({ kind: 'ins', no: no++, text })
        else all.push({ kind: 'eq', oldNo: oldNo++, no: no++, text })
      }
    }
    if (context) return all
    const keep = new Set<number>()
    const noted = new Set(notes.flatMap((n) => (n.located && n.endLine ? [n.endLine] : [])))
    all.forEach((r, i) => {
      if (r.kind !== 'eq' || (r.oldNo !== undefined && noted.has(r.oldNo))) for (let k = i - 3; k <= i + 3; k++) keep.add(k)
    })
    const out: R[] = []
    let skipped = 0
    all.forEach((r, i) => {
      if (keep.has(i)) {
        if (skipped) out.push({ kind: 'fold', text: `… ${skipped}行 変更なし` })
        skipped = 0
        out.push(r)
      } else skipped++
    })
    if (skipped) out.push({ kind: 'fold', text: `… ${skipped}行 変更なし` })
    return out
  }, [diff, context, notes])

  const threads = withComments(notes)
  const top = threads.filter(({ at }) => at.scope === 'file' || !at.located || !at.endLine)
  const byOldEnd = new Map<number, Comment[]>()
  for (const { at, c } of threads) {
    if (at.scope === 'line' && at.located && at.endLine) byOldEnd.set(at.endLine, [...(byOldEnd.get(at.endLine) ?? []), c])
  }
  const commentedOld = new Set<number>()
  for (const { at } of threads) {
    if (at.scope === 'line' && at.located && at.startLine && at.endLine)
      for (let l = at.startLine; l <= at.endLine; l++) commentedOld.add(l)
  }

  if (error) return <div class="empty error">{error}</div>
  if (!diff) return <div class="empty">読み込み中…</div>
  return (
    <div class="file-view wrap">
      <div class="file-head">
        <span class="file-path" title={path}>{path}</span>
        <span class={`chip kind-${diff.kind}`}>{kindText[diff.kind]}</span>
        <span class="muted small">{rangeText(diff)}</span>
        <span class="spacer" />
        <label class="toggle">
          <input type="checkbox" checked={!context} onChange={(e) => setContext(!e.currentTarget.checked)} /> 変更箇所だけ表示
        </label>
        {diff.kind !== 'deleted' && (
          <a class="btn small" href={fileHref(path)}>
            現在のファイルを開く
          </a>
        )}
      </div>
      {top.length > 0 && (
        <div class="file-comments">
          {top.map(({ c }) => (
            <Thread key={c.id} comment={c} historic />
          ))}
        </div>
      )}
      <div class="code">
        {rows.map((r) =>
          r.kind === 'fold' ? (
            <div class="row fold">
              <span class="ln old" />
              <span class="ln" />
              <span class="text">{r.text}</span>
            </div>
          ) : (
            <>
              <div class={`row ${r.kind} ${r.oldNo && commentedOld.has(r.oldNo) ? 'covered' : ''}`}>
                <span class="ln old">{r.oldNo ?? ''}</span>
                <span class="ln">{r.no ?? ''}</span>
                <span class="sign">{r.kind === 'ins' ? '+' : r.kind === 'del' ? '-' : ''}</span>
                <span class="text">{r.text || ' '}</span>
              </div>
              {r.oldNo !== undefined && byOldEnd.has(r.oldNo) && (
                <div class="inline-threads history">
                  {byOldEnd.get(r.oldNo)!.map((c) => (
                    <Thread key={c.id} comment={c} historic />
                  ))}
                </div>
              )}
            </>
          ),
        )}
      </div>
    </div>
  )
}
