export type ApiErrorBody = {
  error: {
    code: string
    message: string
  }
}

export type User = {
  id: string
  username: string
  display_name: string
  role: string
  step_up_active: boolean
}

export type SetupStatus = {
  needs_bootstrap: boolean
  master_key_ready: boolean
  checklist_complete: boolean
  show_banner: boolean
}

export type Asset = {
  id: string
  name: string
  hostname: string
  asset_type: string
  os_family: string
  os_detail: string
  environment: string
  purpose: string
  primary_ip: string
  additional_ips: string[]
  location: string
  hypervisor: string
  owner_id: string | null
  backup_owner_id: string | null
  status: string
  config_notes: string
  tags: string[]
  deleted_at?: string | null
  created_at: string
  updated_at: string
}

export type AccountSummary = {
  id: string
  username: string
  auth_type: string
  description: string
  last_rotated_at?: string | null
  has_secret: boolean
}

export type JobSummary = {
  id: string
  name: string
  scheduler_type: string
  enabled_doc: boolean
}

export type NoteSummary = {
  id: string
  title: string
  created_at: string
}

export type AssetDetail = Asset & {
  accounts: AccountSummary[]
  jobs: JobSummary[]
  notes: NoteSummary[]
}

export type AssetListResponse = {
  items: Asset[]
  limit: number
  offset: number
  total: number
}

export type CreateAssetInput = {
  name: string
  hostname: string
  asset_type: string
  os_family: string
  os_detail?: string
  environment: string
  purpose: string
  primary_ip?: string
  owner_id?: string | null
  status?: string
  tags?: string[]
}

export type Account = {
  id: string
  asset_id: string
  username: string
  auth_type: string
  description: string
  last_rotated_at?: string | null
  has_secret: boolean
  created_at: string
  updated_at: string
}

export type CreateAccountInput = {
  username: string
  auth_type: string
  description?: string
  secret?: string
}

export type PatchAccountInput = {
  username?: string
  auth_type?: string
  description?: string
}

export type RotateSecretInput = {
  secret: string
  auth_type?: string
}

export type RevealResponse = {
  account_id: string
  username: string
  auth_type: string
  secret: string
  revealed_at: string
}

export type ScheduledJob = {
  id: string
  asset_id: string
  name: string
  scheduler_type: string
  schedule_expr: string
  command_or_path: string
  description: string
  enabled_doc: boolean
  created_at: string
  updated_at: string
}

export type CreateJobInput = {
  name: string
  scheduler_type: string
  schedule_expr?: string
  command_or_path?: string
  description?: string
  enabled_doc?: boolean
}

export type PatchJobInput = {
  name?: string
  scheduler_type?: string
  schedule_expr?: string
  command_or_path?: string
  description?: string
  enabled_doc?: boolean
}

export type JobListResponse = {
  items: ScheduledJob[]
  limit: number
  offset: number
  total: number
}

export type AssetNote = {
  id: string
  asset_id: string
  title: string
  body: string
  author_id: string | null
  created_at: string
  updated_at: string
}

export type CreateNoteInput = {
  title?: string
  body?: string
}

export type PatchNoteInput = {
  title?: string
  body?: string
}

export type NoteListResponse = {
  items: AssetNote[]
  limit: number
  offset: number
  total: number
}
