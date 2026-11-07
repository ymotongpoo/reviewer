import { getDocument, GlobalWorkerOptions } from 'pdfjs-dist'
import workerURL from 'pdfjs-dist/build/pdf.worker.min.mjs?url'
import { sha256 } from '@noble/hashes/sha2.js'
import { pageHighlights, type TextRun } from './highlights'
import type { PDFInput } from '../types'

GlobalWorkerOptions.workerSrc = workerURL

export async function extractPDF(file: File, signal: AbortSignal, progress: (page: number, total: number) => void): Promise<PDFInput> {
  if (file.size > 50 * 1024 * 1024) throw new Error('PDFは50 MiB以下にしてください。')
  const data = new Uint8Array(await file.arrayBuffer())
  signal.throwIfAborted()
  const documentHash = Array.from(sha256(data), (b) => b.toString(16).padStart(2, '0')).join('')
  const task = getDocument({ data, cMapUrl: '/assets/pdf/cmaps/', cMapPacked: true, standardFontDataUrl: '/assets/pdf/standard_fonts/' })
  const abort = () => { void task.destroy() }
  signal.addEventListener('abort', abort, { once: true })
  try {
    const doc = await task.promise
    if (doc.numPages > 10000) throw new Error('PDFは10,000ページ以下にしてください。')
    const result: PDFInput = { name: file.name, documentHash, highlights: [] }
    for (let n = 1; n <= doc.numPages; n++) {
      signal.throwIfAborted()
      progress(n, doc.numPages)
      const page = await doc.getPage(n)
      const annotations = await page.getAnnotations()
      if (annotations.some((a) => a.subtype === 'Highlight' && a.contentsObj?.str?.trim())) {
        const text = await page.getTextContent()
        result.highlights.push(...pageHighlights(n, annotations, text.items.filter((item): item is TextRun & { hasEOL: boolean } => 'str' in item), text.styles))
        if (result.highlights.length > 500) throw new Error('コメント付きハイライトは500件以下にしてください。')
      }
      page.cleanup()
    }
    return result
  } finally {
    signal.removeEventListener('abort', abort)
    await task.destroy()
  }
}
