import { test, expect } from '../support/fixtures'
import type { AgentModels } from '../../src/types'

test('AI confirmation sends the selected reasoning effort for each model', async ({ page, project, setToken }) => {
  const models: AgentModels = {
    models: [
      { provider: 'codex', providerName: 'Codex', model: 'gpt-test', label: 'GPT Test', reasoning: {
        id: 'reasoningEffort', label: '', default: 'medium',
        choices: ['low', 'medium', 'high', 'xhigh', 'max', 'ultra'].map((id) => ({ id, label: id })),
      } },
      { provider: 'claudeAgent', providerName: 'Claude', model: 'claude-test', label: 'Claude Test', reasoning: {
        id: 'effort', label: '', default: 'high',
        choices: ['low', 'high', 'max'].map((id) => ({ id, label: id })),
      } },
      { provider: 'other', providerName: 'Other', model: 'plain', label: 'Plain' },
    ],
    recent: { provider: 'codex', model: 'gpt-test' },
  }
  await page.route('**/api/agent', (route) => route.fulfill({ json: {
    available: true, kind: 't3', name: 'T3 Code', notify: 'none', autoSend: false,
    canCreate: true, canPickModel: true, active: [],
  } }))
  await page.route('**/api/agent/models', (route) => route.fulfill({ json: models }))
  await page.route('**/api/annotate', (route) => route.fulfill({ json: {
    request: { id: 'Q-1' }, agentRun: { id: 'test-run' },
  } }))
  await setToken()
  await page.getByRole('button', { name: 'AIに確認を依頼' }).click()
  const dialog = page.getByRole('dialog', { name: 'AIに確認を依頼' })
  const model = dialog.getByRole('combobox', { name: 'モデル', exact: true })
  const effort = dialog.getByRole('combobox', { name: '推論レベル', exact: true })
  await expect(effort).toHaveValue('medium')
  await effort.selectOption('ultra')

  await model.selectOption({ label: 'Claude Test' })
  await expect(effort).toHaveValue('high')
  await effort.selectOption('max')
  await model.selectOption({ label: 'Plain' })
  await expect(effort).toHaveCount(0)

  await model.selectOption({ label: 'GPT Test' })
  await expect(effort).toHaveValue('medium')
  await effort.selectOption('xhigh')
  const sent = page.waitForRequest((r) => r.url().endsWith(`/p/${project.id}/api/annotate`) && r.method() === 'POST')
  await dialog.getByRole('button', { name: '確認を依頼', exact: true }).click()
  expect((await sent).postDataJSON()).toMatchObject({
    target: 'new', model: {
      provider: 'codex', model: 'gpt-test', options: [{ id: 'reasoningEffort', value: 'xhigh' }],
    },
  })
  await expect(dialog).toHaveCount(0)
})
