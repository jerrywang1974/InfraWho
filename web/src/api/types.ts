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
