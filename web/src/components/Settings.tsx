import { useEffect, useMemo, useState } from 'preact/hooks'
import { api } from '../api'
import { toast } from '../state'
import { theme, setTheme, themeIcon, themeText, type Theme } from '../theme'
import type { GitLanguage, GitSettings, GitStatus } from '../types'

const themes: Theme[] = ['auto', 'light', 'dark']

export function Settings() {
  const [git, setGit] = useState<GitStatus | null>(null)
  const [settings, setSettings] = useState<GitSettings | null>(null)
  const [remote, setRemote] = useState('')
  const [branch, setBranch] = useState('')
  const [language, setLanguage] = useState<GitLanguage>('ja')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    document.title = '設定 - reviewer'
    api.gitStatus()
      .then((s) => {
        setGit(s)
        setSettings(s.settings)
        setRemote(s.settings.remote || s.target.remote)
        setBranch(s.settings.branch || s.target.branch || s.branch)
        setLanguage(s.settings.language)
      })
      .catch((e) => setError((e as Error).message))
  }, [])

  const branches = useMemo(() => {
    if (!git) return []
    const selected = git.remotes.find((r) => r.name === remote)
    return [...new Set([git.branch, ...(selected?.branches ?? [])].filter(Boolean))]
  }, [git, remote])

  function selectRemote(value: string) {
    setRemote(value)
    const next = git?.remotes.find((r) => r.name === value)?.branches ?? []
    if (branch && !next.includes(branch) && branch !== git?.branch) setBranch(git?.branch || next[0] || '')
  }

  async function saveGit() {
    setBusy(true)
    setError(null)
    try {
      const saved = await api.saveGitSettings({ language, remote, branch })
      setSettings(saved)
      toast('Git の設定を保存しました', 'success')
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div class="settings-page">
      <div class="settings-head">
        <div>
          <h1>設定</h1>
          <p class="muted small">このプロジェクトと reviewer の表示設定を変更します。</p>
        </div>
        <a class="btn" href="#/">レビューに戻る</a>
      </div>
      {error && <div class="banner error">{error}</div>}

      <section class="card settings-section">
        <h2>表示</h2>
        <label class="settings-row">
          <span>
            <strong>テーマ</strong>
            <span class="muted small">自動はOSの設定に従います。</span>
          </span>
          <select value={theme.value} onChange={(e) => setTheme((e.currentTarget as HTMLSelectElement).value as Theme)}>
            {themes.map((t) => <option value={t}>{themeIcon[t]} {themeText[t]}</option>)}
          </select>
        </label>
      </section>

      <section class="card settings-section">
        <h2>Git</h2>
        {!git ? (
          <p class="muted">Gitの状態を読み込み中…</p>
        ) : !git.repo ? (
          <p class="muted">このプロジェクトはGitリポジトリの中にありません。</p>
        ) : (
          <>
            <p class="muted small">commitメッセージの言語と、通常のpush先を設定します。操作時には別のremote／branchへ一時的に切り替えられます。</p>
            <label class="settings-row">
              <span><strong>コミットメッセージ</strong><span class="muted small">Conventional Commits形式で生成します。</span></span>
              <select value={language} onChange={(e) => setLanguage((e.currentTarget as HTMLSelectElement).value as GitLanguage)}>
                <option value="ja">日本語</option>
                <option value="en">English</option>
              </select>
            </label>
            <label class="settings-row">
              <span><strong>デフォルト remote</strong><span class="muted small">URLではなく、登録済みのremoteから選択します。</span></span>
              <select value={remote} onChange={(e) => selectRemote((e.currentTarget as HTMLSelectElement).value)}>
                <option value="">自動（upstreamまたはorigin）</option>
                {git.remotes.map((r) => <option value={r.name}>{r.name} · {r.url}</option>)}
              </select>
            </label>
            <label class="settings-row">
              <span><strong>デフォルト branch</strong><span class="muted small">現在のbranch、または選択したremoteに存在するbranchから選択します。</span></span>
              <select value={branch} onChange={(e) => setBranch((e.currentTarget as HTMLSelectElement).value)}>
                <option value="">自動（現在のbranch）</option>
                {branches.map((b) => <option value={b}>{b}</option>)}
              </select>
            </label>
            <div class="settings-actions">
              <span class="muted small">{settings && `保存済み: ${settings.language} / ${settings.remote || '自動'} / ${settings.branch || '自動'}`}</span>
              <span class="spacer" />
              <button class="btn primary" disabled={busy} onClick={() => void saveGit()}>{busy ? '保存中…' : 'Git設定を保存'}</button>
            </div>
          </>
        )}
      </section>
    </div>
  )
}
