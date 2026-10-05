import { spawn } from 'node:child_process'
import { mkdir, open, readFile, realpath, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { basename, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

export const repoRoot = fileURLToPath(new URL('../../..', import.meta.url))
export const e2eRoot = resolve(process.env.E2E_ROOT ?? join(tmpdir(), 'reviewer-e2e'))
export const baseURL = () => process.env.E2E_BASE_URL ?? `http://127.0.0.1:${process.env.E2E_PORT ?? 17777}`

export async function checkRoot(root: string) {
  if (basename(root) !== 'reviewer-e2e' || root !== resolve(root)) throw new Error(`Unsafe E2E_ROOT: ${root}`)
  await mkdir(root, { recursive: true })
  if (await realpath(root) !== root) throw new Error(`E2E_ROOT must not contain symlinks: ${root}`)
}

export function isolatedEnv(root: string): NodeJS.ProcessEnv {
  return {
    ...process.env,
    XDG_CONFIG_HOME: join(root, 'config'), XDG_STATE_HOME: join(root, 'state'),
    XDG_DATA_HOME: join(root, 'data'), XDG_CACHE_HOME: join(root, 'cache'),
    GOMODCACHE: process.env.GOMODCACHE ?? join(tmpdir(), 'reviewer-e2e-go-mod'),
    GOCACHE: process.env.GOCACHE ?? join(tmpdir(), 'reviewer-e2e-go-cache'),
    GIT_CONFIG_NOSYSTEM: '1', GIT_CONFIG_GLOBAL: '/dev/null',
    GIT_AUTHOR_NAME: 'E2E', GIT_AUTHOR_EMAIL: 'e2e@example.invalid',
    GIT_COMMITTER_NAME: 'E2E', GIT_COMMITTER_EMAIL: 'e2e@example.invalid',
  }
}

export interface ServerOptions { port: number; token: string; root: string }
export interface ServerHandle { pid: number; port: number; stop(): Promise<void>; restart(): Promise<ServerHandle> }

export async function stopServer(pid: number, root: string) {
  // Never signal a recycled PID belonging to a different process.
  const cmd = await readFile(`/proc/${pid}/cmdline`, 'utf8').catch(() => '')
  if (!cmd) return
  if (!cmd.startsWith(join(root, 'bin/reviewer') + '\0serve\0')) throw new Error(`PID ${pid} is not this E2E server`)
  process.kill(pid, 'SIGTERM')
  for (let i = 0; i < 100; i++) {
    if (!(await readFile(`/proc/${pid}/cmdline`, 'utf8').catch(() => ''))) return
    await new Promise((r) => setTimeout(r, 50))
  }
  throw new Error(`E2E server ${pid} did not stop`)
}

export async function spawnServer(opts: ServerOptions): Promise<ServerHandle> {
  const { root, port, token } = opts
  await checkRoot(root)
  for (const dir of ['projects', 'config/reviewer', 'state', 'data', 'cache']) await mkdir(join(root, dir), { recursive: true })
  const config = join(root, 'config/reviewer/config.toml')
  await writeFile(config, `bind = "127.0.0.1"\nroots = [${JSON.stringify(join(root, 'projects'))}]\n[agent]\nkind = "none"\n`)
  const log = await open(join(root, `server-${port}.log`), 'a')
  const child = spawn(join(root, 'bin/reviewer'), ['serve', '--token', token, '--port', String(port), '--bind', '127.0.0.1', '--config', config], {
    cwd: root, env: isolatedEnv(root), stdio: ['ignore', log.fd, log.fd],
  })
  let failure: Error | undefined
  child.on('error', (e) => { failure = e })
  await log.close()
  const deadline = Date.now() + 30_000
  try {
    while (Date.now() < deadline) {
      if (failure) throw failure
      if (child.exitCode !== null) throw new Error(`Server exited ${child.exitCode}: ${await readFile(join(root, `server-${port}.log`), 'utf8')}`)
      const res = await fetch(`http://127.0.0.1:${port}/api/server`, {
        headers: { Authorization: `Bearer ${token}` }, signal: AbortSignal.timeout(200),
      }).catch(() => null)
      if (res?.status === 200) {
        // An occupied port must not be mistaken for our server becoming ready.
        await new Promise((r) => setTimeout(r, 100))
        if (child.exitCode !== null) throw new Error('Server exited during readiness check')
        const pid = child.pid!
        child.unref()
        return { pid, port, stop: () => stopServer(pid, root), restart: async () => { await stopServer(pid, root); return spawnServer(opts) } }
      }
      await new Promise((r) => setTimeout(r, 200))
    }
    throw new Error('Server readiness timed out after 30 seconds')
  } catch (e) {
    if (child.pid) await stopServer(child.pid, root)
    throw e
  }
}
