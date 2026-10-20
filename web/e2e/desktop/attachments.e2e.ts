import { readFile } from 'node:fs/promises'
import { test, expect } from '../support/fixtures'
import { json } from '../support/project'
import type { Comment } from '../../src/types'

test('pasted image is saved with the comment and included in agent feedback', async ({ page, openFile, api, project }) => {
  await openFile('guide.md')
  await page.locator('#L3 .ln').click({ position: { x: 40, y: 10 } })

  await page.locator('.composer textarea').evaluate(async (textarea) => {
    const canvas = document.createElement('canvas')
    canvas.width = 1
    canvas.height = 1
    const context = canvas.getContext('2d')!
    context.fillStyle = '#336699'
    context.fillRect(0, 0, 1, 1)
    const blob = await new Promise<Blob>((resolve, reject) => {
      canvas.toBlob((value) => value ? resolve(value) : reject(new Error('could not encode test image')), 'image/png')
    })
    const data = new DataTransfer()
    data.items.add(new File([blob], 'clipboard.png', { type: 'image/png' }))
    textarea.dispatchEvent(new ClipboardEvent('paste', { clipboardData: data, bubbles: true, cancelable: true }))
  })

  const composerImage = page.locator('.composer .attachments img')
  await expect(composerImage).toHaveCount(1)
  await expect(composerImage).toHaveJSProperty('naturalWidth', 1)
  await expect(page.locator('.save-state')).toHaveText('下書き保存済み')
  await page.locator('.composer').getByRole('button', { name: '完了', exact: true }).click()
  await expect(page.locator('.composer')).toHaveCount(0)

  const saved = await json<{ comments: Comment[] }>(api, 'GET', `/p/${project.id}/api/comments`)
  expect(saved.comments).toHaveLength(1)
  expect(saved.comments[0].attachments?.map(({ name }) => name)).toEqual(['clipboard.png'])
  const threadImage = page.locator('.thread .attachments img')
  await expect(threadImage).toHaveCount(1)
  await expect(threadImage).toHaveJSProperty('naturalWidth', 1)

  const submitted = await json<{ feedbackPath: string }>(api, 'POST', `/p/${project.id}/api/rounds/submit`, {})
  const feedback = await readFile(submitted.feedbackPath, 'utf8')
  expect(feedback).toContain(`![clipboard.png](<../../attachments/${saved.comments[0].attachments![0].id}>)`)
})
