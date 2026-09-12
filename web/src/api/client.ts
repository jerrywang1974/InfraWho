import type {
  Account,
  ApiErrorBody,
  Asset,
  AssetDetail,
  AssetListResponse,
  AssetNote,
  CreateAccountInput,
  CreateAssetInput,
  CreateJobInput,
  CreateNoteInput,
  JobListResponse,
  NoteListResponse,
  PatchAccountInput,
  PatchJobInput,
  PatchNoteInput,
  RevealResponse,
  RotateSecretInput,
  ScheduledJob,
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

export function stepUp(password: string): Promise<void> {
  return request('/api/v1/auth/step-up', {
    method: 'POST',
    body: JSON.stringify({ password }),
  })
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

export async function getAsset(id: string): Promise<AssetDetail> {
  const raw = await request<AssetDetail>(`/api/v1/assets/${encodeURIComponent(id)}`)
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
    accounts: (raw.accounts ?? []).map((a) => ({
      id: a.id,
      username: a.username,
      auth_type: a.auth_type,
      description: a.description ?? '',
      last_rotated_at: a.last_rotated_at,
      has_secret: Boolean(a.has_secret),
    })),
    jobs: raw.jobs ?? [],
    notes: raw.notes ?? [],
  }
}

export function createAsset(body: CreateAssetInput): Promise<Asset> {
  return request('/api/v1/assets', {
    method: 'POST',
    body: JSON.stringify(body),
  })
}

export function createAccount(assetId: string, body: CreateAccountInput): Promise<Account> {
  return request(`/api/v1/assets/${encodeURIComponent(assetId)}/accounts`, {
    method: 'POST',
    body: JSON.stringify(body),
  })
}

export function patchAccount(id: string, body: PatchAccountInput): Promise<Account> {
  return request(`/api/v1/accounts/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body: JSON.stringify(body),
  })
}

export function deleteAccount(id: string): Promise<void> {
  return request(`/api/v1/accounts/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

export function revealAccount(id: string): Promise<RevealResponse> {
  return request(`/api/v1/accounts/${encodeURIComponent(id)}/reveal`, { method: 'POST' })
}

export function rotateSecret(id: string, body: RotateSecretInput): Promise<Account> {
  return request(`/api/v1/accounts/${encodeURIComponent(id)}/rotate-secret`, {
    method: 'POST',
    body: JSON.stringify(body),
  })
}

export function listJobs(
  assetId: string,
  params?: { limit?: number; offset?: number },
): Promise<JobListResponse> {
  const sp = new URLSearchParams()
  if (params?.limit != null) sp.set('limit', String(params.limit))
  if (params?.offset != null) sp.set('offset', String(params.offset))
  const qs = sp.toString()
  return request(`/api/v1/assets/${encodeURIComponent(assetId)}/jobs${qs ? `?${qs}` : ''}`)
}

export function createJob(assetId: string, body: CreateJobInput): Promise<ScheduledJob> {
  return request(`/api/v1/assets/${encodeURIComponent(assetId)}/jobs`, {
    method: 'POST',
    body: JSON.stringify(body),
  })
}

export function patchJob(id: string, body: PatchJobInput): Promise<ScheduledJob> {
  return request(`/api/v1/jobs/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body: JSON.stringify(body),
  })
}

export function deleteJob(id: string): Promise<void> {
  return request(`/api/v1/jobs/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

export function listNotes(
  assetId: string,
  params?: { limit?: number; offset?: number },
): Promise<NoteListResponse> {
  const sp = new URLSearchParams()
  if (params?.limit != null) sp.set('limit', String(params.limit))
  if (params?.offset != null) sp.set('offset', String(params.offset))
  const qs = sp.toString()
  return request(`/api/v1/assets/${encodeURIComponent(assetId)}/notes${qs ? `?${qs}` : ''}`)
}

export function createNote(assetId: string, body: CreateNoteInput): Promise<AssetNote> {
  return request(`/api/v1/assets/${encodeURIComponent(assetId)}/notes`, {
    method: 'POST',
    body: JSON.stringify(body),
  })
}

export function patchNote(id: string, body: PatchNoteInput): Promise<AssetNote> {
  return request(`/api/v1/notes/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body: JSON.stringify(body),
  })
}

export function deleteNote(id: string): Promise<void> {
  return request(`/api/v1/notes/${encodeURIComponent(id)}`, { method: 'DELETE' })
}
