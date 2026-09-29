import { useState } from 'preact/hooks'
import { api } from '../api'
import { copyText } from '../clipboard'
import { agentInfo, comments, draftCount, info, refreshAll, responseBanner, toast } from '../state'
import { AgentChip } from './Agent'
import type { SubmitResult } from '../types'

export function CopyPrompt({ prompt, label = '指示文をコピー' }: { prompt: string; label?: string }) {
  const [done, setDone] = useState(false)
  return (
    <div class="copy-prompt">
      <pre>{prompt}</pre>
      <button
        class={`btn ${done ? 'success' : 'primary'}`}
        onClick={async () => {
          try {
            await copyText(prompt)
            setDone(true)
            setTimeout(() => setDone(false), 2000)
          } catch {
            toast('コピーできませんでした。テキストを選択してコピーしてください', 'error')
          }
        }}
      >
        {done ? '✓ コピーしました' : `📋 ${label}`}
      </button>
    </div>
  )
}

export function Header() {
  const i = info.value
  const [dialog, setDialog] = useState<'confirm' | SubmitResult | null>(null)
  if (!i) return null
  const submitted = i.roundStatus === 'submitted'

  return (
    <>
      <header class="app-header">
        <a class="brand" href="#/">
          reviewer
        </a>
        <span class="project" title={i.root}>
          {i.name}
        </span>
        <span class={`round-chip ${i.roundStatus}`}>
          ラウンド {i.round} · {submitted ? '提出済み' : '下書き中'}
        </span>
        <span class="spacer" />
        <AgentChip />
        {submitted && (
          <button
            class="btn"
            onClick={async () => {
              try {
                await api.openRound()
                await refreshAll()
              } catch (e) {
                toast((e as Error).message, 'error')
              }
            }}
          >
            次のラウンドを開始
          </button>
        )}
        {i.latest && submitted && (
          <button
            class="btn"
            onClick={async () => {
              try {
                await copyText(i.latest!.prompt)
                toast('指示文をコピーしました', 'success')
              } catch {
                toast('コピーできませんでした', 'error')
              }
            }}
          >
            📋 指示文をコピー
          </button>
        )}
        {!submitted && (
          <button class="btn primary" onClick={() => setDialog('confirm')}>
            レビューを提出
            {draftCount.value > 0 && <span class="badge">{draftCount.value}</span>}
          </button>
        )}
      </header>
      {responseBanner.value !== null && (
        <div class="banner success top">
          エージェントがラウンド{responseBanner.value}に返答しました。各コメントを確認し、解決するか返信してください。
          <span class="spacer" />
          <a href="#/" onClick={() => (responseBanner.value = null)}>
            概要を見る
          </a>
          <button class="link" onClick={() => (responseBanner.value = null)} aria-label="閉じる">
            ×
          </button>
        </div>
      )}
      {dialog && <SubmitDialog state={dialog} setState={setDialog} />}
    </>
  )
}

function SubmitDialog({
  state,
  setState,
}: {
  state: 'confirm' | SubmitResult
  setState: (s: 'confirm' | SubmitResult | null) => void
}) {
  const [busy, setBusy] = useState(false)
  const ag = agentInfo.value
  const canSend = !!ag?.available && !!ag.binding
  const [send, setSend] = useState(canSend && !!ag?.autoSend)
  const i = info.value!
  const all = comments.value
  const drafts = all.filter((c) => c.status === 'draft')
  const replies = all.reduce((n, c) => n + c.replies.filter((r) => r.draft).length, 0)
  const carried = all.filter((c) => c.round < i.round && c.status !== 'resolved' && c.status !== 'draft')
  const untouched = carried.filter((c) => (c.status === 'addressed' || c.status === 'wontfix') && !c.replies.some((r) => r.draft))
  const empty = drafts.length === 0 && carried.length === 0

  async function submit() {
    setBusy(true)
    try {
      const res = await api.submit(canSend && send)
      await refreshAll()
      setState(res)
      if (!res.agentRun) copyText(res.prompt).catch(() => {})
    } catch (e) {
      toast((e as Error).message, 'error')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div class="modal-backdrop" onClick={(e) => e.target === e.currentTarget && setState(null)}>
      <div class="modal" role="dialog" aria-modal="true">
        {state === 'confirm' ? (
          <>
            <h2>ラウンド{i.round}を提出</h2>
            <ul class="submit-summary">
              <li>
                新しいコメント: <b>{drafts.length}</b>件
              </li>
              <li>
                返信: <b>{replies}</b>件
              </li>
              <li>
                前ラウンドから持ち越す未解決コメント: <b>{carried.length}</b>件
              </li>
            </ul>
            {untouched.length > 0 && (
              <div class="banner warn">
                エージェントが「対応済み」「対応しない」と返答したまま、解決も返信もしていないコメントが{untouched.length}
                件あります（{untouched.map((c) => c.id).join(', ')}）。納得できるものは「解決」にすると、持ち越されません。
              </div>
            )}
            {empty && <div class="banner warn">提出するコメントがありません。</div>}
            <p class="muted">
              提出すると、この時点のファイル内容を保存し、<code>feedback.md</code> と <code>feedback.json</code> を書き出します。
            </p>
            {canSend && (
              <label class="send-agent">
                <input type="checkbox" checked={send} onChange={(e) => setSend(e.currentTarget.checked)} />
                <span>
                  {ag!.name} に送信する（<b>{ag!.binding!.title}</b>）
                </span>
              </label>
            )}
            {ag?.available && !ag.binding && (
              <p class="muted small">右上の「🤖 {ag.name}」で送信先を選ぶと、提出と同時にエージェントへ送れます。</p>
            )}
            <div class="modal-actions">
              <button class="btn" onClick={() => setState(null)}>
                キャンセル
              </button>
              <button class="btn primary" disabled={busy || empty} onClick={() => void submit()}>
                {busy ? '提出中…' : '提出する'}
              </button>
            </div>
          </>
        ) : (
          <>
            <h2>ラウンド{state.round}を提出しました</h2>
            {state.agentRun ? (
              <div class="banner success">
                {state.count}件のコメントを {agentInfo.value?.name} に送りました。進捗は概要ページで見られます。
              </div>
            ) : (
              <p>{state.count}件のコメントを書き出しました。次の指示文をエージェントに渡してください。</p>
            )}
            {state.agentError && <div class="banner error">送信できませんでした: {state.agentError}</div>}
            <CopyPrompt prompt={state.prompt} label={state.agentRun ? '指示文をコピー（手動で渡す場合）' : undefined} />
            <dl>
              <dt>feedback.md</dt>
              <dd>
                <code>{state.feedbackPath}</code>
              </dd>
              <dt>feedback.json</dt>
              <dd>
                <code>{state.feedbackJsonPath}</code>
              </dd>
              <dt>返答先</dt>
              <dd>
                <code>{state.responsePath}</code>
              </dd>
            </dl>
            <div class="modal-actions">
              <button class="btn primary" onClick={() => setState(null)}>
                閉じる
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  )
}
