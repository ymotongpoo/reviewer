import { useEffect, useLayoutEffect, useRef, useState } from 'preact/hooks'
import { api } from '../api'
import { editing, labels, removeComment, toast, upsertComment, type EditTarget } from '../state'
import { renderMarkdown } from '../markdown'
import type { Comment } from '../types'

interface Props {
  /** Target for a new comment. */
  target?: Extract<EditTarget, { kind: 'new' }>
  /** Existing draft to edit. */
  comment?: Comment
  /** Lines under the comment, used for suggestions. */
  original?: string[]
  onClosed?: () => void
}

type SaveState = 'idle' | 'dirty' | 'saving' | 'saved' | 'error'

/** Editor for a draft comment that saves automatically while typing. */
export function Composer({ target, comment, original, onClosed }: Props) {
  const [label, setLabel] = useState(comment?.label ?? labels.value[0]?.name ?? 'must')
  const [body, setBody] = useState(comment?.body ?? '')
  const [tab, setTab] = useState<'write' | 'preview'>('write')
  const [save, setSave] = useState<SaveState>(comment ? 'saved' : 'idle')
  const id = useRef<string | undefined>(comment?.id)
  const queue = useRef<Promise<void>>(Promise.resolve())
  const timer = useRef<ReturnType<typeof setTimeout>>()
  const latest = useRef({ label, body })
  const ta = useRef<HTMLTextAreaElement>(null)
  const caret = useRef<number | null>(null)
  latest.current = { label, body }

  // Restore the caret after programmatic edits, once the new value is rendered.
  useLayoutEffect(() => {
    if (caret.current === null || !ta.current) return
    ta.current.focus()
    ta.current.setSelectionRange(caret.current, caret.current)
    caret.current = null
  }, [body])

  useEffect(() => {
    ta.current?.focus()
    const warn = (e: BeforeUnloadEvent) => {
      if (timer.current) e.preventDefault()
    }
    window.addEventListener('beforeunload', warn)
    return () => {
      window.removeEventListener('beforeunload', warn)
      if (timer.current) {
        clearTimeout(timer.current)
        void enqueue()
      }
    }
  }, [])

  function enqueue(): Promise<void> {
    timer.current = undefined
    queue.current = queue.current.then(persist)
    return queue.current
  }

  async function persist() {
    const { label, body } = latest.current
    try {
      if (!id.current) {
        if (!body.trim() || !target) return
        setSave('saving')
        const c = await api.createComment({
          scope: target.scope,
          path: target.path,
          start: target.start,
          end: target.end,
          hash: target.hash,
          range: target.range,
          label,
          body,
        })
        id.current = c.id
        upsertComment(c)
        if (editing.value?.kind === 'new') editing.value = { ...editing.value, createdId: c.id }
      } else {
        setSave('saving')
        upsertComment(await api.updateComment(id.current, { label, body }))
      }
      setSave('saved')
    } catch (e) {
      setSave('error')
      toast(`保存できませんでした: ${(e as Error).message}`, 'error')
    }
  }

  function schedule() {
    setSave('dirty')
    clearTimeout(timer.current)
    timer.current = setTimeout(() => void enqueue(), 600)
  }

  async function close() {
    if (timer.current) {
      clearTimeout(timer.current)
      await enqueue()
    } else {
      await queue.current
    }
    if (id.current && !latest.current.body.trim()) {
      await api.deleteComment(id.current).catch(() => {})
      removeComment(id.current)
    }
    editing.value = null
    onClosed?.()
  }

  async function discard() {
    clearTimeout(timer.current)
    timer.current = undefined
    await queue.current
    if (id.current) {
      try {
        await api.deleteComment(id.current)
        removeComment(id.current)
      } catch (e) {
        toast(`削除できませんでした: ${(e as Error).message}`, 'error')
        return
      }
    }
    editing.value = null
    onClosed?.()
  }

  function insertSuggestion() {
    const el = ta.current
    const content = (original ?? []).join('\n')
    const pos = el && tab === 'write' ? el.selectionStart : body.length
    const prefix = body.slice(0, pos)
    const sep = prefix && !prefix.endsWith('\n') ? '\n' : ''
    const head = prefix + sep + '```suggestion\n' + content
    const next = head + '\n```\n' + body.slice(pos)
    setTab('write')
    setBody(next)
    latest.current.body = next
    caret.current = head.length
    schedule()
  }

  function onKeyDown(e: KeyboardEvent) {
    if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') {
      e.preventDefault()
      void close()
    } else if (e.key === 'Escape') {
      e.preventDefault()
      void close()
    }
  }

  const saveText: Record<SaveState, string> = {
    idle: '入力すると自動保存されます',
    dirty: '未保存…',
    saving: '保存中…',
    saved: '下書き保存済み',
    error: '保存に失敗しました',
  }

  return (
    <div class="composer" onKeyDown={onKeyDown}>
      <div class="composer-top">
        <div class="label-picker" role="radiogroup" aria-label="種別">
          {labels.value.map((l) => (
            <button
              type="button"
              role="radio"
              aria-checked={label === l.name}
              class={`chip label-${l.name} ${label === l.name ? 'on' : ''}`}
              title={l.description}
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => {
                setLabel(l.name)
                latest.current.label = l.name
                if (id.current || body.trim()) schedule()
              }}
            >
              {l.name}
            </button>
          ))}
        </div>
        <div class="tabs">
          <button type="button" class={tab === 'write' ? 'on' : ''} onClick={() => setTab('write')}>
            書く
          </button>
          <button type="button" class={tab === 'preview' ? 'on' : ''} onClick={() => setTab('preview')}>
            プレビュー
          </button>
        </div>
      </div>
      {tab === 'write' ? (
        <textarea
          ref={ta}
          value={body}
          placeholder="コメントを書く（Markdown）。⌘/Ctrl+Enter で閉じる"
          rows={Math.min(16, Math.max(4, body.split('\n').length + 1))}
          onInput={(e) => {
            setBody(e.currentTarget.value)
            latest.current.body = e.currentTarget.value
            schedule()
          }}
        />
      ) : (
        <div class="md preview" dangerouslySetInnerHTML={{ __html: renderMarkdown(body || '（本文なし）', original) }} />
      )}
      <div class="composer-bottom">
        {original && (
          <button
            type="button"
            class="btn small"
            onMouseDown={(e) => e.preventDefault()}
            onClick={insertSuggestion}
            title="選択範囲を置き換える修正案を挿入"
          >
            ± 修正案を挿入
          </button>
        )}
        <span class={`save-state ${save}`}>{saveText[save]}</span>
        <span class="spacer" />
        <button type="button" class="btn small danger-text" onClick={() => void discard()}>
          破棄
        </button>
        <button type="button" class="btn small primary" onClick={() => void close()}>
          完了
        </button>
      </div>
    </div>
  )
}
