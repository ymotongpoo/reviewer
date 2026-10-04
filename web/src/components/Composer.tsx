import { useKeyboardReveal } from './useKeyboardReveal'
import { useEffect, useLayoutEffect, useRef, useState } from 'preact/hooks'
import { api, projectId } from '../api'
import { comments, composerJournalVersion, draftStore, editing, labels, refreshComments, registerComposerFlusher, removeComment, toast, upsertComment, type EditTarget } from '../state'
import { classifySaveError, composerKey, matchesDraft, shouldDropJournal, type ComposerJournal, type SaveOutcome } from '../drafts'
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

type SaveState = 'idle' | 'dirty' | 'saving' | 'saved' | 'failed' | 'unknown'

/** Editor for a draft comment that saves automatically while typing. */
export function Composer({ target, comment, original, onClosed }: Props) {
  const [label, setLabel] = useState(comment?.label ?? labels.value[0]?.name ?? 'must')
  const [body, setBody] = useState(comment?.body ?? '')
  const [tab, setTab] = useState<'write' | 'preview'>('write')
  const [save, setSave] = useState<SaveState>(comment ? 'saved' : 'idle')
  const [recovery, setRecovery] = useState<ComposerJournal>()
  const [showError, setShowError] = useState(false)
  const [busy, setBusy] = useState(false)
  const id = useRef<string | undefined>(comment?.id)
  const newTarget = useRef(target)
  const journalTarget = useRef(target ?? (comment ? {
    kind: 'new' as const, scope: comment.scope, path: comment.path,
    start: comment.origStart, end: comment.origEnd, range: comment.range,
  } : undefined))
  const key = useRef(composerKey(projectId!, { ...target, commentId: comment?.id, scope: target?.scope ?? comment!.scope }))
  const queue = useRef<Promise<SaveOutcome | 'empty'>>(Promise.resolve('empty'))
  const journalQueue = useRef<Promise<void>>(Promise.resolve())
  const ready = useRef<Promise<void>>(Promise.resolve())
  const timer = useRef<ReturnType<typeof setTimeout>>()
  const journalTimer = useRef<ReturnType<typeof setTimeout>>()
  const rev = useRef(0)
  const savedRev = useRef(0)
  const status = useRef<SaveState>(comment ? 'saved' : 'idle')
  const pendingRecovery = useRef<ComposerJournal>()
  const attemptedBody = useRef<string>()
  const inFlight = useRef(false)
  const closed = useRef(false)
  const latest = useRef({ label, body })
  const formRef = useKeyboardReveal()
  const ta = useRef<HTMLTextAreaElement>(null)
  const caret = useRef<number | null>(null)

  function updateStatus(value: SaveState) {
    status.current = value
    setSave(value)
  }
  const blocked = () => status.current === 'failed' || status.current === 'unknown'

  useLayoutEffect(() => {
    if (caret.current === null || !ta.current) return
    ta.current.focus()
    ta.current.setSelectionRange(caret.current, caret.current)
    caret.current = null
  }, [body])

  useLayoutEffect(() => {
    ready.current = (async () => {
      let j = await draftStore.get<ComposerJournal>('composer', key.current)
      if (!j && comment) {
        j = (await draftStore.list<ComposerJournal>('composer', `${projectId}:`)).find((j) => j.commentId === comment.id)
      }
      if (closed.current || !j) return
      key.current = j.key
      // Even identical text needs a decision when the preceding POST was uncertain.
      if (j.body !== latest.current.body || j.label !== latest.current.label || j.outcome) {
        pendingRecovery.current = j
        setRecovery(j)
      }
    })()
    const unregister = registerComposerFlusher(flush)
    return unregister
  }, [])

  useEffect(() => {
    ta.current?.focus()
    const warn = (e: BeforeUnloadEvent) => {
      if (rev.current !== savedRev.current || blocked()) e.preventDefault()
    }
    const flushHidden = () => {
      if (document.visibilityState === 'hidden') flushPage()
    }
    const flushPage = () => { void writeJournal(); void enqueue() }
    window.addEventListener('beforeunload', warn)
    document.addEventListener('visibilitychange', flushHidden)
    window.addEventListener('pagehide', flushPage)
    return () => {
      window.removeEventListener('beforeunload', warn)
      document.removeEventListener('visibilitychange', flushHidden)
      window.removeEventListener('pagehide', flushPage)
      clearTimeout(timer.current)
      clearTimeout(journalTimer.current)
      if (!closed.current) { void writeJournal(); void enqueue() }
    }
  }, [])

  function journalTask(fn: () => Promise<void>) {
    journalQueue.current = journalQueue.current.then(fn)
    return journalQueue.current
  }

  function writeJournal() {
    clearTimeout(journalTimer.current)
    return journalTask(async () => {
      await ready.current
      if (closed.current || pendingRecovery.current) return
      if (!latest.current.body.trim()) {
        await draftStore.del('composer', key.current)
      } else if (rev.current !== savedRev.current || blocked() || status.current === 'saving') {
        await draftStore.put<ComposerJournal>('composer', key.current, {
          key: key.current, target: journalTarget.current, commentId: id.current, ...latest.current,
          rev: rev.current, savedAt: Date.now(),
          outcome: inFlight.current ? 'unknown' : blocked() ? status.current as 'failed' | 'unknown' : undefined,
          attemptedBody: attemptedBody.current,
        })
      }
      composerJournalVersion.value++
    })
  }

  function enqueue(): Promise<SaveOutcome | 'empty'> {
    clearTimeout(timer.current)
    timer.current = undefined
    queue.current = queue.current.then(persist)
    return queue.current
  }

  async function persist(): Promise<SaveOutcome | 'empty'> {
    await ready.current
    if (closed.current) return 'empty'
    if (pendingRecovery.current) return 'failed'
    if (blocked()) return status.current as SaveOutcome
    while (rev.current !== savedRev.current) {
      const { label, body } = latest.current
      const sentRev = rev.current
      const target = newTarget.current
      if (!id.current && (!body.trim() || !target)) return 'empty'
      updateStatus('saving')
      inFlight.current = true
      attemptedBody.current = body
      // Persist uncertainty before sending: reloading must not silently repeat a POST.
      await writeJournal()
      try {
        const c = id.current
          ? await api.updateComment(id.current, { label, body })
          : await api.createComment({ scope: target!.scope, path: target!.path, start: target!.start, end: target!.end, hash: target!.hash, range: target!.range, label, body })
        id.current = c.id
        upsertComment(c)
        if (target && editing.value?.kind === 'new' && composerKey(projectId!, editing.value) === key.current) {
          editing.value = { ...editing.value, createdId: c.id }
        }
        inFlight.current = false
        savedRev.current = sentRev
        attemptedBody.current = undefined
        updateStatus('saved')
        await journalTask(async () => {
          const j = await draftStore.get<ComposerJournal>('composer', key.current)
          if (shouldDropJournal(j, sentRev)) await draftStore.del('composer', key.current)
          composerJournalVersion.value++
        })
        if (rev.current !== sentRev) await writeJournal()
      } catch (e) {
        inFlight.current = false
        updateStatus(classifySaveError(e))
        clearTimeout(timer.current)
        timer.current = undefined
        await writeJournal()
        toast(`保存できませんでした: ${(e as Error).message}`, 'error')
        return status.current as SaveOutcome
      }
    }
    return id.current ? 'saved' : 'empty'
  }

  function schedule() {
    rev.current++
    clearTimeout(journalTimer.current)
    journalTimer.current = setTimeout(() => void writeJournal(), 300)
    if (blocked() || pendingRecovery.current) return
    updateStatus('dirty')
    clearTimeout(timer.current)
    timer.current = setTimeout(() => void enqueue(), 600)
  }

  async function flush() {
    const outcome = await enqueue()
    if (outcome === 'failed' || outcome === 'unknown') setShowError(true)
    return outcome
  }

  function finish() {
    closed.current = true
    clearTimeout(timer.current)
    clearTimeout(journalTimer.current)
    editing.value = null
    onClosed?.()
  }

  async function close() {
    if (busy) return
    setBusy(true)
    try {
      const outcome = await flush()
      if (outcome === 'failed' || outcome === 'unknown') return
      if (id.current && !latest.current.body.trim()) {
        await api.deleteComment(id.current)
        removeComment(id.current)
      }
      finish()
    } catch (e) {
      toast(`保存できませんでした: ${(e as Error).message}`, 'error')
    } finally { setBusy(false) }
  }

  async function retry() {
    if (busy || pendingRecovery.current) return
    setBusy(true)
    try {
      await queue.current
      const target = newTarget.current
      if (status.current === 'unknown' && !id.current && target) {
        await refreshComments()
        const c = comments.value.find((c) => matchesDraft(c, target, attemptedBody.current ?? latest.current.body))
        if (c) id.current = c.id
      }
      updateStatus('dirty')
      setShowError(false)
      const outcome = await flush()
      if (outcome === 'saved' || outcome === 'empty') finish()
    } catch (e) {
      toast(`サーバーを確認できませんでした: ${(e as Error).message}`, 'error')
    } finally { setBusy(false) }
  }

  async function keepLocal() {
    setBusy(true)
    try {
      await queue.current
      await writeJournal()
      if (!draftStore.persistent) {
        toast('端末への保存を使えません。本文をコピーしてから閉じてください', 'error')
        return
      }
      finish()
      toast('端末に保存しました。同じ場所でコメントを開くと復元できます')
    } finally { setBusy(false) }
  }

  async function discard() {
    setBusy(true)
    clearTimeout(timer.current)
    clearTimeout(journalTimer.current)
    await queue.current
    try {
      if (id.current) {
        await api.deleteComment(id.current)
        removeComment(id.current)
      }
      await journalTask(() => draftStore.del('composer', key.current))
      composerJournalVersion.value++
      finish()
    } catch (e) {
      toast(`削除できませんでした: ${(e as Error).message}`, 'error')
    } finally { setBusy(false) }
  }

  function restoreJournal() {
    const j = pendingRecovery.current!
    pendingRecovery.current = undefined
    setRecovery(undefined)
    id.current = j.commentId ?? id.current
    if (!id.current && j.target) newTarget.current = j.target
    journalTarget.current = j.target ?? journalTarget.current
    latest.current = { label: j.label, body: j.body }
    setLabel(j.label); setBody(j.body)
    rev.current = Math.max(rev.current, j.rev)
    attemptedBody.current = j.attemptedBody
    if (j.outcome) { updateStatus(j.outcome); setShowError(true) }
    schedule()
  }

  async function discardRecovery() {
    await journalTask(() => draftStore.del('composer', key.current))
    pendingRecovery.current = undefined
    setRecovery(undefined)
    composerJournalVersion.value++
    if (rev.current !== savedRev.current) schedule()
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
    failed: '保存に失敗しました',
    unknown: '保存結果を確認できません',
  }

  return (
    <div ref={formRef} class="composer" onKeyDown={onKeyDown}>
      {recovery && (
        <div class="banner warn composer-recovery"><div>
          端末に保存された下書き（{new Date(recovery.savedAt).toLocaleTimeString('ja-JP', { hour: '2-digit', minute: '2-digit' })}）があります
          <button class="btn small" onClick={restoreJournal}>復元</button>
          <button class="btn small" onClick={() => void discardRecovery()}>破棄</button>
        </div></div>
      )}
      <div class="composer-top">
        <div class="label-picker" role="radiogroup" aria-label="種別">
          {labels.value.map((l) => (
            <button
              type="button"
              disabled={busy}
              role="radio"
              aria-checked={label === l.name}
              class={`chip label-${l.name} ${label === l.name ? 'on' : ''}`}
              title={l.description}
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => {
                setLabel(l.name)
                latest.current.label = l.name
                schedule()
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
          disabled={busy}
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
      {showError && !recovery && (
        <div class="banner error composer-error" role="alert"><div>
          <p>{saveText[save]}。{save === 'unknown' && '再試行すると、先にサーバーの下書きを確認します。'}</p>
          <button class="btn small" disabled={busy} onClick={() => void retry()}>再試行</button>
          <button class="btn small" disabled={busy} onClick={() => void keepLocal()}>端末に保存して閉じる</button>
          <button class="btn small" disabled={busy} onClick={() => setShowError(false)}>編集を続ける</button>
        </div></div>
      )}
      <div class="composer-bottom">
        {original && (
          <button
            type="button"
            class="btn small"
            onMouseDown={(e) => e.preventDefault()}
            disabled={busy}
            onClick={insertSuggestion}
            title="選択範囲を置き換える修正案を挿入"
          >
            ± 修正案を挿入
          </button>
        )}
        <span class={`save-state ${save}${save === 'failed' || save === 'unknown' ? ' error' : ''}`}>{saveText[save]}</span>
        <span class="spacer" />
        <button type="button" class="btn small danger-text" disabled={busy} onClick={() => void discard()}>
          破棄
        </button>
        <button type="button" class="btn small primary" disabled={busy} onClick={() => void close()}>
          完了
        </button>
      </div>
    </div>
  )
}
