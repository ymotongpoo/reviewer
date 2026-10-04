import { describe, expect, test } from 'vitest'
import { beforeInputIntent, createIntentGuard, keydownIntent, type Caret, type Intent, type KeyInfo } from './inputintent'

const bools = [false, true]
const keyInfo = (key: string, rest: Partial<KeyInfo> = {}): KeyInfo => ({
  key, keyCode: 0, isComposing: false, shift: false, ctrl: false, meta: false, alt: false, ...rest,
})
const caret: Caret = { collapsed: true, atStart: true, atEnd: true, block: false }

describe('keydown classification', () => {
  for (const key of ['Enter', 'Tab', 'Backspace', 'Delete', 'z', 'Z', 'y', 'a', 'ArrowLeft', 'Escape', 'Unidentified', 'Process']) {
    test(`${key}: all modifiers and IME flags`, () => {
      for (const shift of bools) for (const ctrl of bools) for (const meta of bools) for (const alt of bools) {
        for (const composing of bools) for (const isComposing of bools) for (const keyCode of [0, 229]) {
          const k = keyInfo(key, { shift, ctrl, meta, alt, isComposing, keyCode })
          let expected: Intent = 'native'
          if (composing || isComposing || keyCode === 229 || key === 'Unidentified' || key === 'Process') expected = 'defer'
          else if ((ctrl || meta) && !alt && /^[zZ]$/.test(key)) expected = shift ? 'redo' : 'undo'
          else if (ctrl && !meta && !alt && key === 'y') expected = 'redo'
          else if (!ctrl && !meta && !alt) expected = ({ Enter: 'newline', Tab: shift ? 'outdent' : 'indent', Backspace: 'joinBackward', Delete: 'joinForward' } as Record<string, Intent>)[key] ?? 'native'
          expect(keydownIntent(k, composing), JSON.stringify({ k, composing })).toBe(expected)
        }
      }
    })
  }
})

describe('beforeinput classification', () => {
  for (const inputType of ['insertLineBreak', 'insertParagraph', 'deleteContentBackward', 'deleteContentForward', 'historyUndo', 'historyRedo', 'insertCompositionText', 'insertFromComposition', 'deleteCompositionText', 'insertText', 'insertFromPaste', 'deleteWordBackward', '']) {
    test(`${inputType || '(empty)'}: all cancellation, composition and caret combinations`, () => {
      for (const cancelable of bools) for (const composing of bools) {
        for (const collapsed of bools) for (const atStart of bools) for (const atEnd of bools) for (const block of bools) {
          const c = { collapsed, atStart, atEnd, block }
          let expected: Intent = 'native'
          if (!composing) {
            if (inputType === 'insertLineBreak' || inputType === 'insertParagraph') expected = cancelable ? 'newline' : 'native'
            if (inputType === 'deleteContentBackward' && (block || (collapsed && atStart))) expected = cancelable ? 'joinBackward' : 'defer'
            if (inputType === 'deleteContentForward' && (block || (collapsed && atEnd))) expected = cancelable ? 'joinForward' : 'defer'
            if (inputType === 'historyUndo') expected = 'undo'
            if (inputType === 'historyRedo') expected = 'redo'
          }
          expect(beforeInputIntent(inputType, cancelable, composing, c), JSON.stringify({ cancelable, composing, c })).toBe(expected)
        }
      }
    })
  }
})

test('S-04 sequences: Android keys defer to cancelable beforeinput exactly once', () => {
  for (const key of ['Unidentified', 'Process', 'Enter', 'Backspace', 'Delete']) {
    for (const [type, intent] of [['insertLineBreak', 'newline'], ['deleteContentBackward', 'joinBackward'], ['deleteContentForward', 'joinForward']] as const) {
      const guard = createIntentGuard()
      guard.keydown()
      const intents = [keydownIntent(keyInfo(key, { keyCode: 229 }), false), beforeInputIntent(type, true, false, caret)]
      expect(intents).toEqual(['defer', intent])
      expect(intents.filter((i) => i !== 'defer' && !guard.consume(i))).toEqual([intent])
    }
  }
})

test('keydown echoes cannot reapply an intent, while new keys and native-only events remain independent', () => {
  const guard = createIntentGuard()
  for (const [key, type] of [['Enter', 'insertLineBreak'], ['Backspace', 'deleteContentBackward'], ['Delete', 'deleteContentForward'], ['z', 'historyUndo']] as const) {
    let applications = 0
    expect(guard.keydown()).toBeGreaterThan(0)
    const intent = keydownIntent(keyInfo(key, { ctrl: key === 'z' }), false)
    guard.mark(intent)
    applications++
    const echo = beforeInputIntent(type, true, false, caret)
    if (!guard.consume(echo)) applications++
    expect(applications).toBe(1)
    expect(guard.consume(echo)).toBe(true)
    guard.clear()
    expect(guard.consume(echo)).toBe(false)
    guard.mark(intent)
    guard.keydown()
    expect(guard.consume(echo)).toBe(false)
    guard.mark(intent)
    guard.clear()
    expect(guard.consume(echo)).toBe(false)
  }
})

test('S-04 uncancelable sequences delegate newline to input and boundary deletion to defer', () => {
  const intents = ['insertLineBreak', 'insertParagraph', 'deleteContentBackward', 'deleteContentForward'].map((type) => beforeInputIntent(type, false, false, caret))
  expect(intents).toEqual(['native', 'native', 'defer', 'defer'])
})

// Actual Gboard ordering remains a device gate; synthetic sequences are above.
const deviceLogs = import.meta.glob('../../docs/mobile/device-logs/*.json', { query: '?raw', import: 'default', eager: true }) as Record<string, string>
test.skipIf(Object.keys(deviceLogs).length === 0)('replay device logs (no device logs recorded yet)', () => {
  for (const raw of Object.values(deviceLogs)) {
    const log = JSON.parse(raw)
    const events = Array.isArray(log) ? log : log.entries
    expect(Array.isArray(events)).toBe(true)
    let composing = false
    for (const event of events) {
      if (event.type === 'compositionstart') composing = true
      if (event.type === 'compositionend') composing = false
      if (event.type === 'keydown' && (composing || event.isComposing || event.keyCode === 229 || ['Unidentified', 'Process'].includes(event.key))) {
        expect(keydownIntent(keyInfo(event.key, event), composing)).toBe('defer')
      }
      if (event.type === 'beforeinput' && (composing || event.isComposing)) {
        expect(beforeInputIntent(event.inputType, event.cancelable, true, caret)).toBe('native')
      }
    }
  }
})
