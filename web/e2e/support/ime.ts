import { test, expect } from './fixtures'

/** CDP and synthetic events verify ordering, not the Gboard/device gate. */
export function imeCases(touch: boolean) {
  test.beforeEach(async ({ openFile, page }) => {
    await openFile('mixed.md')
    const edit = page.getByRole('button', { name: '行2を編集', exact: true })
    if (touch) await edit.tap()
    else { await page.locator('#L2').hover(); await edit.click() }
    await expect(page.locator('.inline-input')).toHaveValue('b')
    await page.locator('.inline-input').press('End')
  })

  test('CDP commit and trailing input create one history entry, then Enter splits once', async ({ page }) => {
    const cdp = await page.context().newCDPSession(page)
    await cdp.send('Input.imeSetComposition', { text: 'にほんご', selectionStart: 4, selectionEnd: 4 })
    await expect(page.locator('#L2')).toHaveAttribute('data-composing', '')
    await cdp.send('Input.insertText', { text: '日本語' })
    await page.locator('.inline-input').dispatchEvent('input', { inputType: 'insertText', data: '日本語' })
    await expect(page.locator('.inline-input')).toHaveValue('b日本語')
    await page.keyboard.press('Control+z')
    await expect(page.locator('.inline-input')).toHaveValue('b')
    await page.keyboard.press('Control+Shift+z')
    await expect(page.locator('.inline-input')).toHaveValue('b日本語')
    await page.keyboard.press('Enter')
    await expect(page.locator('.code .row')).toHaveCount(4)
    await expect(page.locator('#L2 .text')).toContainText('b日本語')
    await expect(page.locator('#L3 .inline-input')).toHaveValue('')
    await cdp.detach()
  })

  test('Enter during CDP composition confirms without inserting a newline', async ({ page }) => {
    const cdp = await page.context().newCDPSession(page)
    await cdp.send('Input.imeSetComposition', { text: 'にほんご', selectionStart: 4, selectionEnd: 4 })
    // Send the IME key without Playwright's additional text='\r' insertion.
    await cdp.send('Input.dispatchKeyEvent', { type: 'keyDown', key: 'Enter', code: 'Enter', windowsVirtualKeyCode: 13 })
    await cdp.send('Input.dispatchKeyEvent', { type: 'keyUp', key: 'Enter', code: 'Enter', windowsVirtualKeyCode: 13 })
    await expect(page.locator('.code .row')).toHaveCount(3)
    await cdp.send('Input.insertText', { text: '日本語' })
    await expect(page.locator('.inline-input')).toHaveValue('b日本語')
    await expect(page.locator('.code .row')).toHaveCount(3)
    await page.keyboard.press('Control+z')
    await expect(page.locator('.inline-input')).toHaveValue('b')
    await cdp.detach()
  })

  for (const action of ['Tab', 'Shift+Tab', 'Control+z', 'Control+s', 'review', 'row', 'pencil'] as const) {
    test(`composition defers ${action} until commit, once`, async ({ page }) => {
      // Make review available without including this edit in the composition's history entry.
      await page.locator('.inline-input').fill('  base')
      await page.locator('.inline-input').press('End')
      const cdp = await page.context().newCDPSession(page)
      await cdp.send('Input.imeSetComposition', { text: 'にほんご', selectionStart: 4, selectionEnd: 4 })
      if (action === 'review') {
        // Keep the composition alive so the click exercises the pending action.
        await page.locator('.edit-bar').getByRole('button', { name: '差分を確認' }).dispatchEvent('click')
      } else if (action === 'row') {
        if (touch) await page.locator('#L3 .text').tap()
        else await page.locator('#L3 .text').click()
      } else if (action === 'pencil') {
        if (touch) await page.getByRole('button', { name: '行3を編集', exact: true }).tap()
        else { await page.locator('#L3').hover(); await page.getByRole('button', { name: '行3を編集', exact: true }).click() }
      } else await page.keyboard.press(action)
      await expect(page.locator('#L2 .inline-input')).toHaveCount(1)
      await expect(page.locator('.edit-review')).toHaveCount(0)
      await expect(page.locator('.inline-input')).toHaveValue('  baseにほんご')
      await cdp.send('Input.insertText', { text: '日本語' })
      if (action === 'review' || action === 'Control+s') {
        await expect(page.locator('.edit-review')).toBeVisible()
        await expect(page.locator('.edit-review-body')).toContainText('base日本語')
      } else if (action === 'row' || action === 'pencil') {
        await expect(page.locator('#L3 .inline-input')).toBeFocused()
        await expect(page.locator('#L2 .text')).toContainText('base日本語')
        await page.keyboard.press('Control+z')
        await expect(page.locator('#L2 .inline-input')).toHaveValue('  base')
      } else if (action === 'Control+z') {
        await expect(page.locator('.inline-input')).toHaveValue('  base')
        await page.keyboard.press('Control+Shift+z')
        await expect(page.locator('.inline-input')).toHaveValue('  base日本語')
      } else {
        await expect(page.locator('.inline-input')).toHaveValue(action === 'Tab' ? '  base日本語\t' : 'base日本語')
        await page.keyboard.press('Control+z')
        await expect(page.locator('.inline-input')).toHaveValue('  base日本語')
        await page.keyboard.press('Control+z')
        await expect(page.locator('.inline-input')).toHaveValue('  base')
      }
      await cdp.detach()
    })
  }

  test('ordinary Enter arriving before compositionend is deferred and splits once', async ({ page }) => {
    await page.locator('.inline-input').evaluate((el) => {
      const ta = el as HTMLTextAreaElement
      ta.dispatchEvent(new CompositionEvent('compositionstart', { bubbles: true }))
      ta.value = 'b日本語'
      ta.setSelectionRange(ta.value.length, ta.value.length)
      ta.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, cancelable: true, key: 'Enter' }))
    })
    await expect(page.locator('.code .row')).toHaveCount(3)
    await page.locator('.inline-input').evaluate((el) => {
      el.dispatchEvent(new CompositionEvent('compositionend', { bubbles: true }))
      el.dispatchEvent(new InputEvent('input', { bubbles: true, inputType: 'insertText' }))
    })
    await expect(page.locator('.code .row')).toHaveCount(4)
    await expect(page.locator('#L3 .inline-input')).toHaveValue('')
    await page.keyboard.press('Control+z')
    await expect(page.locator('#L2 .inline-input')).toHaveValue('b日本語')
    await page.keyboard.press('Control+z')
    await expect(page.locator('#L2 .inline-input')).toHaveValue('b')
  })

  test('multiline composition with a trailing input and pending review is committed once', async ({ page }) => {
    await page.locator('.inline-input').fill('base')
    await page.locator('.inline-input').dispatchEvent('compositionstart')
    await page.locator('.edit-bar').getByRole('button', { name: '差分を確認' }).dispatchEvent('click')
    await page.locator('.inline-input').evaluate((el) => {
      const ta = el as HTMLTextAreaElement
      ta.value = 'base日本\n語'
      ta.setSelectionRange(ta.value.length, ta.value.length)
      ta.dispatchEvent(new CompositionEvent('compositionend', { bubbles: true }))
      ta.dispatchEvent(new InputEvent('input', { bubbles: true, inputType: 'insertCompositionText' }))
    })
    await expect(page.locator('.code .row')).toHaveCount(4)
    await expect(page.locator('.edit-review')).toBeVisible()
    await expect(page.locator('#L2 .text')).toContainText('base日本')
    await expect(page.locator('#L3 .text')).toContainText('語')
    await page.getByRole('button', { name: '編集に戻る' }).click()
    await page.locator('#L3 .edit-marker').dispatchEvent('click')
    await page.keyboard.press('Control+z')
    await expect(page.locator('.code .row')).toHaveCount(3)
    await expect(page.locator('#L2 .inline-input')).toHaveValue('base')
  })

  test('empty-line and repeated boundary Backspace each join only one line', async ({ page }) => {
    await page.locator('.inline-input').fill('')
    for (const rows of [2, 1]) {
      if (rows === 1) await page.locator('#L2 .edit-marker').dispatchEvent('click')
      await page.locator('.inline-input').evaluate((el) => {
        const ta = el as HTMLTextAreaElement
        ta.setSelectionRange(0, 0)
        ta.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, key: 'Unidentified', keyCode: 229, repeat: true }))
        ta.dispatchEvent(new InputEvent('beforeinput', { bubbles: true, cancelable: false, inputType: 'deleteContentBackward' }))
        ta.dispatchEvent(new InputEvent('input', { bubbles: true, inputType: 'deleteContentBackward' }))
      })
      await expect(page.locator('.code .row')).toHaveCount(rows)
    }
    await expect(page.locator('.inline-input')).toHaveValue('ac')
    await page.locator('.inline-input').press('Home')
    await page.locator('.inline-input').dispatchEvent('beforeinput', { inputType: 'deleteContentBackward', cancelable: false })
    await page.evaluate(() => new Promise<void>((resolve) => requestAnimationFrame(() => resolve())))
    await expect(page.locator('.inline-input')).toHaveValue('ac')
  })

  test('composition beforeinput is never canceled, including native history', async ({ page }) => {
    const prevented = await page.locator('.inline-input').evaluate((el) => {
      const ta = el as HTMLTextAreaElement
      ta.dispatchEvent(new CompositionEvent('compositionstart', { bubbles: true }))
      const results = ['insertCompositionText', 'deleteCompositionText', 'insertLineBreak', 'deleteContentBackward', 'deleteContentForward', 'historyUndo', 'historyRedo'].map((inputType) => {
        const e = new InputEvent('beforeinput', { bubbles: true, cancelable: true, isComposing: true, inputType })
        ta.dispatchEvent(e)
        return e.defaultPrevented
      })
      ta.value = 'b日本語'
      ta.setSelectionRange(ta.value.length, ta.value.length)
      ta.dispatchEvent(new CompositionEvent('compositionend', { bubbles: true }))
      ta.dispatchEvent(new InputEvent('input', { bubbles: true, inputType: 'insertCompositionText' }))
      return results
    })
    expect(prevented).toEqual(Array(7).fill(false))
    await expect(page.locator('.inline-input')).toHaveValue('b日本語')
    await page.keyboard.press('Control+z')
    await expect(page.locator('.inline-input')).toHaveValue('b')
  })

  for (const cancelable of [true, false]) {
    for (const type of ['insertLineBreak', 'insertParagraph', 'deleteContentBackward', 'deleteContentForward']) {
      for (const delivery of type.startsWith('delete') && !cancelable ? ['input', 'frame'] : ['input']) {
        test(`Android 229 ${type}, cancelable=${cancelable}, ${delivery}: applied once`, async ({ page }) => {
          const prevented = await page.locator('.inline-input').evaluate((el, { type, cancelable, delivery }) => {
            const ta = el as HTMLTextAreaElement
            const at = type === 'deleteContentBackward' ? 0 : ta.value.length
            ta.setSelectionRange(at, at)
            ta.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, cancelable: true, key: 'Unidentified', keyCode: 229 }))
            const before = new InputEvent('beforeinput', { bubbles: true, cancelable, inputType: type })
            ta.dispatchEvent(before)
            // Synthetic events have no browser default; model the native newline explicitly.
            if (!before.defaultPrevented && type.startsWith('insert')) {
              ta.value += '\n'
              ta.setSelectionRange(ta.value.length, ta.value.length)
            }
            if (!before.defaultPrevented && delivery === 'input') ta.dispatchEvent(new InputEvent('input', { bubbles: true, inputType: type }))
            return before.defaultPrevented
          }, { type, cancelable, delivery })
          expect(prevented).toBe(cancelable)
          if (type.startsWith('insert')) {
            await expect(page.locator('.code .row')).toHaveCount(4)
            await expect(page.locator('#L3 .inline-input')).toHaveValue('')
          } else {
            await expect(page.locator('.code .row')).toHaveCount(2)
            await expect(page.locator('.inline-input')).toHaveValue(type === 'deleteContentBackward' ? 'ab' : 'bc')
          }
          await page.evaluate(() => new Promise<void>((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve()))))
          await page.keyboard.press('Control+z')
          await expect(page.locator('.code .row')).toHaveCount(3)
          await expect(page.locator('#L2 .inline-input')).toHaveValue('b')
        })
      }
    }

    for (const [key, type] of [['Enter', 'insertLineBreak'], ['Backspace', 'deleteContentBackward'], ['Delete', 'deleteContentForward'], ['z', 'historyUndo']] as const) {
      test(`keydown + ${type} echo, cancelable=${cancelable}: applied once`, async ({ page }) => {
        if (key === 'z') await page.locator('.inline-input').fill('changed')
        await page.locator('.inline-input').evaluate((el, { key, type, cancelable }) => {
          const ta = el as HTMLTextAreaElement
          const at = key === 'Backspace' ? 0 : ta.value.length
          ta.setSelectionRange(at, at)
          ta.dispatchEvent(new KeyboardEvent('keydown', { bubbles: true, cancelable: true, key, ctrlKey: key === 'z' }))
          const before = new InputEvent('beforeinput', { bubbles: true, cancelable, inputType: type })
          ta.dispatchEvent(before)
          ta.dispatchEvent(new InputEvent('beforeinput', { bubbles: true, cancelable, inputType: type }))
          if (!before.defaultPrevented) {
            ta.value = 'unwanted native mutation'
            ta.dispatchEvent(new InputEvent('input', { bubbles: true, inputType: type }))
          }
        }, { key, type, cancelable })
        await expect(page.locator('.code .row')).toHaveCount(key === 'Enter' ? 4 : key === 'z' ? 3 : 2)
        await expect(page.locator('.inline-input')).toHaveValue(key === 'Enter' ? '' : key === 'z' ? 'b' : key === 'Backspace' ? 'ab' : 'bc')
        if (key !== 'z') {
          await page.keyboard.press('Control+z')
          await expect(page.locator('.code .row')).toHaveCount(3)
          await expect(page.locator('.inline-input')).toHaveValue('b')
        }
      })
    }

    test(`native history undo/redo, cancelable=${cancelable}`, async ({ page }) => {
      await page.locator('.inline-input').fill('changed')
      for (const [inputType, value] of [['historyUndo', 'b'], ['historyRedo', 'changed']]) {
        await page.locator('.inline-input').evaluate((el, { inputType, cancelable }) => {
          const ta = el as HTMLTextAreaElement
          const e = new InputEvent('beforeinput', { bubbles: true, cancelable, inputType })
          ta.dispatchEvent(e)
          if (!e.defaultPrevented) {
            ta.value = 'browser history'
            ta.dispatchEvent(new InputEvent('input', { bubbles: true, inputType }))
          }
        }, { inputType, cancelable })
        await expect(page.locator('.inline-input')).toHaveValue(value)
      }
    })
  }

  for (const changed of ['value', 'caret']) {
    test(`uncancelable boundary delete does not join after native ${changed} changes`, async ({ page }) => {
      await page.locator('.inline-input').evaluate((el, changed) => {
        const ta = el as HTMLTextAreaElement
        ta.setSelectionRange(0, 0)
        ta.dispatchEvent(new InputEvent('beforeinput', { bubbles: true, cancelable: false, inputType: 'deleteContentBackward' }))
        if (changed === 'value') ta.value = 'native'
        ta.setSelectionRange(1, 1)
        ta.dispatchEvent(new InputEvent('input', { bubbles: true, inputType: 'deleteContentBackward' }))
      }, changed)
      await page.evaluate(() => new Promise<void>((resolve) => requestAnimationFrame(() => resolve())))
      await expect(page.locator('.code .row')).toHaveCount(3)
      await expect(page.locator('.inline-input')).toHaveValue(changed === 'value' ? 'native' : 'b')
    })
  }

  test('native multiline input and paste retain both surrounding lines', async ({ page }) => {
    await page.keyboard.insertText('日本\n語')
    await expect(page.locator('.code .row')).toHaveCount(4)
    await expect(page.locator('#L2 .text')).toContainText('b日本')
    await expect(page.locator('#L3 .inline-input')).toHaveValue('語')
    await page.keyboard.press('Control+z')
    await expect(page.locator('#L2 .inline-input')).toHaveValue('b')
    await page.locator('.inline-input').evaluate((el) => {
      const data = new DataTransfer()
      data.setData('text/plain', '貼り\n付け')
      el.dispatchEvent(new ClipboardEvent('paste', { bubbles: true, cancelable: true, clipboardData: data }))
    })
    await expect(page.locator('#L3 .inline-input')).toHaveValue('付け')
    await expect(page.locator('#L1 .text')).toContainText('a')
    await expect(page.locator('#L4 .text')).toContainText('c')
  })
}
