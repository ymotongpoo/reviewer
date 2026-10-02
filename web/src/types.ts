export type Scope = 'project' | 'file' | 'line'
export type Status = 'draft' | 'open' | 'addressed' | 'wontfix' | 'question' | 'resolved'
export type AnchorState = 'exact' | 'moved' | 'fuzzy' | 'outdated'

export interface Anchor {
  lines: string[]
  before: string[]
  after: string[]
  hash: string
}

/**
 * The half-open character range [start, end) of a range comment. Lines are
 * 1-based; columns are 0-based Unicode code points of the text without a byte
 * order mark, not counting the CR of a CRLF line break.
 */
export interface TextRange {
  startLine: number
  startColumn: number
  endLine: number
  endColumn: number
  /** The selected text, with lines joined by "\n". */
  text?: string
  /** Characters around the range, which tell identical selections apart. */
  before?: string
  after?: string
}

export interface Location {
  start: number
  end: number
  state: AnchorState
  blob: string
  anchor: Anchor
  /** The range of a range comment where it was last found, without its text. */
  range?: TextRange
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
  /** Set for a comment on part of the lines: the characters it was written against. */
  range?: TextRange
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
  /** Whether the project is inside a Git work tree. */
  git: boolean
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

export type ChangeKind = 'modified' | 'added' | 'deleted' | 'unchanged'

export interface RoundChange {
  path: string
  kind: ChangeKind
  insert: number
  delete: number
  comments: number
}

/** A comment of a round; lines refer to the submitted snapshot (old side). */
export interface RoundComment {
  id: string
  scope: Scope
  path?: string
  startLine?: number
  endLine?: number
  located: boolean
  carriedOver: boolean
}

export interface RoundChanges {
  round: number
  /** The next round, or 0 when compared with the current files. */
  toRound: number
  files: RoundChange[]
  comments: RoundComment[]
}

export interface RoundDiff {
  round: number
  toRound: number
  path: string
  kind: ChangeKind
  phase: 'review' | 'agent'
  ops: DiffOp[]
}

export interface SubmitResult extends RoundPaths {
  count: number
  agentRun?: AgentRun
  agentError?: string
}

export interface ServerEvent {
  type: 'files' | 'tree' | 'comments' | 'round' | 'response' | 'agent' | 'annotate' | 'annotations' | 'git'
  paths?: string[]
  round?: number
  request?: string
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
  purpose?: 'feedback' | 'annotate'
  request?: string
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

export type PresetScope = 'all' | 'current' | 'selected'
export type PresetOrigin = 'builtin' | 'global' | 'project'

export interface Preset {
  name: string
  prompt: string
  scope: PresetScope
  origin: PresetOrigin
}

export type AnnotationSeverity = 'critical' | 'major' | 'minor' | 'info'
export type AnnotationConfidence = 'high' | 'medium' | 'low'
export type AnnotationState = 'pending' | 'adopted' | 'dismissed'

export interface AnnotationEvidence {
  url?: string
  quote?: string
  note?: string
}

export interface Annotation {
  id: string
  request: string
  path: string
  origStart?: number
  origEnd?: number
  origBlob?: string
  anchor?: Anchor
  loc?: Location
  severity: AnnotationSeverity
  confidence: AnnotationConfidence
  label?: string
  body: string
  evidence: AnnotationEvidence[]
  suggestion?: string
  /** How reviewer adjusted the agent's proposal, if it did. */
  suggestionNote?: string
  state: AnnotationState
  adoptedAs?: string
  createdAt: string
  updatedAt: string
}

export interface AnnotationImport {
  hash: string
  importedAt: string
  summary?: string
  count: number
  warnings?: string[]
  error?: string
}

export interface AnnotationRequest {
  id: string
  preset?: string
  prompt: string
  files: Record<string, string>
  target: 'new' | 'bound' | 'session'
  sessionId?: string
  sessionTitle?: string
  hidden?: boolean
  createdAt: string
  completedAt?: string
  changedPaths?: string[]
  import?: AnnotationImport
}

export interface AnnotationRequestView extends AnnotationRequest {
  runs: AgentRunView[]
}

export interface AnnotateResult {
  request: AnnotationRequest
  agentRun: AgentRun
}

export interface RequestDiff {
  request: string
  path: string
  kind: ChangeKind
  ops: DiffOp[]
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

export interface ServerInfo {
  roots: string[]
  home: string
  version: string
}

export interface ProjectSummary {
  id: string
  name: string
  path: string
  display: string
  open: boolean
  exists: boolean
  initialized: boolean
  lastOpened: string
  round?: number
  roundStatus?: 'open' | 'submitted'
  agent?: AgentBinding
  busy?: boolean
}

export interface DirEntry {
  name: string
  path: string
  display: string
  hasReviewer: boolean
}

export interface DirListing {
  path?: string
  display?: string
  parent?: string
  entries: DirEntry[]
  hasReviewer: boolean
}

export type GitLanguage = 'ja' | 'en'

export interface GitSettings {
  language: GitLanguage
  /** Default push target; empty means the upstream (or origin) and the current branch. */
  remote: string
  branch: string
}

export interface GitChange {
  path: string
  from?: string
  kind: 'added' | 'modified' | 'deleted' | 'renamed' | 'typechange' | 'conflict'
  staged: boolean
  untracked?: boolean
}

export interface GitRemote {
  name: string
  /** Masked push URL, informational only. */
  url: string
  branches: string[]
  mirror?: boolean
}

export interface GitTarget {
  remote: string
  branch: string
  tracked: boolean
  ahead: number
  behind: number
}

export interface GitStatus {
  repo: boolean
  toplevel?: string
  prefix?: string
  branch: string
  unborn: boolean
  head?: string
  upstream: { remote?: string; branch?: string }
  operation?: string
  changes: GitChange[]
  outsideStaged: string[]
  fingerprint: string
  identity?: string
  messages: Partial<Record<GitLanguage, string>>
  settings: GitSettings
  remotes: GitRemote[]
  target: GitTarget
  commitBlockers: string[]
  pushBlockers: string[]
  env: { sshAgent: boolean; credentialHelper: boolean }
}

export interface GitCommitResult {
  commit: string
  branch: string
  subject: string
  files: number
}

export interface GitHint {
  text: string
  commands?: string[]
}

export interface GitFailure {
  kind: string
  message: string
  detail: string
  hints: GitHint[]
}

export interface GitPushResult {
  ok: boolean
  remote: string
  branch: string
  commit?: string
  messages?: string
  failure?: GitFailure
}
