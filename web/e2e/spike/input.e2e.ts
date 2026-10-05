import { test, expect } from '../support/fixtures'
import { frame, record } from './record'

test('S-03 CDP composition commit, Enter and Undo with event sequences', async ({ page, project, setToken }) => {
  await setToken()
  const results = []
  for (const action of ['commit', 'Enter', 'Control+z']) {
    await page.goto(`${project.url}?inputlog=1#/file/noeol.md`)
    await page.locator('#L1 .text').dblclick()
    await page.locator('.inline-input').press('End')
    await page.evaluate(() => window.__reviewerInputLog!.clear())
    const cdp = await page.context().newCDPSession(page)
    try {
      await cdp.send('Input.imeSetComposition', { text: 'にほんご', selectionStart: 4, selectionEnd: 4 })
      if (action === 'commit') await cdp.send('Input.insertText', { text: '日本語' })
      else if (action === 'Enter') {
        await cdp.send('Input.dispatchKeyEvent', { type: 'keyDown', key: 'Enter', code: 'Enter', windowsVirtualKeyCode: 13 })
        await cdp.send('Input.dispatchKeyEvent', { type: 'keyUp', key: 'Enter', code: 'Enter', windowsVirtualKeyCode: 13 })
      } else {
        await cdp.send('Input.dispatchKeyEvent', { type: 'keyDown', key: 'z', code: 'KeyZ', windowsVirtualKeyCode: 90, modifiers: 2 })
        await cdp.send('Input.dispatchKeyEvent', { type: 'keyUp', key: 'z', code: 'KeyZ', windowsVirtualKeyCode: 90, modifiers: 2 })
      }
      await frame(page)
      const state = await page.evaluate(() => ({ value: document.querySelector<HTMLTextAreaElement>('.inline-input')?.value,
        rows: document.querySelectorAll('.code .row').length, events: window.__reviewerInputLog!.entries() }))
      let afterUndo: string | null = null
      if (action === 'commit') {
        await page.keyboard.press('Control+z')
        await frame(page)
        afterUndo = await page.locator('.inline-input').inputValue()
      }
      results.push({ action, ...state, afterUndo })
    } finally {
      await cdp.send('Input.imeSetComposition', { text: '', selectionStart: 0, selectionEnd: 0 })
      await cdp.detach()
    }
  }
  await record('S-03', results)
})

test('S-04 synthetic Android-like keyCode 229 and beforeinput sequences', async ({ page, project, setToken }) => {
  await setToken()
  const results = []
  for (const inputType of ['insertLineBreak', 'deleteContentBackward']) {
    for (const cancelable of [true, false]) {
      await page.goto(`${project.url}?inputlog=1#/file/guide.md`)
      await page.locator('#L3 .text').dblclick()
      await expect(page.locator('.inline-input')).toBeFocused()
      const result = await page.locator('.inline-input').evaluate((el, { inputType, cancelable }) => {
        const input = el as HTMLTextAreaElement
        const caret = inputType === 'deleteContentBackward' ? 0 : input.value.length
        input.setSelectionRange(caret, caret)
        window.__reviewerInputLog!.clear()
        const before = { value: input.value, rows: document.querySelectorAll('.code .row').length }
        const key = new KeyboardEvent('keydown', { key: 'Unidentified', bubbles: true, cancelable: true })
        Object.defineProperty(key, 'keyCode', { value: 229 })
        input.dispatchEvent(key)
        const event = new InputEvent('beforeinput', { inputType, cancelable, bubbles: true })
        input.dispatchEvent(event)
        return { before, keyPrevented: key.defaultPrevented, beforeInputPrevented: event.defaultPrevented }
      }, { inputType, cancelable })
      await frame(page)
      results.push({ inputType, cancelable, ...result, after: await page.evaluate(() => ({
        value: document.querySelector<HTMLTextAreaElement>('.inline-input')?.value,
        rows: document.querySelectorAll('.code .row').length, events: window.__reviewerInputLog!.entries(),
      })) })
    }
  }
  await record('S-04', { limitation: 'Synthetic events have no native default action; this measures handlers only.', results })
})
