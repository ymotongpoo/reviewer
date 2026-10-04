import { execFileSync } from 'node:child_process'
import { mkdir, readFile, readdir, rm, writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import { checkRoot, e2eRoot, isolatedEnv, repoRoot, spawnServer } from './support/server'

export default async function setup() {
  await checkRoot(e2eRoot)
  const entries = await readdir(e2eRoot)
  const marker = join(e2eRoot, '.harness-owned')
  if (entries.length && await readFile(marker, 'utf8').catch(() => '') !== 'reviewer-e2e\n') {
    throw new Error(`Refusing to clear an unowned directory: ${e2eRoot}`)
  }
  const previous = await readFile(join(e2eRoot, 'server.json'), 'utf8').catch(() => '')
  if (previous) {
    const { pid } = JSON.parse(previous) as { pid: number }
    try { process.kill(pid, 0); throw new Error(`Previous server PID ${pid} is still running`) }
    catch (e) { if ((e as NodeJS.ErrnoException).code !== 'ESRCH') throw e }
  }
  for (const entry of entries) await rm(join(e2eRoot, entry), { recursive: true, force: true })
  await writeFile(marker, 'reviewer-e2e\n')
  await mkdir(join(e2eRoot, 'bin'), { recursive: true })
  const env = isolatedEnv(e2eRoot)
  if (process.env.E2E_SKIP_BUILD !== '1') execFileSync('npm', ['run', 'build'], { cwd: join(repoRoot, 'web'), env, stdio: 'inherit' })
  execFileSync('go', ['build', '-o', join(e2eRoot, 'bin/reviewer'), './cmd/reviewer'], { cwd: repoRoot, env, stdio: 'inherit' })
  const port = Number(process.env.E2E_PORT ?? 17777)
  if (!Number.isInteger(port) || port < 1 || port > 65535) throw new Error('Invalid E2E_PORT')
  const server = await spawnServer({ root: e2eRoot, port, token: 'e2e' })
  await writeFile(join(e2eRoot, 'server.json'), JSON.stringify({ pid: server.pid, port }))
  process.env.E2E_BASE_URL = `http://127.0.0.1:${port}`
}
