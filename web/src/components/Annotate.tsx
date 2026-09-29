import { useEffect, useMemo, useRef, useState } from 'preact/hooks'
import { api, ApiError } from '../api'
import {
  agentInfo,
  info,
  presets,
  refreshAnnotationRequests,
  refreshAnnotations,
  refreshPresets,
  toast,
  tree,
} from '../state'
import { route } from '../router'
import type { AgentSession, AnnotationRequest, Preset, PresetScope } from '../types'
import { SessionPicker } from './Agent'

const originText = { builtin: '組み込み', global: 'グローバル', project: 'プロジェクト' }
const scopeText = { all: '全体', current: '現在のファイル', selected: 'ファイルを選択' }

export function AnnotateButton() {
  const [open, setOpen] = useState(false)
  if (!agentInfo.value?.available) return null
  return (
    <>
      <button class="btn" onClick={() => setOpen(true)}>
        🔍 AIに確認を依頼
      </button>
      {open && <AnnotateDialog onClose={() => setOpen(false)} />}
    </>
  )
}

function AnnotateDialog({ onClose }: { onClose: () => void }) {
  const list = presets.value
  const currentPath = route.value.page === 'file' ? route.value.path : undefined
  const first = list[0]
  const [presetName, setPresetName] = useState(first?.name ?? '')
  const [prompt, setPrompt] = useState(first?.prompt ?? '')
  const [scope, setScope] = useState<PresetScope>(first?.scope ?? 'all')
  const [selectedPaths, setSelectedPaths] = useState<string[]>([])
  const [fileFilter, setFileFilter] = useState('')
  const [saveName, setSaveName] = useState(first?.name ?? '')
  const [target, setTarget] = useState<AnnotationRequest['target']>('new')
  const [sessionTitle, setSessionTitle] = useState('')
  const sessionTitleInput = useRef<HTMLInputElement>(null)
  const [session, setSession] = useState<AgentSession | null>(null)
  const [picking, setPicking] = useState(false)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!first) void refreshPresets().catch((e) => toast((e as Error).message, 'error'))
  }, [])

  const chosen = list.find((p) => p.name === presetName)
  const projectPresets = list.filter((p) => p.origin === 'project')
  const shownFiles = useMemo(
    () => tree.value.filter((f) => !fileFilter || f.path.toLowerCase().includes(fileFilter.toLowerCase())),
    [fileFilter, tree.value],
  )

  function choosePreset(name: string) {
    const p = list.find((item) => item.name === name)
    setPresetName(name)
    if (!p) return
    setPrompt(p.prompt)
    setScope(p.scope)
    setSaveName(p.name)
  }

  async function savePreset() {
    const name = saveName.trim()
    if (!name || !prompt.trim()) {
      toast('プリセット名とプロンプトを入力してください', 'error')
      return
    }
    const next = projectPresets
      .filter((p) => p.name !== name)
      .map(stripOrigin)
      .concat({ name, prompt: prompt.trim(), scope })
    try {
      presets.value = await api.savePresets(next)
      setPresetName(name)
      toast(`プリセット「${name}」を保存しました`, 'success')
    } catch (e) {
      toast((e as Error).message, 'error')
    }
  }

  async function deletePreset() {
    if (!chosen || chosen.origin !== 'project') return
    if (!confirm(`プロジェクトプリセット「${chosen.name}」を削除しますか？`)) return
    try {
      presets.value = await api.savePresets(projectPresets.filter((p) => p.name !== chosen.name).map(stripOrigin))
      const next = presets.value[0]
      setPresetName(next?.name ?? '')
      setPrompt(next?.prompt ?? '')
      setScope(next?.scope ?? 'all')
      setSaveName(next?.name ?? '')
      toast('プリセットを削除しました', 'success')
    } catch (e) {
      toast((e as Error).message, 'error')
    }
  }

  function togglePath(path: string) {
    setSelectedPaths((paths) => (paths.includes(path) ? paths.filter((p) => p !== path) : [...paths, path]))
  }

  async function send() {
    const paths = scope === 'all' ? undefined : scope === 'current' ? (currentPath ? [currentPath] : []) : selectedPaths
    if (!prompt.trim()) return toast('確認内容を入力してください', 'error')
    if (scope !== 'all' && !paths?.length) return toast('確認するファイルを選んでください', 'error')
    if (target === 'bound' && !agentInfo.value?.binding) return toast('バインド済みのセッションがありません', 'error')
    if (target === 'session' && !session) return toast('送信先のセッションを選んでください', 'error')
    setBusy(true)
    try {
      const result = await api.annotate({
        preset: presetName,
        prompt: prompt.trim(),
        paths,
        target,
        sessionId: target === 'session' ? session!.id : undefined,
        sessionTitle: target === 'new' ? sessionTitle.trim() || undefined : undefined,
      })
      await Promise.all([refreshAnnotationRequests(), refreshAnnotations()])
      toast(`${result.request.id} を ${agentInfo.value?.name ?? 'エージェント'} に送りました`, 'success')
      onClose()
      location.hash = '#/'
    } catch (e) {
      toast((e as Error).message, 'error')
      if (e instanceof ApiError && e.status === 409) sessionTitleInput.current?.focus()
    } finally {
      setBusy(false)
    }
  }

  return (
    <div class="modal-backdrop" onClick={(e) => e.target === e.currentTarget && onClose()}>
      <div class="modal annotate-modal" role="dialog" aria-modal="true" aria-label="AIに確認を依頼">
        <h2>🔍 AIに確認を依頼</h2>

        <label class="field">
          <span>プリセット</span>
          <span class="field-row">
            <select value={presetName} onChange={(e) => choosePreset(e.currentTarget.value)}>
              {list.map((p) => (
                <option value={p.name}>{p.name}（{originText[p.origin]}）</option>
              ))}
            </select>
            {chosen && <span class={`chip preset-${chosen.origin}`}>{originText[chosen.origin]}</span>}
          </span>
        </label>
        <label class="field">
          <span>確認内容</span>
          <textarea rows={7} value={prompt} onInput={(e) => setPrompt(e.currentTarget.value)} />
        </label>
        <div class="preset-save">
          <input value={saveName} placeholder="プリセット名" onInput={(e) => setSaveName(e.currentTarget.value)} />
          <button class="btn small" onClick={() => void savePreset()}>
            プリセットとして保存
          </button>
          {chosen?.origin === 'project' && (
            <button class="btn small danger-text" onClick={() => void deletePreset()}>
              削除
            </button>
          )}
        </div>
        {chosen && chosen.origin !== 'project' && <div class="hint">組み込み／グローバルのプリセットは読み取り専用です。別名でプロジェクトに保存できます。</div>}

        <fieldset>
          <legend>確認する範囲</legend>
          {(Object.keys(scopeText) as PresetScope[]).map((value) => (
            <label>
              <input
                type="radio"
                name="annotation-scope"
                value={value}
                checked={scope === value}
                disabled={value === 'current' && !currentPath}
                onChange={() => setScope(value)}
              />{' '}
              {scopeText[value]}
              {value === 'current' && currentPath ? <span class="muted small">（{currentPath}）</span> : null}
            </label>
          ))}
          {scope === 'selected' && (
            <div class="file-picker">
              <input type="search" placeholder="ファイルを絞り込む" value={fileFilter} onInput={(e) => setFileFilter(e.currentTarget.value)} />
              <div class="file-checks">
                {shownFiles.map((f) => (
                  <label title={f.path}>
                    <input type="checkbox" checked={selectedPaths.includes(f.path)} onChange={() => togglePath(f.path)} /> {f.path}
                  </label>
                ))}
              </div>
              <span class="muted small">{selectedPaths.length}件を選択</span>
            </div>
          )}
        </fieldset>

        <fieldset>
          <legend>送信先</legend>
          <label>
            <input type="radio" name="annotation-target" checked={target === 'new'} onChange={() => setTarget('new')} /> 新規セッション
          </label>
          {target === 'new' && (
            <label class="field">
              <span>セッション名（空欄なら自動）</span>
              <input
                type="text"
                ref={sessionTitleInput}
                value={sessionTitle}
                placeholder={`reviewer: ${info.value?.name ?? ''} Q-… ${presetName.trim() || 'AI確認'}`}
                onInput={(e) => setSessionTitle(e.currentTarget.value)}
              />
            </label>
          )}
          <label>
            <input
              type="radio"
              name="annotation-target"
              checked={target === 'bound'}
              disabled={!agentInfo.value?.binding}
              onChange={() => setTarget('bound')}
            />{' '}
            バインド済みセッション
            {agentInfo.value?.binding && <span class="muted small">（{agentInfo.value.binding.title}）</span>}
          </label>
          <label>
            <input type="radio" name="annotation-target" checked={target === 'session'} onChange={() => setTarget('session')} /> 別のセッション
          </label>
          {target === 'session' && (
            <button class="btn small session-choice" onClick={() => setPicking(true)}>
              {session ? `選択中: ${session.title}` : 'セッションを選ぶ…'}
            </button>
          )}
        </fieldset>

        <div class="modal-actions">
          <button class="btn" onClick={onClose}>キャンセル</button>
          <button class="btn primary" disabled={busy} onClick={() => void send()}>{busy ? '送信中…' : '確認を依頼'}</button>
        </div>
      </div>
      {picking && (
        <SessionPicker
          onClose={() => setPicking(false)}
          onSelect={setSession}
          selected={session?.id}
          title="確認依頼の送信先"
          description="この確認依頼を続ける既存のセッションを選んでください。バインド先は変更されません。"
        />
      )}
    </div>
  )
}

function stripOrigin(p: Preset): Omit<Preset, 'origin'> {
  return { name: p.name, prompt: p.prompt, scope: p.scope }
}
