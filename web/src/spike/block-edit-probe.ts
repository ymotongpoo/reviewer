import { insertText, mapLines } from '../editbuffer'
import { initInputLog } from '../inputlog'

initInputLog({ force: true })
const original = Array.from({ length: 5 }, (_, i) => [
  `## 計測用の見出し ${i + 1}`, '', '日本語の変換と確定を確認します。', '次の行へ移って入力を続けます。', '',
  '- コメントを追加します。', '- 差分を確認します。', '', '```ts', `const sample = ${i + 1}`, '```', '',
]).flat()
let lines = original.slice()
const history: string[][] = []
const container = document.getElementById('lines')!
const result = document.getElementById('result')!
let editing: { start: number; end: number; input: HTMLTextAreaElement } | undefined
let composing = false
let pending: (() => void) | undefined

function finish() {
  if (!editing) return
  const { start, end, input } = editing
  const next = insertText({ lines, sel: { anchor: { line: start, col: 0 }, head: { line: end, col: lines[end].length } } }, input.value).lines
  if (next.join('\n') !== lines.join('\n')) history.push(lines)
  lines = next
  editing = undefined
}
function afterComposition(action: () => void) {
  if (composing) { pending = action; return }
  action()
}
function render(message = '') {
  container.replaceChildren()
  const diff = mapLines(original, lines)
  result.textContent = `変更 ${diff.changedCount}行、削除 ${diff.deletedCount}行。${message}`
  lines.forEach((text, no) => {
    const row = document.createElement('div')
    row.className = 'row'
    row.id = `L${no + 1}`
    const gutter = document.createElement('span')
    gutter.className = 'ln clickable'
    gutter.append(String(no + 1))
    const edit = document.createElement('button')
    edit.className = 'edit-marker'
    edit.textContent = '✎'
    edit.setAttribute('aria-label', `行${no + 1}を編集`)
    edit.onclick = () => afterComposition(() => { finish(); open(Math.min(no, lines.length - 1)) })
    gutter.append(edit)
    const body = document.createElement('span')
    body.className = 'text'
    body.textContent = text || '\u200b'
    row.append(gutter, body)
    container.append(row)
  })
}
function open(no: number) {
  render()
  let start = no, end = no
  if (lines[no].trim()) {
    while (start > Math.max(0, no - 10) && lines[start - 1].trim()) start--
    while (end < Math.min(lines.length - 1, no + 10) && lines[end + 1].trim()) end++
  }
  const block = document.createElement('div')
  block.className = 'block-editor'
  const label = document.createElement('label')
  label.textContent = `L${start + 1}–L${end + 1}`
  const input = document.createElement('textarea')
  input.dataset.inputlogTarget = 'block'
  input.setAttribute('aria-label', 'ブロック本文')
  input.value = lines.slice(start, end + 1).join('\n')
  input.rows = Math.max(4, end - start + 2)
  input.addEventListener('compositionstart', () => { composing = true })
  input.addEventListener('compositionend', () => {
    composing = false
    // Let the final input event and the bubble-stage logger observe the textarea.
    queueMicrotask(() => { const action = pending; pending = undefined; action?.() })
  })
  label.append(input)
  const done = document.createElement('button')
  done.textContent = '完了'
  done.onclick = () => afterComposition(() => { finish(); render() })
  const actions = document.createElement('div')
  actions.className = 'block-actions'
  actions.append(done)
  block.append(label, actions)
  container.children[start].before(block)
  for (let i = start; i <= end; i++) document.getElementById(`L${i + 1}`)!.remove()
  editing = { start, end, input }
  input.focus()
}
document.getElementById('undo')!.onclick = () => afterComposition(() => {
  finish()
  const previous = history.pop()
  if (previous) lines = previous
  render(lines.join('\n') === original.join('\n') ? '元の60行と一致します。' : '直前の変更を戻しました。')
})
render()
