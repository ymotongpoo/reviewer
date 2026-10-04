import { batch, computed, signal } from '@preact/signals'
import { api, ApiError, projectBase, projectId } from './api'
import { connectionGeneration, createConnection } from './connection'
import type {
  AgentInfo,
  AgentRunView,
  Annotation,
  AnnotationRequestView,
  AnnotationSeverity,
  Comment,
  GitStatus,
  Info,
  Preset,
  ServerEvent,
  TextRange,
  TreeFile,
} from './types'

export const info = signal<Info | null>(null)
export const tree = signal<TreeFile[]>([])
export const comments = signal<Comment[]>([])
export const fatal = signal<string | null>(null)
export const agentInfo = signal<AgentInfo | null>(null)
/** Runs of the round shown in the agent panel, keyed by run id. */
export const agentRuns = signal<AgentRunView[]>([])
export const agentRound = signal<number>(0)
export const presets = signal<Preset[]>([])
export const annotations = signal<Annotation[]>([])
export const annotationRequests = signal<AnnotationRequestView[]>([])
/** Git state with the default push target, for the header button. */
export const gitStatus = signal<GitStatus | null>(null)
/** Bumped when the working tree or the repository may have changed. */
export const gitVersion = signal(0)

export type CommentListKind = 'human' | 'ai'
export interface CommentListSelection {
  path: string
  kind: CommentListKind
}
export const commentListSelection = signal<CommentListSelection | null>(null)

export function showCommentList(path: string, kind: CommentListKind) {
  const current = commentListSelection.value
  commentListSelection.value = current?.path === path && current.kind === kind ? null : { path, kind }
}

export function closeCommentList() {
  commentListSelection.value = null
}

function stored<T>(key: string, fallback: T): T {
  try {
    const value = localStorage.getItem(key)
    return value === null ? fallback : (JSON.parse(value) as T)
  } catch {
    return fallback
  }
}

export type SeverityFilter = 'all' | AnnotationSeverity
export const annotationSeverity = signal<SeverityFilter>(stored<SeverityFilter>('reviewer.annotationSeverity', 'all'))
export const showDismissedAnnotations = signal<boolean>(stored<boolean>('reviewer.showDismissedAnnotations', false))

export function setAnnotationSeverity(value: SeverityFilter) {
  annotationSeverity.value = value
  try {
    localStorage.setItem('reviewer.annotationSeverity', JSON.stringify(value))
  } catch {
    // storage unavailable
  }
}

export function setShowDismissedAnnotations(value: boolean) {
  showDismissedAnnotations.value = value
  try {
    localStorage.setItem('reviewer.showDismissedAnnotations', JSON.stringify(value))
  } catch {
    // storage unavailable
  }
}

export const visibleAnnotations = computed(() => {
  const hidden = new Set(annotationRequests.value.filter((r) => r.hidden).map((r) => r.id))
  return annotations.value.filter(
    (a) =>
      !hidden.has(a.request) &&
      a.state !== 'adopted' &&
      !a.adoptedAs &&
      (a.state === 'pending' || showDismissedAnnotations.value) &&
      (annotationSeverity.value === 'all' || a.severity === annotationSeverity.value),
  )
})

/** Bumped when files change on disk; views refetch when their path is listed. */
export const fileVersion = signal<{ n: number; paths: string[] }>({ n: 0, paths: [] })

export interface Toast {
  id: number
  text: string
  kind: 'info' | 'error' | 'success'
}
export const toasts = signal<Toast[]>([])
let toastSeq = 0
export function toast(text: string, kind: Toast['kind'] = 'info') {
  const id = ++toastSeq
  toasts.value = [...toasts.value, { id, text, kind }]
  setTimeout(() => (toasts.value = toasts.value.filter((t) => t.id !== id)), kind === 'error' ? 8000 : 4000)
}

/** Response of the agent that arrived while this page was open. */
export const responseBanner = signal<number | null>(null)

/** The comment whose editor is open, or a new-comment target. */
export type EditTarget =
  | { kind: 'comment'; id: string }
  | {
      kind: 'new'
      scope: 'line' | 'file' | 'project'
      path?: string
      start?: number
      end?: number
      hash?: string
      /** Selected text of a line comment, which makes it a range comment on the lines start..end. */
      range?: TextRange
      /** Set once the draft has been saved; the thread is then hidden in favor of the editor. */
      createdId?: string
    }
export const editing = signal<EditTarget | null>(null)

export const labels = computed(() => info.value?.labels ?? [])

export const draftCount = computed(
  () =>
    comments.value.filter((c) => c.status === 'draft').length +
    comments.value.reduce((n, c) => n + c.replies.filter((r) => r.draft).length, 0),
)

export function commentsFor(path: string) {
  return comments.value.filter((c) => c.path === path)
}

const requests = new Map<string, number>()

function invalidateRequest(key: string) {
  const seq = (requests.get(key) ?? 0) + 1
  requests.set(key, seq)
  return seq
}

async function prepareLatest<T>(key: string, fetch: () => Promise<T>, apply: (value: T) => void) {
  const seq = invalidateRequest(key)
  const gen = connectionGeneration.value
  const value = await fetch()
  return () => {
    if (requests.get(key) === seq && connectionGeneration.value === gen) apply(value)
  }
}

export async function latestOnly<T>(key: string, fetch: () => Promise<T>, apply: (value: T) => void) {
  const commit = await prepareLatest(key, fetch, apply)
  commit()
}

async function loadAgent() {
  const round = agentRound.value
  const [agent, runs] = await Promise.all([api.agent(), round > 0 ? api.agentRuns(round) : Promise.resolve([])])
  return { agent, runs, round }
}
function setAgent(value: Awaited<ReturnType<typeof loadAgent>>) {
  agentInfo.value = value.agent
  if (value.round === agentRound.value) agentRuns.value = value.runs
}
async function loadAnnotationRequests() {
  const list = await api.annotationRequests()
  return Promise.all(list.map(async (r) => {
    try { return await api.annotationRequest(r.id) }
    catch { return { ...r, runs: [] } }
  }))
}

export const refreshInfo = () => latestOnly('info', api.project, (v) => { info.value = v })
export const refreshTree = () => latestOnly('tree', api.tree, (v) => { tree.value = v })
export const refreshComments = () => latestOnly('comments', api.comments, (v) => { comments.value = v })
export const refreshAgent = () => latestOnly('agent', loadAgent, setAgent)
export const refreshPresets = () => latestOnly('presets', api.presets, (v) => { presets.value = v })
export const refreshAnnotations = () => latestOnly('annotations', () => api.annotations(true), (v) => { annotations.value = v })
export const refreshAnnotationRequests = () => latestOnly('annotationRequests', loadAnnotationRequests, (v) => { annotationRequests.value = v })
export const refreshGit = () => latestOnly('git', () => info.value?.git ? api.gitStatus() : Promise.resolve(null), (v) => { gitStatus.value = v })

export async function showAgentRound(round: number) {
  if (agentRound.value === round) return
  agentRound.value = round
  await refreshAgent()
}

const responseSeenKey = `reviewer.responseSeen.${projectId}`
export function markResponseSeen() {
  const importedAt = info.value?.response?.importedAt ?? ''
  try {
    const previous = localStorage.getItem(responseSeenKey)
    if (previous === null || importedAt > previous) localStorage.setItem(responseSeenKey, importedAt)
  } catch { /* storage unavailable */ }
}
export function dismissResponseBanner() {
  pendingResponseRound = undefined
  markResponseSeen()
  responseBanner.value = null
}
export function checkResponseBanner() {
  const importedAt = info.value?.response?.importedAt ?? ''
  try {
    const previous = localStorage.getItem(responseSeenKey)
    if (previous === null) markResponseSeen()
    else if (importedAt > previous) responseBanner.value = info.value!.round
  } catch { /* storage unavailable */ }
}

let allSeq = 0
export async function refreshAll(gen = connectionGeneration.value) {
  const seq = ++allSeq
  const project = api.project()
  const commits = await Promise.all([
    prepareLatest('info', () => project, (v) => { info.value = v }),
    prepareLatest('git', async () => (await project).git ? api.gitStatus().catch(() => null) : null, (v) => { gitStatus.value = v }),
    prepareLatest('tree', api.tree, (v) => { tree.value = v }),
    prepareLatest('comments', api.comments, (v) => { comments.value = v }),
    prepareLatest('agent', loadAgent, setAgent),
    prepareLatest('presets', api.presets, (v) => { presets.value = v }),
    prepareLatest('annotations', () => api.annotations(true), (v) => { annotations.value = v }),
    prepareLatest('annotationRequests', loadAnnotationRequests, (v) => { annotationRequests.value = v }),
  ])
  if (gen !== connectionGeneration.value || seq !== allSeq) return
  batch(() => {
    for (const commit of commits) commit()
    checkResponseBanner()
    gitVersion.value++
    fileVersion.value = { n: fileVersion.value.n + 1, paths: ['*'] }
  })
}

/** Appends a streamed event to its run, or refetches when the run is unknown. */
function applyAgentEvent(ev: ServerEvent) {
  const runs = agentRuns.value
  const i = runs.findIndex((r) => r.id === ev.run)
  if (!ev.agent || i < 0) {
    lazyAgent()
    return
  }
  const r = runs[i]
  invalidateRequest('agent')
  const next = { ...r, events: [...r.events, ev.agent] }
  if (ev.agent.type === 'approval') next.pending = ev.agent.approval
  if (ev.agent.type === 'answered') next.pending = undefined
  agentRuns.value = [...runs.slice(0, i), next, ...runs.slice(i + 1)]
  if (ev.agent.type === 'done' || ev.agent.type === 'error') lazyAgent()
}

function applyAnnotationAgentEvent(ev: ServerEvent) {
  const request = annotationRequests.value.find((r) => r.id === ev.request)
  const i = request?.runs.findIndex((r) => r.id === ev.run) ?? -1
  if (!request || !ev.agent || i < 0) {
    lazyAnnotationRequests()
    return
  }
  const run = request.runs[i]
  invalidateRequest('annotationRequests')
  const next = { ...run, events: [...run.events, ev.agent] }
  if (ev.agent.type === 'approval') next.pending = ev.agent.approval
  if (ev.agent.type === 'answered') next.pending = undefined
  const runs = [...request.runs.slice(0, i), next, ...request.runs.slice(i + 1)]
  annotationRequests.value = annotationRequests.value.map((r) => (r.id === request.id ? { ...r, runs } : r))
  if (ev.agent.type === 'done' || ev.agent.type === 'error') {
    lazyAnnotationRequests()
    lazyAnnotations()
  }
}

export function upsertComment(c: Comment) {
  invalidateRequest('comments')
  const list = comments.value
  const i = list.findIndex((x) => x.id === c.id)
  comments.value = i >= 0 ? [...list.slice(0, i), c, ...list.slice(i + 1)] : [...list, c]
}
export function removeComment(id: string) {
  invalidateRequest('comments')
  comments.value = comments.value.filter((c) => c.id !== id)
}

function debounced(fn: () => unknown, ms: number) {
  let t: ReturnType<typeof setTimeout> | undefined
  return () => {
    clearTimeout(t)
    t = setTimeout(fn, ms)
  }
}

const lazyComments = debounced(() => refreshComments().catch(() => {}), 150)
const lazyTree = debounced(() => refreshTree().catch(() => {}), 150)
let pendingResponseRound: number | undefined
const lazyInfo = debounced(() => {
  const round = pendingResponseRound
  if (round === undefined) return
  void latestOnly('info', api.project, (v) => {
    info.value = v
    if (pendingResponseRound === round && v.round === round) responseBanner.value = round
  }).catch(() => {})
}, 150)
const lazyAgent = debounced(() => refreshAgent().catch(() => {}), 150)
const lazyPresets = debounced(() => refreshPresets().catch(() => {}), 150)
const lazyAnnotations = debounced(() => refreshAnnotations().catch(() => {}), 150)
const lazyAnnotationRequests = debounced(() => refreshAnnotationRequests().catch(() => {}), 150)
// git status runs several commands; coalesce bursts of file events.
const lazyGit = debounced(() => {
  gitVersion.value++
  refreshGit().catch(() => {})
}, 800)

function refreshRoundInfo() {
  void latestOnly('info', api.project, (v) => { info.value = v; markResponseSeen() }).catch(() => {})
}

export function dispatchEvent(ev: ServerEvent) {
  switch (ev.type) {
    case 'files':
      fileVersion.value = { n: fileVersion.value.n + 1, paths: ev.paths ?? [] }
      lazyTree()
      lazyComments()
      lazyGit()
      break
    case 'git':
      lazyGit()
      break
    case 'tree':
      lazyTree()
      break
    case 'comments':
      lazyComments()
      lazyTree()
      break
    case 'round':
      pendingResponseRound = undefined
      responseBanner.value = null
      markResponseSeen()
      refreshRoundInfo()
      lazyComments()
      lazyTree()
      break
    case 'agent':
      applyAgentEvent(ev)
      break
    case 'annotate':
      if (ev.agent) applyAnnotationAgentEvent(ev)
      else {
        lazyPresets()
        lazyAnnotationRequests()
      }
      break
    case 'annotations':
      lazyAnnotations()
      lazyAnnotationRequests()
      lazyComments()
      lazyTree()
      break
    case 'response':
      pendingResponseRound = ev.round
      lazyInfo()
      lazyComments()
      toast(`エージェントの返答を取り込みました（ラウンド${ev.round}）`, 'success')
      break
  }
}

function replayEvents(kinds: Set<string>, paths: Set<string>) {
  // Stream fragments cannot be replayed without their payload; fetch the runs.
  for (const kind of kinds) {
    if (kind === 'agent') lazyAgent()
    else if (kind === 'annotate') { lazyPresets(); lazyAnnotationRequests(); lazyAnnotations() }
    else if (kind === 'response') {
      pendingResponseRound = info.value?.round
      lazyInfo()
      lazyComments()
    } else if (kind === 'round' && kinds.has('response')) {
      lazyTree()
    } else dispatchEvent({ type: kind as ServerEvent['type'], paths: [...paths] })
  }
}

export const connection = createConnection(`${projectBase}/api/events`, {
  makeSource: (url) => new EventSource(url),
  sync: refreshAll,
  probe: async () => {
    try { await api.project(); return 'ok' }
    catch (e) {
      if (e instanceof ApiError && e.status === 404) return 'unavailable'
      if (e instanceof ApiError && e.status === 401) return 'unauthorized'
      return 'error'
    }
  },
  dispatch: dispatchEvent,
  replay: replayEvents,
  visibility: {
    get: () => document.visibilityState === 'hidden' ? 'hidden' : 'visible',
    on: (cb) => {
      document.addEventListener('visibilitychange', cb)
      return () => document.removeEventListener('visibilitychange', cb)
    },
  },
  timers: { set: (fn, ms) => setTimeout(fn, ms), clear: (h) => clearTimeout(h as ReturnType<typeof setTimeout>) },
  closeOnHidden: false,
})
