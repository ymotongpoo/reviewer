import { Portal } from './Portal'
import { useEffect, useState } from 'preact/hooks'
import { api } from '../api'
import { agentInfo, agentRuns, info, refreshAgent, showAgentRound, toast } from '../state'
import { renderMarkdown } from '../markdown'
import type { AgentEvent, AgentRunView, AgentSession } from '../types'
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

const sourceText: Record<string, string> = { discord: 'Discord', cli: 'CLI', telegram: 'Telegram', slack: 'Slack' }

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
  const current = selected ?? agentInfo.value?.binding?.sessionId

  useEffect(() => {
    api
      .agentSessions()
      .then(setSessions)
      .catch((e) => setError((e as Error).message))
  }, [])

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
          {sessions && shown.length === 0 && <div class="empty small">セッションがありません</div>}
        </div>
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
  useEffect(() => {
    void showAgentRound(round)
  }, [round])
  if (!a || !a.available) return null
  const runs = agentRuns.value
  const submitted = info.value?.rounds.find((r) => r.round === round)?.submittedAt
  const running = runs.some((r) => r.status === 'running')

  async function send() {
    try {
      await api.agentSend(round)
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
