import { useState } from 'preact/hooks'
import { api } from '../api'
import { editing, info, removeComment, toast, upsertComment } from '../state'
import { renderMarkdown } from '../markdown'
import { anchorHelp, anchorText, commentLines, statusText } from '../labels'
import { fileHref } from '../router'
import type { Comment, Reply } from '../types'
import { Composer } from './Composer'

interface Props {
  comment: Comment
  /** Show the file and line (used outside of the file view). */
  showLocation?: boolean
  /** Shown in a round's history diff, where the current position is irrelevant. */
  historic?: boolean
}

export function Thread({ comment: c, showLocation, historic }: Props) {
  const [open, setOpen] = useState(c.status !== 'resolved')
  const [replying, setReplying] = useState(false)
  const isEditing = editing.value?.kind === 'comment' && editing.value.id === c.id
  const original = c.anchor?.lines

  async function act(p: Promise<Comment>) {
    try {
      upsertComment(await p)
    } catch (e) {
      toast((e as Error).message, 'error')
    }
  }

  const locState = c.scope === 'line' && c.loc && !historic ? c.loc.state : 'exact'
  const where =
    c.scope === 'project'
      ? '全体'
      : c.scope === 'file'
        ? `${c.path}（ファイル全体）`
        : `${c.path} ${commentLines(c)}`

  return (
    <div class={`thread status-${c.status} ${open ? '' : 'collapsed'}`} id={`comment-${c.id}`}>
      <div class="thread-head" onClick={() => c.status === 'resolved' && setOpen(!open)}>
        <span class="cid">{c.id}</span>
        <span class={`chip label-${c.label}`}>{c.label}</span>
        <span class={`chip status status-${c.status}`}>{statusText[c.status]}</span>
        {anchorText[locState] && (
          <span class={`chip anchor-${locState}`} title={anchorHelp[locState]}>
            {anchorText[locState]}
          </span>
        )}
        {showLocation && (
          <a class="where" href={c.path ? fileHref(c.path, c.scope === 'line' ? c.loc?.start : undefined) : '#/'}>
            {where}
          </a>
        )}
        <span class="spacer" />
        <span class="round" title="コメントしたラウンド">
          R{c.round}
        </span>
        {!open && <span class="snippet">{c.body.split('\n')[0]}</span>}
      </div>
      {open && (
        <>
          {locState === 'outdated' && original && (
            <blockquote class="quote" title={c.range ? 'コメントした文字列' : 'コメントしたときの文章'}>
              {(c.range?.text?.split('\n') ?? original).map((l) => (
                <div>{l || ' '}</div>
              ))}
            </blockquote>
          )}
          {isEditing ? (
            <Composer comment={c} original={original} />
          ) : (
            <div class="md" dangerouslySetInnerHTML={{ __html: renderMarkdown(c.body || '（本文なし）', original) }} />
          )}
          {c.replies.map((r) => (
            <ReplyView key={r.id} comment={c} reply={r} />
          ))}
          {replying && <ReplyForm comment={c} onDone={() => setReplying(false)} />}
          {!isEditing && (
            <div class="thread-actions">
              {c.status === 'draft' ? (
                <>
                  <button class="btn small" onClick={() => (editing.value = { kind: 'comment', id: c.id })}>
                    編集
                  </button>
                  <button
                    class="btn small danger-text"
                    onClick={async () => {
                      try {
                        await api.deleteComment(c.id)
                        removeComment(c.id)
                      } catch (e) {
                        toast((e as Error).message, 'error')
                      }
                    }}
                  >
                    削除
                  </button>
                </>
              ) : (
                <>
                  {!replying && (
                    <button class="btn small" onClick={() => setReplying(true)}>
                      返信
                    </button>
                  )}
                  {c.status === 'resolved' ? (
                    <button class="btn small" onClick={() => act(api.updateComment(c.id, { status: 'open' }))}>
                      再オープン
                    </button>
                  ) : (
                    <button
                      class="btn small success"
                      onClick={async () => {
                        await act(api.updateComment(c.id, { status: 'resolved' }))
                        if (!showLocation && !historic) toast(`${c.id} を解決済みにしました。ラウンド${c.round}の履歴から確認できます`, 'success')
                      }}
                    >
                      ✓ 解決
                    </button>
                  )}
                </>
              )}
            </div>
          )}
        </>
      )}
    </div>
  )
}

function ReplyView({ comment, reply: r }: { comment: Comment; reply: Reply }) {
  const [edit, setEdit] = useState(false)
  if (edit) return <ReplyForm comment={comment} reply={r} onDone={() => setEdit(false)} />
  const agent = r.author === 'agent'
  return (
    <div class={`reply ${agent ? 'agent' : 'human'} ${r.draft ? 'draft' : ''}`}>
      <div class="reply-head">
        <span class="who">{agent ? '🤖 エージェント' : '👤 レビュアー'}</span>
        {r.status && <span class={`chip status status-${r.status}`}>{statusText[r.status as keyof typeof statusText] ?? r.status}</span>}
        {r.draft && <span class="chip status status-draft">下書き</span>}
        <span class="round">R{r.round}</span>
        <span class="spacer" />
        {r.draft && (
          <>
            <button class="link" onClick={() => setEdit(true)}>
              編集
            </button>
            <button
              class="link danger-text"
              onClick={async () => {
                try {
                  upsertComment(await api.deleteReply(comment.id, r.id))
                } catch (e) {
                  toast((e as Error).message, 'error')
                }
              }}
            >
              削除
            </button>
          </>
        )}
      </div>
      <div class="md" dangerouslySetInnerHTML={{ __html: renderMarkdown(r.body || '（本文なし）', comment.anchor?.lines) }} />
    </div>
  )
}

function ReplyForm({ comment, reply, onDone }: { comment: Comment; reply?: Reply; onDone: () => void }) {
  const [body, setBody] = useState(reply?.body ?? '')
  const [busy, setBusy] = useState(false)
  const submitted = info.value?.roundStatus === 'submitted'
  async function save() {
    if (!body.trim()) return
    setBusy(true)
    try {
      upsertComment(reply ? await api.updateReply(comment.id, reply.id, body) : await api.addReply(comment.id, body))
      onDone()
    } catch (e) {
      toast((e as Error).message, 'error')
    } finally {
      setBusy(false)
    }
  }
  return (
    <div class="reply-form">
      <textarea
        value={body}
        rows={3}
        placeholder="返信（次のラウンドでエージェントに渡されます）"
        ref={(el) => {
          if (el && !reply) el.focus()
        }}
        onInput={(e) => setBody(e.currentTarget.value)}
        onKeyDown={(e) => {
          if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') void save()
          if (e.key === 'Escape') onDone()
        }}
      />
      <div class="composer-bottom">
        {submitted && !reply && <span class="hint">保存すると次のラウンドが始まります</span>}
        <span class="spacer" />
        <button class="btn small" onClick={onDone}>
          キャンセル
        </button>
        <button class="btn small primary" disabled={busy || !body.trim()} onClick={() => void save()}>
          下書き保存
        </button>
      </div>
    </div>
  )
}
