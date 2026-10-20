import type { Attachment } from '../types'
import { api } from '../api'

export function Attachments({
  items,
  onRemove,
}: {
  items: Attachment[]
  onRemove?: (id: string) => void
}) {
  if (items.length === 0) return null
  return (
    <div class="attachments" aria-label="添付画像">
      {items.map((item) => (
        <div class="attachment" key={item.id}>
          <a href={api.attachmentUrl(item.id)} target="_blank" rel="noreferrer" title={item.name}>
            <img src={api.attachmentUrl(item.id)} alt={item.name} loading="lazy" />
            <span>{item.name}</span>
          </a>
          {onRemove && (
            <button type="button" class="attachment-remove" aria-label={`${item.name}を削除`} onClick={() => onRemove(item.id)}>
              ×
            </button>
          )}
        </div>
      ))}
    </div>
  )
}
