import type { DiffLine, Format } from '../editbuffer'

export interface EditError {
  message: string
  conflict: boolean
}

/** Status of the in-place edit, shown under the file header while a draft exists. */
export function EditBar({
  mode,
  changed,
  deleted,
  dirty,
  saving,
  conflict,
  error,
  unknown,
  onCheckServer,
  onReview,
  onDiscard,
  onShowExternal,
  onReload,
  onCopyDraft,
  onDismissError,
}: {
  mode: 'normal' | 'insert'
  changed: number
  deleted: number
  dirty: boolean
  saving: boolean
  conflict: boolean
  error: EditError | null
  unknown: boolean
  onCheckServer: () => void
  onReview: () => void
  onDiscard: () => void
  onShowExternal: () => void
  onReload: () => void
  onCopyDraft: () => void
  onDismissError: () => void
}) {
  return (
    <div class="edit-bar-wrap">
      <div class="edit-bar">
        <span class={`edit-mode ${mode}`}>{mode === 'insert' ? '-- INSERT --' : '-- NORMAL --'}</span>
        <span class="edit-count">
          {dirty ? `${changed}行を変更・追加${deleted ? `、${deleted}行を削除` : ''}` : '変更なし'}
        </span>
        <span class="muted small edit-hint">
          {mode === 'insert' ? 'Escでノーマル・Ctrl+Sで差分' : 'iか行のダブルクリックで挿入'}
        </span>
        <span class="spacer" />
        <button class="btn small primary" disabled={!dirty || saving} onClick={onReview}>
          差分を確認
        </button>
        <button class="btn small" disabled={saving} onClick={onDiscard}>
          破棄
        </button>
      </div>
      {conflict && (
        <div class="edit-banner warn" role="alert">
          <span>編集中にディスク上のファイルが外部で変更されました。このまま保存すると競合として拒否されます。</span>
          <span class="spacer" />
          <button class="btn small" onClick={onShowExternal}>外部変更を見る</button>
          <button class="btn small" onClick={onCopyDraft}>下書きをコピー</button>
          <button class="btn small danger" disabled={saving} onClick={onReload}>破棄して再読み込み</button>
        </div>
      )}
      {error && (
        <div class="edit-banner error" role="alert">
          <span>{unknown ? '保存結果を確認できません。サーバーの内容を確認してください' : `保存できませんでした: ${error.message}`}。下書きはそのまま残っています。</span>
          <span class="spacer" />
          {unknown && <button class="btn small" disabled={saving} onClick={onCheckServer}>サーバーを確認</button>}
          {!conflict && error.conflict && (
            <button class="btn small" onClick={onShowExternal}>外部変更を見る</button>
          )}
          <button class="btn small" onClick={onCopyDraft}>下書きをコピー</button>
          {!unknown && <button class="btn small" onClick={onDismissError}>閉じる</button>}
        </div>
      )}
    </div>
  )
}

const eolName = { '\n': 'LF', '\r\n': 'CRLF' }

/** Diff of the draft (or of an external change) in a drawer of bounded height. */
export function EditReview({
  kind,
  hunks,
  format,
  dirty,
  saving,
  conflict,
  unknown,
  onClose,
  onSave,
}: {
  kind: 'draft' | 'external'
  hunks: DiffLine[][]
  format: Format
  dirty: boolean
  saving: boolean
  conflict: boolean
  unknown: boolean
  onClose: () => void
  onSave: () => void
}) {
  return (
    <div class="edit-review" role="dialog" aria-label={kind === 'draft' ? '変更内容の確認' : '外部の変更'}>
      <div class="edit-review-head">
        <strong>{kind === 'draft' ? '変更内容を確認' : '編集開始後に外部で加えられた変更'}</strong>
        <span class="muted small">
          {kind === 'draft'
            ? `改行 ${eolName[format.eol]}・BOM ${format.bom ? 'あり' : 'なし'}・末尾改行 ${format.trailingNewline ? 'あり' : 'なし'}のまま保存します`
            : '下書きの基準からディスク上の現在の内容への差分です'}
        </span>
      </div>
      <div class="edit-review-body">
        {hunks.length === 0 && <div class="muted small">差分はありません。</div>}
        {hunks.map((h, i) => (
          <pre class="edit-hunk" key={i}>
            <span class="diff-hunk-head">
              @@ {h.find((l) => l.baseNo)?.baseNo ?? '-'} / {h.find((l) => l.draftNo)?.draftNo ?? '-'} @@
            </span>
            {h.map((l) => (
              <span class={l.kind === '-' ? 'diff-old' : l.kind === '+' ? 'diff-new' : 'diff-same'}>
                {l.kind} {l.text}
              </span>
            ))}
          </pre>
        ))}
      </div>
      <div class="edit-review-actions">
        {kind === 'draft' && conflict && (
          <span class="small warn-text">外部で変更されているため、保存は競合として拒否されます。</span>
        )}
        <span class="spacer" />
        <button class="btn" disabled={saving} onClick={onClose}>
          {kind === 'draft' ? '編集に戻る' : '閉じる'}
        </button>
        {kind === 'draft' && (
          <button class="btn primary" disabled={!dirty || saving || unknown} onClick={onSave}>
            {saving ? '保存中…' : '保存'}
          </button>
        )}
      </div>
    </div>
  )
}
