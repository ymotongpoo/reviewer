export type Scope = 'project' | 'file' | 'line'
export type Status = 'draft' | 'open' | 'addressed' | 'wontfix' | 'question' | 'resolved'
export type AnchorState = 'exact' | 'moved' | 'fuzzy' | 'outdated'

export interface Anchor {
  lines: string[]
  before: string[]
  after: string[]
  hash: string
}

export interface Location {
  start: number
  end: number
  state: AnchorState
  blob: string
  anchor: Anchor
}

export interface Reply {
  id: string
  author: 'human' | 'agent'
  body: string
  status?: string
  round: number
  draft?: boolean
  createdAt: string
  updatedAt: string
}

export interface Comment {
  id: string
  round: number
  scope: Scope
  path?: string
  label: string
  body: string
  status: Status
  anchor?: Anchor
  origStart?: number
  origEnd?: number
  loc?: Location
  replies: Reply[]
  resolvedRound?: number
  createdAt: string
  updatedAt: string
}

export interface Label {
  name: string
  description: string
}

export interface ResponseInfo {
  hash: string
  importedAt: string
  summary?: string
  count: number
  warnings?: string[]
  error?: string
}

export interface RoundPaths {
  round: number
  feedbackPath: string
  feedbackJsonPath: string
  responsePath: string
  prompt: string
}

export interface RoundSummary {
  round: number
  openedAt: string
  submittedAt?: string
  comments: number
  response?: ResponseInfo
}

export interface Info {
  name: string
  root: string
  dataDir: string
  round: number
  roundStatus: 'open' | 'submitted'
  openedAt: string
  baseRound: number
  labels: Label[]
  warnings: string[] | null
  latest?: RoundPaths
  response?: ResponseInfo
  rounds: RoundSummary[]
}

export interface TreeFile {
  path: string
  unresolved: number
  changed: boolean
  new: boolean
}

export interface FileView {
  path: string
  content: string
  hash: string
}

export interface DiffOp {
  kind: 'eq' | 'del' | 'ins'
  lines: string[]
}

export type ChangeKind = 'modified' | 'added' | 'deleted'

export interface RoundChange {
  path: string
  kind: ChangeKind
  insert: number
  delete: number
}

export interface RoundChanges {
  round: number
  /** The next round, or 0 when compared with the current files. */
  toRound: number
  files: RoundChange[]
}

export interface RoundDiff {
  round: number
  toRound: number
  path: string
  kind: ChangeKind
  ops: DiffOp[]
}

export interface SubmitResult extends RoundPaths {
  count: number
  agentRun?: AgentRun
  agentError?: string
}

export interface ServerEvent {
  type: 'files' | 'tree' | 'comments' | 'round' | 'response' | 'agent'
  paths?: string[]
  round?: number
  run?: string
  agent?: AgentEvent
}

export interface AgentApproval {
  id?: string
  command?: string
  description?: string
  choices: string[]
}

export interface AgentEvent {
  type: 'started' | 'text' | 'thinking' | 'tool' | 'approval' | 'answered' | 'done' | 'error'
  text?: string
  tool?: string
  toolState?: 'started' | 'completed' | 'failed'
  status?: string
  approval?: AgentApproval
  at: string
}

export interface AgentBinding {
  kind: string
  sessionId: string
  title: string
  source?: string
  boundAt: string
}

export interface AgentRun {
  id: string
  kind: string
  round: number
  sessionId: string
  status: 'running' | 'completed' | 'failed' | 'cancelled' | 'error'
  startedAt: string
  endedAt?: string
  text?: string
  error?: string
  pending?: AgentApproval
  noResponse?: boolean
  notifyError?: string
}

export interface AgentRunView extends AgentRun {
  events: AgentEvent[]
}

export interface AgentInfo {
  available: boolean
  kind?: string
  name?: string
  reason?: string
  notify: string
  autoSend: boolean
  binding?: AgentBinding
  active: AgentRun[]
}

export interface AgentSession {
  id: string
  title: string
  source: string
  preview?: string
  updatedAt: string
}
