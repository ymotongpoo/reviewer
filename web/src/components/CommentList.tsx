import { closeCommentList, comments, commentListSelection, visibleAnnotations } from '../state'
import { lineRange, statusText } from '../labels'
import { renderMarkdown } from '../markdown'
import type { Annotation, Comment } from '../types'

const severityText = { critical: '重大', major: '要修正', minor: '軽微', info: '確認推奨' }

function humanLocation(c: Comment): string {
  if (c.scope === 'file') return 'ファイル全体'
  if (c.scope === 'project') return 'プロジェクト全体'
  return lineRange(c.loc?.start, c.loc?.end)
}

function aiLocation(a: Annotation): string {
  if (!a.loc) return '位置不明'
  return lineRange(a.loc.start, a.loc.end)
}

function jumpTo(id: string) {
  const element = document.getElementById(id)
  if (!element) return
  element.scrollIntoView({ behavior: 'smooth', block: 'center' })
  element.classList.add('comment-list-target')
  window.setTimeout(() => element.classList.remove('comment-list-target'), 1400)
}

function HumanCard({ comment }: { comment: Comment }) {
  return (
    <button class="comment-list-card" onClick={() => jumpTo(`comment-${comment.id}`)}>
      <div class="comment-list-card-head">
        <span class="cid">{comment.id}</span>
        <span class={`chip label-${comment.label}`}>{comment.label}</span>
        <span class={`chip status status-${comment.status}`}>{statusText[comment.status]}</span>
        <span class="spacer" />
        <span class="where">{humanLocation(comment)}</span>
      </div>
      <div class="comment-list-excerpt">{comment.body.split('\n')[0] || '（本文なし）'}</div>
    </button>
  )
}

function AnnotationCardItem({ annotation }: { annotation: Annotation }) {
  return (
    <button class="comment-list-card" onClick={() => jumpTo(`annotation-${annotation.id}`)}>
      <div class="comment-list-card-head">
        <span class="cid">{annotation.id}</span>
        <span class={`chip severity severity-${annotation.severity}`}>{severityText[annotation.severity]}</span>
        <span class={`chip confidence confidence-${annotation.confidence}`}>{annotation.confidence}</span>
        <span class="spacer" />
        <span class="where">{aiLocation(annotation)}</span>
      </div>
      <div
        class="comment-list-excerpt md"
        dangerouslySetInnerHTML={{ __html: renderMarkdown(annotation.body || '（本文なし）') }}
      />
    </button>
  )
}

export function CommentList({ path }: { path: string }) {
  const selection = commentListSelection.value
  if (!selection || selection.path !== path) return null

  const human = selection.kind === 'human'
  const items = human
    ? comments.value.filter((c) => c.path === path && c.status !== 'resolved')
    : visibleAnnotations.value.filter((a) => a.path === path && a.state === 'pending')

  return (
    <section class={`comment-list ${human ? 'human' : 'ai'}`} aria-label={human ? '人間コメント一覧' : 'AIコメント一覧'}>
      <div class="comment-list-head">
        <strong>{human ? '人間コメント' : 'AIコメント'}</strong>
        <span class="muted small">{items.length}件</span>
        <span class="spacer" />
        <button class="btn small" onClick={closeCommentList} aria-label="コメント一覧を閉じる">閉じる</button>
      </div>
      {items.length === 0 ? (
        <div class="empty small">表示できるコメントがありません</div>
      ) : (
        <div class="comment-list-items">
          {human
            ? (items as Comment[]).map((c) => <HumanCard key={c.id} comment={c} />)
            : (items as Annotation[]).map((a) => <AnnotationCardItem key={a.id} annotation={a} />)}
        </div>
      )}
    </section>
  )
}
