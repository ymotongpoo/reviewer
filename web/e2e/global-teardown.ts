import { readFile, unlink } from 'node:fs/promises'
import { join } from 'node:path'
import { e2eRoot, stopServer } from './support/server'

export default async function teardown() {
  const path = join(e2eRoot, 'server.json')
  const state = await readFile(path, 'utf8').catch(() => '')
  if (!state) return
  await stopServer((JSON.parse(state) as { pid: number }).pid, e2eRoot)
  await unlink(path)
}
