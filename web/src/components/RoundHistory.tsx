import { useEffect, useMemo, useState } from 'preact/hooks'
import { api, ApiError } from '../api'
import { fileVersion } from '../state'
import { fileHref, roundHref } from '../router'
import type { RoundChanges, RoundDiff } from '../types'

const kindText = { modified: '変更', added: '追加', deleted: '削除' }

function rangeText(c: { round: number; toRound: number }) {
  return c.toRound ? `ラウンド${c.round}の提出 → ラウンド${c.toRound}の開始` : `ラウンド${c.round}の提出 → 現在`
}

/** What changed in response to a round's feedback. */
export function RoundHistory({ round, path }: { round: number; path?: string }) {
  const [changes, setChanges] = useState<RoundChanges | null>(null)
  const [error, setError] = useState<string | null>(null)
  const fv = fileVersion.value.n

  useEffect(() => {
    setError(null)
    api
      .roundChanges(round)
      .then(setChanges)
      .catch((e) => setError(e instanceof ApiError ? e.message : String(e)))
  }, [round, fv])

  if (error) return <div class="empty error">{error}</div>
  if (!changes) return <div class="empty">読み込み中…</div>

  return (
    <div class="round-history">
      <div class="history-side">
        <div class="history-title">
          <a href="#/">← 概要</a>
          <h2>ラウンド{round}の修正</h2>
          <div class="muted small">{rangeText(changes)}</div>
        </div>
        {changes.files.length === 0 && <div class="empty small">変更されたファイルはありません</div>}
        {changes.files.map((f) => (
          <a class={`history-file ${f.path === path ? 'active' : ''}`} href={roundHref(round, f.path)}>
            <span class={`chip kind-${f.kind}`}>{kindText[f.kind]}</span>
            <span class="name">{f.path}</span>
            <span class="stat">
              <span class="ins">+{f.insert}</span> <span class="del">−{f.delete}</span>
            </span>
          </a>
        ))}
      </div>
      <div class="history-main">
        {path ? (
          <DiffPane round={round} path={path} />
        ) : (
          <div class="empty">左の一覧からファイルを選ぶと差分を表示します。</div>
        )}
      </div>
    </div>
  )
}

function DiffPane({ round, path }: { round: number; path: string }) {
  const [diff, setDiff] = useState<RoundDiff | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [context, setContext] = useState(true)
  const fv = fileVersion.value.n

  useEffect(() => {
    setDiff(null)
    setError(null)
    api
      .roundDiff(round, path)
      .then(setDiff)
      .catch((e) => setError(e instanceof ApiError ? e.message : String(e)))
  }, [round, path, fv])

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
    all.forEach((r, i) => {
      if (r.kind !== 'eq') for (let k = i - 3; k <= i + 3; k++) keep.add(k)
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
  }, [diff, context])

  if (error) return <div class="empty error">{error}</div>
  if (!diff) return <div class="empty">読み込み中…</div>
  return (
    <div class="file-view wrap">
      <div class="file-head">
        <span class="file-path">{path}</span>
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
      <div class="code">
        {rows.map((r) =>
          r.kind === 'fold' ? (
            <div class="row fold">
              <span class="ln old" />
              <span class="ln" />
              <span class="text">{r.text}</span>
            </div>
          ) : (
            <div class={`row ${r.kind}`}>
              <span class="ln old">{r.oldNo ?? ''}</span>
              <span class="ln">{r.no ?? ''}</span>
              <span class="sign">{r.kind === 'ins' ? '+' : r.kind === 'del' ? '-' : ''}</span>
              <span class="text">{r.text || ' '}</span>
            </div>
          ),
        )}
      </div>
    </div>
  )
}
