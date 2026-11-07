import { useEffect, useRef, useState } from 'preact/hooks'
import { api } from '../api'
import { refreshComments, refreshInfo } from '../state'
import { fileHref } from '../router'
import type { PDFInput, PDFMatch } from '../types'

export function PDFImport() {
  const [open, setOpen] = useState(false)
  const [input, setInput] = useState<PDFInput | null>(null)
  const [matches, setMatches] = useState<PDFMatch[]>([])
  const [choices, setChoices] = useState<Record<number, number>>({})
  const [busy, setBusy] = useState(false)
  const [status, setStatus] = useState('')
  const [error, setError] = useState('')
  const controller = useRef<AbortController | null>(null)
  useEffect(() => () => controller.current?.abort(), [])

  async function match(data: PDFInput) {
    const list = await api.matchPDF(data)
    setMatches(list)
    setChoices(Object.fromEntries(list.map((m, i) => [i, m.automatic && !m.existingId ? 0 : -1])))
    setStatus(`${list.length}件のコメント付きハイライトを照合しました。`)
  }

  async function read(file: File) {
    setBusy(true); setError(''); setStatus('PDFを読み込んでいます…'); setMatches([]); setInput(null)
    const c = new AbortController(); controller.current = c
    try {
      const { extractPDF } = await import('../pdf/extract')
      const data = await extractPDF(file, c.signal, (page, total) => setStatus(`PDFを読み込んでいます（${page}/${total}ページ）`))
      c.signal.throwIfAborted()
      controller.current = null
      setInput(data)
      if (!data.highlights.length) setStatus('コメント付きハイライトはありません。付箋・取り消し線・コメントのないハイライトは対象外です。')
      else { setStatus('原稿を照合しています…'); await match(data) }
    } catch (e) { setError(c.signal.aborted ? '読み込みを中止しました。' : (e as Error).message); setStatus('') }
    finally { setBusy(false); controller.current = null }
  }

  async function rematch() {
    if (!input) return
    setBusy(true); setError('')
    try { await match(input) } catch (e) { setError((e as Error).message) } finally { setBusy(false) }
  }

  async function importSelected() {
    if (!input) return
    setBusy(true); setError('')
    let count = 0
    try {
      for (let i = 0; i < matches.length; i++) {
        const m = matches[i], choice = choices[i] ?? -1
        if (choice < 0 || m.existingId) continue
        const comment = await api.importPDFComment({ name: input.name, documentHash: input.documentHash, highlight: m.highlight, candidate: m.candidates[choice] })
        count++
        setMatches((list) => list.map((item, j) => j === i ? { ...item, existingId: comment.id } : item))
      }
      setStatus(`${count}件を下書きコメントに取り込みました。「レビューを提出」で修正を依頼できます。`)
    } catch (e) { setError(`${count}件を取り込み済みです。${(e as Error).message}`) }
    finally { setBusy(false); void Promise.all([refreshComments(), refreshInfo()]).catch(() => {}) }
  }

  const selected = matches.filter((m, i) => !m.existingId && (choices[i] ?? -1) >= 0).length
  return <section class="card pdf-import">
    <div class="section-head">
      <h2>PDFのコメント</h2>
      <span class="spacer" />
      <button class="btn small" aria-expanded={open} onClick={() => setOpen(!open)}>{open ? '閉じる' : 'PDFから取り込む'}</button>
    </div>
    {open && <>
      <p>ハイライトに付いたコメントを、原稿の下書きコメントとして取り込みます。</p>
      <p class="muted small">引用はPDFの文字列の区切りに合わせてハイライトの前後を含む場合があります。原稿の対応箇所を確認してください。PDF本体は保存されません。</p>
      <label>コメント付きPDF <input type="file" accept="application/pdf,.pdf" disabled={busy} onChange={(e) => {
        const f = e.currentTarget.files?.[0]; e.currentTarget.value = ''; if (f) void read(f)
      }} /></label>
      {input && <p>{input.name}</p>}
      {status && <p role="status">{status}</p>}
      {error && <p class="banner error" role="alert">{error}</p>}
      {busy && controller.current && <button class="btn small" onClick={() => controller.current?.abort()}>読み込みを中止</button>}
      {matches.length > 0 && <>
        <div class="pdf-import-actions">
          <button class="btn primary" disabled={busy || !selected} onClick={() => void importSelected()}>選択した{selected}件を取り込む</button>
          <button class="btn" disabled={busy} onClick={() => void rematch()}>原稿と再照合</button>
        </div>
        {matches.map((m, i) => <article class="pdf-match" key={`${m.highlight.page}:${m.highlight.id}`}>
          <h3>{m.highlight.page}ページ {m.existingId ? `（取り込み済み: ${m.existingId}）` : m.automatic ? '（対応箇所あり）' : '（未確定）'}</h3>
          <p class="pdf-comment-body">{m.highlight.body}</p>
          <blockquote class="pdf-quote">{m.highlight.quote || '引用を取得できませんでした。'}</blockquote>
          {m.highlight.warning && <p class="banner warn">{m.highlight.warning}</p>}
          {!m.existingId && <>
            {!m.candidates.length && !m.highlight.warning && <p class="muted">原稿内に対応する箇所が見つかりませんでした。</p>}
            {m.truncated && <p class="banner warn">候補が多いため一部を表示しています。対象ディレクトリを絞ると確認しやすくなります。</p>}
            {m.candidates.length > 0 && <fieldset disabled={busy}>
              <legend>取り込む箇所</legend>
              <label><input type="radio" name={`pdf-${i}`} checked={(choices[i] ?? -1) === -1} onChange={() => setChoices((c) => ({ ...c, [i]: -1 }))} /> 今回は取り込まない</label>
              {m.candidates.map((c, j) => <div class="pdf-candidate" key={`${c.path}:${c.range.startLine}:${c.range.startColumn}`}>
                <label><input type="radio" name={`pdf-${i}`} checked={choices[i] === j} onChange={() => setChoices((v) => ({ ...v, [i]: j }))} /> {c.path} L{c.range.startLine}–{c.range.endLine}</label>
                <a href={fileHref(c.path, c.range.startLine)} target="_blank" rel="noreferrer">原稿を開く</a>
                <pre>{c.range.text}</pre>
              </div>)}
            </fieldset>}
          </>}
        </article>)}
      </>}
    </>}
  </section>
}
