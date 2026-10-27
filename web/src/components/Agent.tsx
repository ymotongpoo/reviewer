import { Portal } from './Portal'
import { useEffect, useState } from 'preact/hooks'
import { api } from '../api'
import { agentInfo, agentRuns, info, refreshAgent, showAgentRound, toast } from '../state'
import { renderMarkdown } from '../markdown'
import type { AgentEvent, AgentModel, AgentRunView, AgentSession, ModelRef } from '../types'
import { fmt } from './Overview'

export const choiceText: Record<string, string> = {
  once: '今回だけ許可',
  session: 'このセッション中は許可',
  always: '常に許可',
  deny: '拒否',
}

const runStatusText: Record<string, string> = {
  running: '対応中',
  completed: '完了',
  failed: '失敗',
  cancelled: '停止',
  error: 'エラー',
}

const sourceText: Record<string, string> = { discord: 'Discord', cli: 'CLI', telegram: 'Telegram', slack: 'Slack', t3: 'T3 Code' }

/** Header chip showing where feedback goes. */
export function AgentChip() {
  const a = agentInfo.value
  const [picking, setPicking] = useState(false)
  if (!a || (!a.available && !a.reason)) return null
  if (!a.available)
    return (
      <span class="agent-chip off" title={a.reason}>
        🤖 未接続
      </span>
    )
  const running = a.active.length > 0
  return (
    <>
      <button
        class={`agent-chip ${running ? 'running' : ''} ${a.binding ? '' : 'unbound'}`}
        onClick={() => setPicking(true)}
        title={a.binding ? `送信先: ${a.binding.title}（クリックで変更）` : '送信先のセッションを選ぶ'}
      >
        {running ? <span class="spinner" /> : '🤖'} {a.name}: {a.binding ? a.binding.title : '送信先を選ぶ'}
      </button>
      {picking && <SessionPicker onClose={() => setPicking(false)} />}
    </>
  )
}

const modelKey = (m: ModelRef) => `${m.provider}\u0000${m.model}`

/** The model with its reasoning effort set to the model's default. */
function withDefaults(m: AgentModel): ModelRef {
  const r = m.reasoning
  return { provider: m.provider, model: m.model, options: r?.default ? [{ id: r.id, value: r.default }] : undefined }
}

/**
 * Model and reasoning effort, grouped by provider. New sessions start at
 * the directory's most recent model. With allowDefault, an empty choice
 * keeps the existing destination's settings. Agents without a catalog
 * render nothing and pick the model themselves.
 */
export function ModelSelect({ value, onChange, allowDefault = false }: {
  value: ModelRef | null
  onChange: (m: ModelRef | null) => void
  allowDefault?: boolean
}) {
  const [models, setModels] = useState<AgentModel[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  useEffect(() => {
    if (!agentInfo.value?.canPickModel) return
    api.agentModels().then((r) => {
      setModels(r.models)
      if (value || allowDefault) return
      const recent = r.recent && r.models.find((m) => modelKey(m) === modelKey(r.recent!))
      const first = recent ?? r.models[0]
      onChange(first ? withDefaults(first) : null)
    }).catch((e) => setError((e as Error).message))
  }, [])
  if (!agentInfo.value?.canPickModel) return null
  if (error) return <div class="banner error">モデルの一覧を取得できませんでした: {error}</div>
  if (!models) return <div class="muted small">モデルを読み込み中…</div>
  if (models.length === 0) return null
  const groups: [string, AgentModel[]][] = []
  for (const m of models) {
    const g = groups.find(([name]) => name === m.providerName)
    if (g) g[1].push(m)
    else groups.push([m.providerName, [m]])
  }
  const selected = value ? models.find((m) => modelKey(m) === modelKey(value)) : undefined
  const reasoning = selected?.reasoning
  const level = reasoning && (value?.options?.find((o) => o.id === reasoning.id)?.value ?? reasoning.default ?? '')
  return (
    <div class="model-select">
      <label class="field">
        <span>モデル</span>
        <select value={value ? modelKey(value) : ''} onChange={(e) => {
          const m = models.find((m) => modelKey(m) === e.currentTarget.value)
          onChange(m ? withDefaults(m) : null)
        }}>
          {allowDefault && <option value="">送信先の設定を使う</option>}
          {groups.map(([name, ms]) => (
            <optgroup label={name}>
              {ms.map((m) => <option value={modelKey(m)}>{m.label}</option>)}
            </optgroup>
          ))}
        </select>
      </label>
      {reasoning && value && (
        <label class="field">
          <span>推論レベル</span>
          <select value={level} onChange={(e) => {
            const v = e.currentTarget.value
            onChange({ ...value, options: [...(value.options ?? []).filter((o) => o.id !== reasoning.id), { id: reasoning.id, value: v }] })
          }}>
            {reasoning.choices.map((c) => (
              <option value={c.id}>{c.label}{c.id === reasoning.default ? '（既定）' : ''}</option>
            ))}
          </select>
        </label>
      )}
    </div>
  )
}

/** Creates a session of the agent and makes it the target of the feedback. */
function NewSessionForm({ onCreated, onCancel }: { onCreated: () => void; onCancel?: () => void }) {
  const i = info.value
  const [title, setTitle] = useState('')
  const [model, setModel] = useState<ModelRef | null>(null)
  const [busy, setBusy] = useState(false)
  const placeholder = `reviewer: ${i?.name ?? ''}${i && i.session.id !== 'main' ? `［${i.session.name}］` : ''}`
  async function create() {
    setBusy(true)
    try {
      const b = await api.createAgentSession({ title: title.trim() || undefined, model: model ?? undefined })
      await refreshAgent()
      toast(`「${b.title}」を作成して送信先にしました`, 'success')
      onCreated()
    } catch (e) {
      toast((e as Error).message, 'error')
    } finally {
      setBusy(false)
    }
  }
  return (
    <form class="new-session" onSubmit={(e) => { e.preventDefault(); void create() }}>
      <label class="field">
        <span>名前（空欄なら自動）</span>
        <input type="text" value={title} placeholder={placeholder} maxLength={100}
          onInput={(e) => setTitle(e.currentTarget.value)} />
      </label>
      <ModelSelect value={model} onChange={setModel} />
      <p class="muted small">
        {agentInfo.value?.name} に空の新しい{agentInfo.value?.kind === 't3' ? 'スレッド' : 'セッション'}を作ります。最初に提出したフィードバックが、その最初のメッセージになります。
      </p>
      <div class="new-session-actions">
        {onCancel && <button type="button" class="btn small" onClick={onCancel}>やめる</button>}
        <button type="submit" class="btn small primary" disabled={busy}>{busy ? '作成中…' : '作成して送信先にする'}</button>
      </div>
    </form>
  )
}

export function SessionPicker({
  onClose,
  onSelect,
  selected,
  title = '送信先のセッション',
  description,
}: {
  onClose: () => void
  onSelect?: (session: AgentSession) => void
  selected?: string
  title?: string
  description?: string
}) {
  const [sessions, setSessions] = useState<AgentSession[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [filter, setFilter] = useState('')
  const [creating, setCreating] = useState(false)
  const [allProjects, setAllProjects] = useState(false)
  const current = selected ?? agentInfo.value?.binding?.sessionId
  const canCreate = !onSelect && !!agentInfo.value?.canCreate

  useEffect(() => {
    let active = true
    setSessions(null)
    setError(null)
    api
      .agentSessions(allProjects)
      .then((next) => { if (active) setSessions(next) })
      .catch((e) => { if (active) setError((e as Error).message) })
    return () => { active = false }
  }, [allProjects])

  async function bind(id: string) {
    try {
      await api.bindAgent(id)
      await refreshAgent()
      onClose()
      toast(id ? '送信先を設定しました' : '送信先を解除しました', 'success')
    } catch (e) {
      toast((e as Error).message, 'error')
    }
  }

  function choose(session: AgentSession) {
    if (onSelect) {
      onSelect(session)
      onClose()
    } else {
      void bind(session.id)
    }
  }

  const shown = (sessions ?? []).filter(
    (s) => !filter || s.title.toLowerCase().includes(filter.toLowerCase()) || (s.preview ?? '').toLowerCase().includes(filter.toLowerCase()),
  )
  return (
    <Portal onClose={onClose}>
    <div class="modal-backdrop" onClick={(e) => e.target === e.currentTarget && onClose()}>
      <div class="modal" role="dialog" aria-modal="true" aria-label={title}>
        <h2>{title}</h2>
        <p class="muted">
          {description ??
            `提出したフィードバックを、このセッションの会話の続きとして ${agentInfo.value?.name} に送ります。文書を書いたときのセッションを選んでください。`}
        </p>
        <input type="search" class="search" placeholder="タイトルで絞り込む" value={filter} onInput={(e) => setFilter(e.currentTarget.value)} />
        {agentInfo.value?.kind === 't3' && (
          <label class="toggle">
            <input type="checkbox" checked={allProjects} onChange={(e) => setAllProjects(e.currentTarget.checked)} /> 他のプロジェクトも含める
          </label>
        )}
        {error && <div class="banner error">{error}</div>}
        {!sessions && !error && <div class="empty small">読み込み中…</div>}
        <div class="session-list">
          {shown.map((s) => (
            <button class={`session-row ${s.id === current ? 'on' : ''}`} onClick={() => choose(s)}>
              <span class={`chip src-${s.source}`}>{sourceText[s.source] ?? s.source}</span>
              <span class="session-title">{s.title}</span>
              <span class="muted small">{fmt(s.updatedAt)}</span>
              {s.preview && <span class="session-preview">{s.preview}</span>}
            </button>
          ))}
          {sessions && shown.length === 0 && <div class="empty small">
            {filter ? '一致するセッションがありません' : allProjects ? '利用できるセッションがありません' : 'このディレクトリで使えるセッションがありません'}
          </div>}
        </div>
        {canCreate && sessions && (creating || (sessions.length === 0 && !error)
          ? <NewSessionForm onCreated={onClose} onCancel={sessions.length > 0 ? () => setCreating(false) : undefined} />
          : <button class="btn small new-session-toggle" onClick={() => setCreating(true)}>
            ＋ 新しい{agentInfo.value?.kind === 't3' ? 'スレッド' : 'セッション'}を作る
          </button>)}
        <div class="modal-actions">
          {!onSelect && current && (
            <button class="btn danger-text" onClick={() => bind('')}>
              解除
            </button>
          )}
          <span class="spacer" />
          <button class="btn" onClick={onClose}>
            閉じる
          </button>
        </div>
      </div>
    </div>
    </Portal>
  )
}

/** Progress of the agent for a round, shown on the overview page. */
export function AgentPanel({ round }: { round: number }) {
  const a = agentInfo.value
  const [model, setModel] = useState<ModelRef | null>(null)
  useEffect(() => {
    void showAgentRound(round)
  }, [round])
  if (!a || !a.available) return null
  const runs = agentRuns.value
  const submitted = info.value?.rounds.find((r) => r.round === round)?.submittedAt
  const running = runs.some((r) => r.status === 'running')

  async function send() {
    try {
      await api.agentSend(round, a?.canConfigureModel ? model ?? undefined : undefined)
      await refreshAgent()
    } catch (e) {
      toast((e as Error).message, 'error')
    }
  }

  return (
    <section class="card agent-panel">
      <div class="section-head">
        <h2>🤖 {a.name}（ラウンド{round}）</h2>
        <span class="spacer" />
        {submitted && a.binding && !running && (
          <button class="btn small" onClick={() => void send()}>
            {runs.length > 0 ? '再送信' : '送信'}
          </button>
        )}
      </div>
      {!a.binding && <p class="muted">送信先のセッションが選ばれていません。右上の「🤖 {a.name}」から選んでください。</p>}
      {submitted && a.binding && !running && a.canConfigureModel && (
        <ModelSelect value={model} onChange={setModel} allowDefault />
      )}
      {a.binding && runs.length === 0 && (
        <p class="muted">
          まだ送信していません。{submitted ? '「送信」を押すと' : '提出時に'}「{a.binding.title}」へフィードバックを送ります。
        </p>
      )}
      {[...runs].reverse().map((r, i) => (
        <RunView key={r.id} run={r} defaultOpen={i === 0} />
      ))}
    </section>
  )
}

export function RunView({ run: r, defaultOpen }: { run: AgentRunView; defaultOpen: boolean }) {
  const [open, setOpen] = useState(defaultOpen)
  const [busy, setBusy] = useState(false)
  const tools = r.events.filter((e) => e.type === 'tool' && e.toolState !== 'started')
  // Events are the source of truth while streaming; the stored text is capped.
  const streamed = r.events.filter((e) => e.type === 'text').map((e) => e.text ?? '').join('')
  const text = streamed.length >= (r.text ?? '').length ? streamed : r.text
  const denied = r.events.filter((e) => e.type === 'answered' && e.text === 'deny').length

  async function answer(choice: string) {
    setBusy(true)
    try {
      await api.agentAnswer(r.id, choice, r.pending?.id)
    } catch (e) {
      toast((e as Error).message, 'error')
    } finally {
      setBusy(false)
    }
  }
  async function stop() {
    try {
      await api.agentStop(r.id)
    } catch (e) {
      toast((e as Error).message, 'error')
    }
  }

  return (
    <div class={`run run-${r.status}`}>
      <div class="run-head" onClick={() => setOpen(!open)}>
        <span class={`chip run-status ${r.status}`}>
          {r.status === 'running' && <span class="spinner" />}
          {runStatusText[r.status] ?? r.status}
        </span>
        <span class="muted small">
          {fmt(r.startedAt)}
          {r.endedAt ? ` → ${fmt(r.endedAt)}` : ''}
        </span>
        <span class="muted small">ツール {tools.length}回</span>
        <span class="spacer" />
        {r.status === 'running' && (
          <button
            class="btn small danger-text"
            onClick={(e) => {
              e.stopPropagation()
              void stop()
            }}
          >
            停止
          </button>
        )}
        <span class="caret">{open ? '▾' : '▸'}</span>
      </div>
      {r.pending && (
        <div class="approval">
          <div class="approval-title">⚠ 承認が必要です</div>
          {r.pending.description && <div>{r.pending.description}</div>}
          {r.pending.command && <pre class="approval-cmd">{r.pending.command}</pre>}
          <div class="approval-actions">
            {r.pending.choices.map((c) => (
              <button class={`btn small ${c === 'deny' ? 'danger-text' : c === 'once' ? 'primary' : ''}`} disabled={busy} onClick={() => void answer(c)}>
                {choiceText[c] ?? c}
              </button>
            ))}
          </div>
        </div>
      )}
      {r.error && <div class="banner error">{r.error}</div>}
      {r.noResponse && (
        <div class="banner warn">
          エージェントは完了しましたが、{r.purpose === 'annotate' ? 'annotations.json' : 'response.json'} が書かれていません。
          {r.purpose === 'annotate' ? 'AI指摘はありません。' : 'コメントごとの返答はありません。'}
        </div>
      )}
      {r.notifyError && <div class="banner warn">通知を送れませんでした: {r.notifyError}</div>}
      {denied > 0 && <div class="muted small">承認を{denied}回拒否しました</div>}
      {open && (
        <>
          {text ? (
            <div class="md run-text" dangerouslySetInnerHTML={{ __html: renderMarkdown(text) }} />
          ) : (
            r.status === 'running' && <p class="muted">応答を待っています…</p>
          )}
          {r.events.some((e) => e.type === 'tool' || e.type === 'thinking') && (
            <details class="run-log" open={r.status === 'running'}>
              <summary>作業ログ</summary>
              <ul>
                {r.events.filter(isLogEvent).map((e) => (
                  <LogLine ev={e} />
                ))}
              </ul>
            </details>
          )}
        </>
      )}
    </div>
  )
}

function isLogEvent(e: AgentEvent) {
  return e.type === 'tool' || e.type === 'thinking' || e.type === 'answered'
}

function LogLine({ ev }: { ev: AgentEvent }) {
  const t = new Date(ev.at).toLocaleTimeString('ja-JP', { hour: '2-digit', minute: '2-digit', second: '2-digit' })
  if (ev.type === 'thinking')
    return (
      <li class="log thinking">
        <span class="log-time">{t}</span>💭 {ev.text}
      </li>
    )
  if (ev.type === 'answered')
    return (
      <li class="log">
        <span class="log-time">{t}</span>承認への回答: {choiceText[ev.text ?? ''] ?? ev.text}
      </li>
    )
  const icon = ev.toolState === 'started' ? '▶' : ev.toolState === 'failed' ? '✗' : '✓'
  return (
    <li class={`log tool ${ev.toolState}`}>
      <span class="log-time">{t}</span>
      {icon} <code>{ev.tool}</code> {ev.text && <span class="muted">{ev.text}</span>}
    </li>
  )
}
