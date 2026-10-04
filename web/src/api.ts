import type { DirListing, ProjectSummary, ServerInfo } from './types'
import type {
  AgentBinding,
  AgentInfo,
  AgentRun,
  AgentRunView,
  AgentSession,
  AnnotateResult,
  Annotation,
  AnnotationRequest,
  AnnotationRequestView,
  Comment,
  FileView,
  GitCommitResult,
  GitLanguage,
  GitPushResult,
  GitSettings,
  GitStatus,
  Info,
  Preset,
  RequestDiff,
  RoundChanges,
  RoundDiff,
  SubmitResult,
  TextRange,
  TreeFile,
} from './types'

export class NetworkError extends Error {}

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
  }).catch((e) => { throw new NetworkError((e as Error).message) })
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
  roundChanges: (round: number, phase: 'review' | 'agent' = 'agent') =>
    request<RoundChanges>('GET', `/api/rounds/${round}/changes?phase=${phase}`),
  roundDiff: (round: number, path: string, phase: 'review' | 'agent' = 'agent') =>
    request<RoundDiff>('GET', `/api/rounds/${round}/diff?path=${q(path)}&phase=${phase}`),
  comments: () => request<{ comments: Comment[] }>('GET', '/api/comments').then((r) => r.comments),
  createComment: (c: {
    scope: string
    path?: string
    start?: number
    end?: number
    label: string
    body: string
    hash?: string
    range?: TextRange
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
  presets: () => request<{ presets: Preset[] }>('GET', '/api/presets').then((r) => r.presets),
  savePresets: (presets: Omit<Preset, 'origin'>[]) =>
    request<{ presets: Preset[] }>('PUT', '/api/presets', { presets }).then((r) => r.presets),
  annotate: (input: {
    preset?: string
    prompt: string
    paths?: string[]
    target: AnnotationRequest['target']
    sessionId?: string
    sessionTitle?: string
  }) => request<AnnotateResult>('POST', '/api/annotate', input),
  annotationRequests: () => request<{ requests: AnnotationRequest[] }>('GET', '/api/annotate/requests').then((r) => r.requests),
  annotationRequest: (id: string) => request<AnnotationRequestView>('GET', `/api/annotate/requests/${q(id)}`),
  updateAnnotationRequest: (id: string, hidden: boolean) =>
    request<AnnotationRequest>('PATCH', `/api/annotate/requests/${q(id)}`, { hidden }),
  discardAnnotationRequest: (id: string) => request<{ ok: boolean }>('POST', `/api/annotate/requests/${q(id)}/discard`),
  annotationRequestDiff: (id: string, path: string) =>
    request<RequestDiff>('GET', `/api/annotate/requests/${q(id)}/diff?path=${q(path)}`),
  annotations: (all = false) =>
    request<{ annotations: Annotation[] }>('GET', `/api/annotations${all ? '?all=1' : ''}`).then((r) => r.annotations),
  adoptAnnotation: (id: string, patch?: { label?: string; body?: string }) =>
    request<Comment>('POST', `/api/annotations/${q(id)}/adopt`, patch),
  updateAnnotation: (id: string, state: 'pending' | 'dismissed') =>
    request<Annotation>('PATCH', `/api/annotations/${q(id)}`, { state }),
  /** remote and branch pick a one-off push target; empty uses the defaults. */
  gitStatus: (remote = '', branch = '') =>
    request<GitStatus>('GET', `/api/git?remote=${q(remote)}&branch=${q(branch)}`),
  saveGitSettings: (settings: GitSettings) => request<GitSettings>('PUT', '/api/git/settings', settings),
  gitCommit: (message: string, fingerprint: string) =>
    request<GitCommitResult>('POST', '/api/git/commit', { message, fingerprint }),
  gitPush: (remote: string, branch: string) => request<GitPushResult>('POST', '/api/git/push', { remote, branch }),
  saveFile: (path: string, content: string, hash: string) =>
    request<{ ok: boolean; hash: string }>('PUT', '/api/file', { path, content, hash }),

  server: () => request<ServerInfo>('GET', '/api/server'),
  projects: () => request<{ projects: ProjectSummary[] }>('GET', '/api/projects').then((r) => r.projects),
  /** gitLanguage is the commit message language chosen when starting a review. */
  openProject: (path: string, gitLanguage?: GitLanguage) =>
    request<{ id: string; url: string; path: string }>('POST', '/api/projects/open', { path, gitLanguage }),
  closeProject: (id: string) => request<{ ok: boolean }>('POST', `/api/projects/${q(id)}/close`),
  forgetProject: (id: string) => request<{ ok: boolean }>('DELETE', `/api/projects/${q(id)}`),
  listDir: (path: string) => request<DirListing>('GET', `/api/fs?path=${q(path)}`),
}
