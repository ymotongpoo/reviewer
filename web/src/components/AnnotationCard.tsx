import { useState } from 'preact/hooks'
import { api } from '../api'
import { annotations, refreshTree, toast, upsertComment } from '../state'
import { renderMarkdown } from '../markdown'
import type { Annotation } from '../types'

const severityText = { critical: '重大', major: '要修正', minor: '軽微', info: '確認推奨' }
const confidenceText = { high: '確信度 高', medium: '確信度 中', low: '確信度 低' }

export function AnnotationCard({ annotation: a, original }: { annotation: Annotation; original?: string[] }) {
  const [busy, setBusy] = useState(false)

  function replace(next: Annotation) {
    annotations.value = annotations.value.map((item) => (item.id === next.id ? next : item))
  }

  async function adopt() {
    setBusy(true)
    try {
      const comment = await api.adoptAnnotation(a.id)
      upsertComment(comment)
      annotations.value = annotations.value.map((item) => (item.id === a.id ? { ...item, state: 'adopted', adoptedAs: comment.id } : item))
      await refreshTree()
      toast(`${a.id} を下書きコメント ${comment.id} として採用しました`, 'success')
    } catch (e) {
      toast((e as Error).message, 'error')
    } finally {
      setBusy(false)
    }
  }

  async function setState(state: 'pending' | 'dismissed') {
    setBusy(true)
    try {
      replace(await api.updateAnnotation(a.id, state))
      toast(state === 'dismissed' ? `${a.id} を却下しました` : `${a.id} を未採用に戻しました`, 'success')
    } catch (e) {
      toast((e as Error).message, 'error')
    } finally {
      setBusy(false)
    }
  }

  const suggestion = a.suggestion ? `\`\`\`suggestion\n${a.suggestion}\n\`\`\`` : ''
  return (
    <article class={`annotation-card severity-${a.severity} confidence-${a.confidence} state-${a.state}`} id={`annotation-${a.id}`}>
      <div class="annotation-head">
        <span class="cid">{a.id}</span>
        <span class={`chip severity severity-${a.severity}`}>{severityText[a.severity]}</span>
        <span class={`chip confidence confidence-${a.confidence}`}>{confidenceText[a.confidence]}</span>
        {a.label && <span class={`chip label-${a.label}`}>{a.label}</span>}
        <span class="spacer" />
        <span class="muted small">依頼 {a.request}</span>
      </div>
      <div class="md" dangerouslySetInnerHTML={{ __html: renderMarkdown(a.body || '（本文なし）') }} />
      {a.evidence.length > 0 && (
        <div class="annotation-evidence">
          <div class="annotation-subtitle">根拠</div>
          {a.evidence.map((e, i) => (
            <div class="evidence-item">
              {e.url ? (
                <a href={e.url} target="_blank" rel="noopener">根拠 {i + 1} ↗</a>
              ) : (
                <span>根拠 {i + 1}</span>
              )}
              {e.note && <span class="evidence-note"> {e.note}</span>}
              {e.quote && <blockquote>{e.quote}</blockquote>}
            </div>
          ))}
        </div>
      )}
      {a.suggestionNote && <div class="suggestion-note">⚠ {a.suggestionNote}</div>}
      {suggestion && <div class="md" dangerouslySetInnerHTML={{ __html: renderMarkdown(suggestion, original) }} />}
      <div class="annotation-actions">
        {a.state === 'pending' ? (
          <>
            <button class="btn small success" disabled={busy} onClick={() => void adopt()}>採用</button>
            <button class="btn small danger-text" disabled={busy} onClick={() => void setState('dismissed')}>却下</button>
          </>
        ) : (
          <button class="btn small" disabled={busy} onClick={() => void setState('pending')}>未採用に戻す</button>
        )}
      </div>
    </article>
  )
}
