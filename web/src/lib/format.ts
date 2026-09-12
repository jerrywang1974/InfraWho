import type { User } from '../api/types'

const statusLabels: Record<string, string> = {
  active: '運作中',
  unknown: '未知',
  retired: '已除役',
}

const envLabels: Record<string, string> = {
  prod: '正式',
  staging: '預備',
  dev: '開發',
  lab: '實驗',
  other: '其他',
}

const assetTypeLabels: Record<string, string> = {
  physical: '實體機',
  vm: '虛擬機',
  other: '其他',
}

const osFamilyLabels: Record<string, string> = {
  linux: 'Linux',
  windows: 'Windows',
  other: '其他',
}

const roleLabels: Record<string, string> = {
  admin: '管理員',
  operator: '操作者',
  viewer: '唯讀',
}

export function formatOwner(ownerId: string | null | undefined, me: User | null): string {
  if (!ownerId) return '未指定'
  if (me && me.id === ownerId) {
    return me.display_name || me.username
  }
  // No users list API in this PR — truncated id; title/hint clarifies it is an ID.
  const short = ownerId.length > 12 ? `${ownerId.slice(0, 8)}…` : ownerId
  return `使用者 ID ${short}`
}

export function formatOS(family: string, detail: string): string {
  const f = osFamilyLabels[family] || family || '—'
  if (!detail) return f
  return `${f} / ${detail}`
}

export function formatStatus(status: string): string {
  return statusLabels[status] || status
}

export function formatEnvironment(env: string): string {
  return envLabels[env] || env
}

export function formatAssetType(t: string): string {
  return assetTypeLabels[t] || t
}

export function formatRole(role: string): string {
  return roleLabels[role] || role
}
