import type {
  ApiErrorBody,
  Asset,
  AssetListResponse,
  CreateAssetInput,
  SetupStatus,
  User,
} from './types'

export class ApiError extends Error {
  status: number
  code: string

  constructor(status: number, code: string, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
  }
}

async function parseError(res: Response): Promise<ApiError> {
  try {
    const body = (await res.json()) as ApiErrorBody
    if (body?.error?.code) {
      return new ApiError(res.status, body.error.code, body.error.message || res.statusText)
    }
  } catch {
    // fall through
  }
  return new ApiError(res.status, 'http_error', res.statusText || 'Request failed')
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const headers = new Headers(init?.headers)
  if (init?.body && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }
  const res = await fetch(path, {
    ...init,
    headers,
    credentials: 'include',
  })
  if (!res.ok) {
    throw await parseError(res)
  }
  if (res.status === 204) {
    return undefined as T
  }
  const text = await res.text()
  if (!text) {
    return undefined as T
  }
  return JSON.parse(text) as T
}

export function getSetupStatus(): Promise<SetupStatus> {
  return request('/api/v1/setup/status')
}

export function bootstrap(body: {
  username: string
  password: string
  display_name?: string
  acknowledge_kek_offline: boolean
  acknowledge_kek_irrecoverable: boolean
  acknowledge_backup_planned: boolean
}): Promise<User> {
  return request('/api/v1/setup/bootstrap', {
    method: 'POST',
    body: JSON.stringify(body),
  })
}

export function acknowledgeChecklist(body: {
  acknowledge_kek_offline: boolean
  acknowledge_kek_irrecoverable: boolean
  acknowledge_backup_planned: boolean
}): Promise<void> {
  return request('/api/v1/setup/acknowledge', {
    method: 'POST',
    body: JSON.stringify(body),
  })
}

export function login(username: string, password: string): Promise<User> {
  return request('/api/v1/auth/login', {
    method: 'POST',
    body: JSON.stringify({ username, password }),
  })
}

export function logout(): Promise<void> {
  return request('/api/v1/auth/logout', { method: 'POST' })
}

export function me(): Promise<User> {
  return request('/api/v1/auth/me')
}

export function listAssets(params?: {
  q?: string
  limit?: number
  offset?: number
  environment?: string
}): Promise<AssetListResponse> {
  const sp = new URLSearchParams()
  if (params?.q) sp.set('q', params.q)
  if (params?.limit != null) sp.set('limit', String(params.limit))
  if (params?.offset != null) sp.set('offset', String(params.offset))
  if (params?.environment) sp.set('environment', params.environment)
  const qs = sp.toString()
  return request(`/api/v1/assets${qs ? `?${qs}` : ''}`)
}

/** Asset master data only — nested accounts/jobs/notes summaries are dropped until PR 10. */
export async function getAsset(id: string): Promise<Asset> {
  const raw = await request<Asset>(`/api/v1/assets/${encodeURIComponent(id)}`)
  return {
    id: raw.id,
    name: raw.name,
    hostname: raw.hostname,
    asset_type: raw.asset_type,
    os_family: raw.os_family,
    os_detail: raw.os_detail,
    environment: raw.environment,
    purpose: raw.purpose,
    primary_ip: raw.primary_ip,
    additional_ips: raw.additional_ips ?? [],
    location: raw.location,
    hypervisor: raw.hypervisor,
    owner_id: raw.owner_id,
    backup_owner_id: raw.backup_owner_id,
    status: raw.status,
    config_notes: raw.config_notes,
    tags: raw.tags ?? [],
    deleted_at: raw.deleted_at,
    created_at: raw.created_at,
    updated_at: raw.updated_at,
  }
}

export function createAsset(body: CreateAssetInput): Promise<Asset> {
  return request('/api/v1/assets', {
    method: 'POST',
    body: JSON.stringify(body),
  })
}
