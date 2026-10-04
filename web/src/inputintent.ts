export interface KeyInfo {
  key: string
  keyCode: number
  isComposing: boolean
  shift: boolean
  ctrl: boolean
  meta: boolean
  alt: boolean
}

export interface Caret { collapsed: boolean; atStart: boolean; atEnd: boolean; block: boolean }
export type Intent = 'newline' | 'joinBackward' | 'joinForward' | 'indent' | 'outdent' | 'undo' | 'redo' | 'native' | 'defer'

export function keydownIntent(k: KeyInfo, composing: boolean): Intent {
  if (composing || k.isComposing || k.keyCode === 229 || k.key === 'Unidentified' || k.key === 'Process') return 'defer'
  if ((k.ctrl || k.meta) && !k.alt && k.key.toLowerCase() === 'z') return k.shift ? 'redo' : 'undo'
  if (k.ctrl && !k.meta && !k.alt && k.key === 'y') return 'redo'
  if (k.ctrl || k.meta || k.alt) return 'native'
  switch (k.key) {
    case 'Enter': return 'newline'
    case 'Tab': return k.shift ? 'outdent' : 'indent'
    case 'Backspace': return 'joinBackward'
    case 'Delete': return 'joinForward'
    default: return 'native'
  }
}

export function beforeInputIntent(inputType: string, cancelable: boolean, composing: boolean, caret: Caret): Intent {
  if (composing) return 'native'
  switch (inputType) {
    case 'insertLineBreak':
    case 'insertParagraph': return cancelable ? 'newline' : 'native'
    case 'deleteContentBackward':
      if (caret.block || (caret.collapsed && caret.atStart)) return cancelable ? 'joinBackward' : 'defer'
      return 'native'
    case 'deleteContentForward':
      if (caret.block || (caret.collapsed && caret.atEnd)) return cancelable ? 'joinForward' : 'defer'
      return 'native'
    case 'historyUndo': return 'undo'
    case 'historyRedo': return 'redo'
    default: return 'native'
  }
}

/** A prevented key normally has no beforeinput; suppress echoes in its sequence. */
export function createIntentGuard() {
  let sequence = 0
  let handled: { sequence: number; intent: Intent } | undefined
  return {
    keydown() { handled = undefined; return ++sequence },
    mark(intent: Intent) { handled = { sequence, intent } },
    consume(intent: Intent) {
      const duplicate = handled?.sequence === sequence && handled.intent === intent
      if (!duplicate) handled = undefined
      return duplicate
    },
    clear() { handled = undefined },
  }
}
