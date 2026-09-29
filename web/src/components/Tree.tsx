import { useMemo, useState } from 'preact/hooks'
import { comments, info, tree } from '../state'
import { fileHref, route } from '../router'
import type { TreeFile } from '../types'
import type { JSX } from 'preact'

interface Dir {
  name: string
  path: string
  dirs: Map<string, Dir>
  files: TreeFile[]
}

function build(files: TreeFile[]): Dir {
  const root: Dir = { name: '', path: '', dirs: new Map(), files: [] }
  for (const f of files) {
    const parts = f.path.split('/')
    let d = root
    for (const part of parts.slice(0, -1)) {
      const p = d.path ? `${d.path}/${part}` : part
      if (!d.dirs.has(part)) d.dirs.set(part, { name: part, path: p, dirs: new Map(), files: [] })
      d = d.dirs.get(part)!
    }
    d.files.push(f)
  }
  return root
}

function count(d: Dir): number {
  let n = d.files.reduce((a, f) => a + f.unresolved, 0)
  for (const c of d.dirs.values()) n += count(c)
  return n
}

export function Tree() {
  const [filter, setFilter] = useState('')
  const [onlyChanged, setOnlyChanged] = useState(false)
  const [onlyCommented, setOnlyCommented] = useState(false)
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set())
  const hasBase = (info.value?.baseRound ?? 0) > 0

  const files = tree.value.filter(
    (f) =>
      (!filter || f.path.toLowerCase().includes(filter.toLowerCase())) &&
      (!onlyChanged || f.changed || f.new) &&
      (!onlyCommented || f.unresolved > 0),
  )
  const root = useMemo(() => build(files), [files.map((f) => `${f.path}${f.unresolved}${f.changed}${f.new}`).join('|')])
  const projectCount = comments.value.filter((c) => c.scope === 'project' && c.status !== 'resolved').length
  const current = route.value.page === 'file' ? route.value.path : null

  function toggle(p: string) {
    const next = new Set(collapsed)
    next.has(p) ? next.delete(p) : next.add(p)
    setCollapsed(next)
  }

  function renderDir(d: Dir, depth: number): JSX.Element {
    const dirs = [...d.dirs.values()].sort((a, b) => a.name.localeCompare(b.name))
    return (
      <>
        {dirs.map((sub): JSX.Element => {
          const isCollapsed = collapsed.has(sub.path) && !filter
          const n = count(sub)
          return (
            <>
              <button class="tree-row dir" style={{ paddingLeft: `${depth * 14 + 8}px` }} onClick={() => toggle(sub.path)}>
                <span class="caret">{isCollapsed ? '▸' : '▾'}</span>
                <span class="name">{sub.name}/</span>
                {n > 0 && isCollapsed && <span class="count">{n}</span>}
              </button>
              {!isCollapsed && renderDir(sub, depth + 1)}
            </>
          )
        })}
        {d.files.map((f) => (
          <a
            class={`tree-row file ${current === f.path ? 'active' : ''}`}
            style={{ paddingLeft: `${depth * 14 + 22}px` }}
            href={fileHref(f.path)}
            title={f.path}
          >
            <span class="name">{f.path.split('/').pop()}</span>
            {f.new && <span class="mark new" title="前回の提出後に追加されたファイル">新規</span>}
            {f.changed && <span class="mark changed" title="前回の提出後に変更されたファイル">●</span>}
            {f.unresolved > 0 && <span class="count">{f.unresolved}</span>}
          </a>
        ))}
      </>
    )
  }

  return (
    <nav class="tree">
      <a class={`tree-row overview ${route.value.page === 'overview' ? 'active' : ''}`} href="#/">
        <span class="name">📋 概要・全体コメント</span>
        {projectCount > 0 && <span class="count">{projectCount}</span>}
      </a>
      <div class="tree-filter">
        <input type="search" placeholder="ファイルを絞り込む" value={filter} onInput={(e) => setFilter(e.currentTarget.value)} />
        <div class="tree-toggles">
          {hasBase && (
            <label>
              <input type="checkbox" checked={onlyChanged} onChange={(e) => setOnlyChanged(e.currentTarget.checked)} /> 変更あり
            </label>
          )}
          <label>
            <input type="checkbox" checked={onlyCommented} onChange={(e) => setOnlyCommented(e.currentTarget.checked)} /> コメントあり
          </label>
        </div>
      </div>
      <div class="tree-list">
        {renderDir(root, 0)}
        {files.length === 0 && <div class="empty small">該当するファイルがありません</div>}
      </div>
    </nav>
  )
}
