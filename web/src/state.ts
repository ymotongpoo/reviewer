import { computed, signal } from '@preact/signals'
import { api, projectBase } from './api'
import type {
  AgentInfo,
  AgentRunView,
  Annotation,
  AnnotationRequestView,
  AnnotationSeverity,
  Comment,
  Info,
  Preset,
  ServerEvent,
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

export async function refreshInfo() {
  info.value = await api.project()
}
export async function refreshTree() {
  tree.value = await api.tree()
}
export async function refreshComments() {
  comments.value = await api.comments()
}
export async function refreshAgent() {
  agentInfo.value = await api.agent()
  if (agentRound.value > 0) agentRuns.value = await api.agentRuns(agentRound.value)
}
export async function refreshPresets() {
  presets.value = await api.presets()
}
export async function refreshAnnotations() {
  annotations.value = await api.annotations(true)
}
export async function refreshAnnotationRequests() {
  const list = await api.annotationRequests()
  const views = await Promise.all(
    list.map(async (r) => {
      try {
        return await api.annotationRequest(r.id)
      } catch {
        return { ...r, runs: [] }
      }
    }),
  )
  annotationRequests.value = views
}
export async function showAgentRound(round: number) {
  if (agentRound.value === round) return
  agentRound.value = round
  agentRuns.value = round > 0 ? await api.agentRuns(round) : []
}
export async function refreshAll() {
  await Promise.all([
    refreshInfo(),
    refreshTree(),
    refreshComments(),
    refreshAgent(),
    refreshPresets(),
    refreshAnnotations(),
    refreshAnnotationRequests(),
  ])
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
  const list = comments.value
  const i = list.findIndex((x) => x.id === c.id)
  comments.value = i >= 0 ? [...list.slice(0, i), c, ...list.slice(i + 1)] : [...list, c]
}
export function removeComment(id: string) {
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
const lazyInfo = debounced(() => refreshInfo().catch(() => {}), 150)
const lazyAgent = debounced(() => refreshAgent().catch(() => {}), 150)
const lazyPresets = debounced(() => refreshPresets().catch(() => {}), 150)
const lazyAnnotations = debounced(() => refreshAnnotations().catch(() => {}), 150)
const lazyAnnotationRequests = debounced(() => refreshAnnotationRequests().catch(() => {}), 150)

export function connectEvents() {
  const es = new EventSource(`${projectBase}/api/events`)
  let wasDown = false
  es.onopen = () => {
    if (wasDown) {
      wasDown = false
      refreshAll().catch(() => {})
      fileVersion.value = { n: fileVersion.value.n + 1, paths: ['*'] }
    }
  }
  es.onerror = () => {
    wasDown = true
  }
  es.onmessage = (m) => {
    const ev = JSON.parse(m.data) as ServerEvent
    switch (ev.type) {
      case 'files':
        fileVersion.value = { n: fileVersion.value.n + 1, paths: ev.paths ?? [] }
        lazyTree()
        lazyComments()
        break
      case 'tree':
        lazyTree()
        break
      case 'comments':
        lazyComments()
        lazyTree()
        break
      case 'round':
        responseBanner.value = null
        lazyInfo()
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
        responseBanner.value = ev.round ?? null
        lazyInfo()
        lazyComments()
        toast(`エージェントの返答を取り込みました（ラウンド${ev.round}）`, 'success')
        break
    }
  }
}
