import { cp, mkdir, readFile, rm, writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import type { APIRequestContext } from '@playwright/test'
import type { Comment } from '../../src/types'
import { baseURL, e2eRoot } from './server'

export interface Project { id: string; dir: string; url: string }
export const specialFiles: Record<string, string> = {
  'crlf.md': '# CRLF\r\n\r\n一行目\r\n二行目\r\n',
  'bom.md': '\uFEFF# BOM\n本文\n',
  'noeol.md': '末尾改行なし',
  'mixed.md': 'a\r\nb\nc\r\n',
  'long.txt': Array.from({ length: 20 }, () => 'x'.repeat(300)).join('\n') + '\n',
  'emoji.md': '𠮷野家🍣 テキスト\n2行目\n',
  'big.md': Array.from({ length: 500 }, (_, i) => `## Section ${i}\n\n段落 ${i}\n\x60\x60\x60go\nvar n = ${i}\n\x60\x60\x60`).join('\n') + '\n',
  'a/b/c/deep.md': '# 深い階層\n本文\n',
}

export async function json<T>(api: APIRequestContext, method: string, url: string, data?: unknown): Promise<T> {
  const res = await api.fetch(url, { method, data })
  if (!res.ok()) throw new Error(`${method} ${url}: ${res.status()} ${await res.text()}`)
  return res.json() as Promise<T>
}

export async function makeProject(name: string, api: APIRequestContext, opts: { git?: boolean } = {}): Promise<Project> {
  if (!/^[a-zA-Z0-9_-]+$/.test(name)) throw new Error(`Invalid fixture name: ${name}`)
  if (opts.git) throw new Error('Git fixture creation requires permission for fixture-local Git mutations')
  const dir = join(e2eRoot, 'projects', name)
  await mkdir(dir)
  await cp(fileURLToPath(new URL('../fixtures/docs', import.meta.url)), dir, { recursive: true })
  for (const [path, content] of Object.entries(specialFiles)) {
    const target = join(dir, path)
    await mkdir(join(target, '..'), { recursive: true })
    await writeFile(target, content)
  }
  const { id, url } = await json<{ id: string; url: string }>(api, 'POST', '/api/projects/open', { path: dir })
  return { id, dir, url: new URL(url, baseURL()).href }
}

export async function removeProject(project: Project, api: APIRequestContext) {
  await json(api, 'POST', `/api/projects/${project.id}/close`)
  await json(api, 'DELETE', `/api/projects/${project.id}`)
  await rm(project.dir, { recursive: true })
}

export async function seedReview(project: Project, api: APIRequestContext) {
  const prefix = `/p/${project.id}/api`
  const file = await json<{ hash: string }>(api, 'GET', `${prefix}/file?path=guide.md`)
  const line = await json<Comment>(api, 'POST', `${prefix}/comments`, {
    scope: 'line', path: 'guide.md', start: 3, end: 3, label: 'must', body: '理由を具体的に説明してください。', hash: file.hash,
  })
  await json(api, 'POST', `${prefix}/comments`, {
    scope: 'line', path: 'guide.md', start: 4, end: 4, label: 'question', body: 'この表現の対象を確認します。', hash: file.hash,
    range: { startLine: 4, startColumn: 0, endLine: 4, endColumn: 2, text: '指摘' },
  })
  // There is no annotation-create API with agent.kind=none. Seed the documented
  // store format while closed, then read it through the real API after reopening.
  await json(api, 'POST', `/api/projects/${project.id}/close`)
  const annotation = {
    id: 'A-1', request: 'Q-1', path: 'guide.md', origStart: 3, origEnd: 3,
    origBlob: line.loc?.blob, anchor: line.anchor, loc: line.loc,
    severity: 'major', confidence: 'high', body: '手順の前提条件を補足してください。',
    evidence: [], state: 'pending', createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z',
  }
  await writeFile(join(project.dir, '.reviewer/annotations.jsonl'), JSON.stringify({ op: 'put', annotation }) + '\n')
  const statePath = join(project.dir, '.reviewer/state.json')
  const state = JSON.parse(await readFile(statePath, 'utf8')) as Record<string, unknown>
  state.nextAnnotation = 2
  await writeFile(statePath, JSON.stringify(state))
  await json(api, 'POST', '/api/projects/open', { path: project.dir })
  const result = await json<{ annotations: unknown[] }>(api, 'GET', `${prefix}/annotations`)
  if (result.annotations.length !== 1) throw new Error('Annotation fixture was not loaded')
}
