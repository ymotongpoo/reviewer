import type { DirListing, ProjectSummary, ServerInfo } from './types'
import type { AgentBinding, AgentInfo, AgentRun, AgentRunView, AgentSession, Comment, FileView, Info, RoundChanges, RoundDiff, SubmitResult, TreeFile } from './types'

export class ApiError extends Error {
  constructor(message: string, public status: number) {
    super(message)
  }
}

/** The id of the project this page shows, from /p/<id>/, or null on the picker. */
export const projectId: string | null = location.pathname.match(/^\/p\/([0-9a-f]+)\//)?.[1] ?? null

/** Prefix of the per-project API. */
export const projectBase = projectId ? `/p/${projectId}` : ''

async function request<T>(method: string, url: string, body?: unknown): Promise<T> {
  if (url.startsWith('/api/') && !serverPaths.some((p) => url.startsWith(p))) url = projectBase + url
  const res = await fetch(url, {
    method,
    headers: {
      'X-Reviewer': '1',
      ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}),
    },
    body: body !== undefined ? JSON.stringify(body) : undefined,
    credentials: 'same-origin',
  })
  if (!res.ok) {
    let msg = res.statusText
    try {
      msg = (await res.json()).error ?? msg
    } catch {
      // not JSON
    }
    throw new ApiError(msg, res.status)
  }
  return res.json() as Promise<T>
}

const q = encodeURIComponent

/** Server-wide endpoints that are not scoped to a project. */
const serverPaths = ['/api/server', '/api/projects', '/api/fs']

export const api = {
  project: () => request<Info>('GET', '/api/project'),
  tree: () => request<{ files: TreeFile[] }>('GET', '/api/tree').then((r) => r.files),
  file: (path: string) => request<FileView>('GET', `/api/file?path=${q(path)}`),
  roundChanges: (round: number) => request<RoundChanges>('GET', `/api/rounds/${round}/changes`),
  roundDiff: (round: number, path: string) => request<RoundDiff>('GET', `/api/rounds/${round}/diff?path=${q(path)}`),
  comments: () => request<{ comments: Comment[] }>('GET', '/api/comments').then((r) => r.comments),
  createComment: (c: {
    scope: string
    path?: string
    start?: number
    end?: number
    label: string
    body: string
    hash?: string
  }) => request<Comment>('POST', '/api/comments', c),
  updateComment: (id: string, patch: { label?: string; body?: string; status?: string }) =>
    request<Comment>('PATCH', `/api/comments/${q(id)}`, patch),
  deleteComment: (id: string) => request<{ ok: boolean }>('DELETE', `/api/comments/${q(id)}`),
  addReply: (id: string, body: string) => request<Comment>('POST', `/api/comments/${q(id)}/replies`, { body }),
  updateReply: (id: string, rid: string, body: string) =>
    request<Comment>('PATCH', `/api/comments/${q(id)}/replies/${q(rid)}`, { body }),
  deleteReply: (id: string, rid: string) => request<Comment>('DELETE', `/api/comments/${q(id)}/replies/${q(rid)}`),
  submit: (sendToAgent = false) => request<SubmitResult>('POST', '/api/rounds/submit', { sendToAgent }),
  openRound: () => request<{ ok: boolean }>('POST', '/api/rounds/open'),
  agent: () => request<AgentInfo>('GET', '/api/agent'),
  agentSessions: () => request<{ sessions: AgentSession[] }>('GET', '/api/agent/sessions').then((r) => r.sessions),
  bindAgent: (sessionId: string) =>
    request<{ binding: AgentBinding | null }>('PUT', '/api/agent/binding', { sessionId }).then((r) => r.binding),
  agentSend: (round: number) => request<AgentRun>('POST', '/api/agent/send', { round }),
  agentRuns: (round: number) => request<{ runs: AgentRunView[] }>('GET', `/api/agent/runs?round=${round}`).then((r) => r.runs),
  agentStop: (id: string) => request<{ ok: boolean }>('POST', `/api/agent/runs/${q(id)}/stop`),
  agentAnswer: (id: string, choice: string, approvalId?: string) =>
    request<{ ok: boolean }>('POST', `/api/agent/runs/${q(id)}/approval`, { choice, approvalId }),

  server: () => request<ServerInfo>('GET', '/api/server'),
  projects: () => request<{ projects: ProjectSummary[] }>('GET', '/api/projects').then((r) => r.projects),
  openProject: (path: string) => request<{ id: string; url: string; path: string }>('POST', '/api/projects/open', { path }),
  closeProject: (id: string) => request<{ ok: boolean }>('POST', `/api/projects/${q(id)}/close`),
  forgetProject: (id: string) => request<{ ok: boolean }>('DELETE', `/api/projects/${q(id)}`),
  listDir: (path: string) => request<DirListing>('GET', `/api/fs?path=${q(path)}`),
}
