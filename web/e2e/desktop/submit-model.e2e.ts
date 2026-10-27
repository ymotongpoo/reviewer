import { test, expect } from '../support/fixtures'
import { json } from '../support/project'
import { expectReachable } from '../support/git'
import type { AgentModels } from '../../src/types'

const models: AgentModels = {
  models: [
    { provider: 'codex', providerName: 'Codex', model: 'gpt-test', label: 'GPT Test', reasoning: {
      id: 'reasoningEffort', label: '', default: 'medium',
      choices: ['low', 'medium', 'high', 'xhigh'].map((id) => ({ id, label: id })),
    } },
    { provider: 'claudeAgent', providerName: 'Claude', model: 'claude-test', label: 'Claude Test', reasoning: {
      id: 'effort', label: '', default: 'high', choices: ['high', 'max'].map((id) => ({ id, label: id })),
    } },
  ],
  recent: { provider: 'codex', model: 'gpt-test' },
}

test.beforeEach(async ({ page, project, api }) => {
  await json(api, 'POST', `${project.url}api/comments`, { scope: 'project', label: 'must', body: 'Please fix this' })
  await page.route('**/api/agent', (route) => route.fulfill({ json: {
    available: true, kind: 't3', name: 'T3 Code', notify: 'none', autoSend: true,
    canPickModel: true, canConfigureModel: true, active: [],
    binding: { kind: 't3', sessionId: 'bound', title: 'Existing conversation', boundAt: '' },
  } }))
  await page.route('**/api/agent/models', (route) => route.fulfill({ json: models }))
  // Let the real backend submit feedback; sending to an AI is checked at
  // the request boundary here and by the Go integration tests.
  await page.route('**/api/rounds/submit', async (route) => {
    const sending = route.request().postDataJSON().sendToAgent
    const response = await route.fetch({ postData: JSON.stringify({ sendToAgent: false }) })
    expect(response.ok()).toBe(true)
    await route.fulfill({ response, json: { ...await response.json(), ...(sending ? { agentRun: { id: 'test-run' } } : {}) } })
  })
})

for (const width of [1440, 390]) {
  test(`review submission and resend include model and reasoning at ${width}px`, async ({ page, project, setToken }) => {
    await page.setViewportSize({ width, height: 900 })
    await setToken()
    await page.getByRole('button', { name: 'レビューを提出', exact: false }).click()
    const dialog = page.getByRole('dialog', { name: 'ラウンド1を提出', exact: true })
    const model = dialog.getByRole('combobox', { name: 'モデル', exact: true })
    const effort = dialog.getByRole('combobox', { name: '推論レベル', exact: true })
    await expect(model).toHaveValue('')
    await expect(effort).toHaveCount(0)
    await model.selectOption({ label: 'GPT Test' })
    await expect(effort).toHaveValue('medium')
    await effort.selectOption('xhigh')
    await expectReachable(effort)
    const sent = page.waitForRequest((r) => r.url().endsWith('/api/rounds/submit') && r.method() === 'POST')
    await dialog.getByRole('button', { name: '提出する', exact: true }).click()
    expect((await sent).postDataJSON()).toEqual({ sendToAgent: true, model: {
      provider: 'codex', model: 'gpt-test', options: [{ id: 'reasoningEffort', value: 'xhigh' }],
    } })
    await expect(dialog).toContainText('ラウンド1を提出しました')
    await dialog.getByRole('button', { name: '閉じる', exact: true }).click()

    const panel = page.locator('.agent-panel')
    await panel.getByRole('combobox', { name: 'モデル', exact: true }).selectOption({ label: 'Claude Test' })
    await panel.getByRole('combobox', { name: '推論レベル', exact: true }).selectOption('max')
    await page.route('**/api/agent/send', (route) => route.fulfill({ json: {
      id: 'resend-run', kind: 't3', round: 1, sessionId: 'bound', status: 'running', startedAt: '',
    } }))
    const resent = page.waitForRequest((r) => r.url().endsWith(`/p/${project.id}/api/agent/send`))
    await panel.getByRole('button', { name: '送信', exact: true }).click()
    expect((await resent).postDataJSON()).toEqual({ round: 1, model: {
      provider: 'claudeAgent', model: 'claude-test', options: [{ id: 'effort', value: 'max' }],
    } })
  })
}

for (const send of [true, false]) {
  test(`submission ${send ? 'keeps destination defaults' : 'without sending omits a selected model'}`, async ({ page, setToken }) => {
    await setToken()
    await page.getByRole('button', { name: 'レビューを提出', exact: false }).click()
    const dialog = page.getByRole('dialog', { name: 'ラウンド1を提出', exact: true })
    const model = dialog.getByRole('combobox', { name: 'モデル', exact: true })
    await expect(model).toHaveValue('')
    if (!send) {
      await model.selectOption({ label: 'GPT Test' })
      await dialog.getByRole('combobox', { name: '推論レベル', exact: true }).selectOption('xhigh')
      await dialog.getByRole('checkbox', { name: /T3 Code に送信する/ }).uncheck()
      await expect(model).toHaveCount(0)
    }
    const sent = page.waitForRequest((r) => r.url().endsWith('/api/rounds/submit') && r.method() === 'POST')
    await dialog.getByRole('button', { name: '提出する', exact: true }).click()
    expect((await sent).postDataJSON()).toEqual({ sendToAgent: send })
    await expect(dialog).toContainText('ラウンド1を提出しました')
  })
}
