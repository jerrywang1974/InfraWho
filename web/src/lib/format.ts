import type { User } from '../api/types'

export function formatOwner(ownerId: string | null | undefined, me: User | null): string {
  if (!ownerId) return '未指定'
  if (me && me.id === ownerId) {
    return me.display_name || me.username
  }
  // No users list API in this PR — show truncated id.
  return ownerId.length > 12 ? `${ownerId.slice(0, 8)}…` : ownerId
}

export function formatOS(family: string, detail: string): string {
  const f = family || '—'
  if (!detail) return f
  return `${f} / ${detail}`
}
