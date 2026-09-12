import { useCallback, useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import * as api from '../api/client'
import { ApiError } from '../api/client'
import type { AssetDetail } from '../api/types'
import { useAuth } from '../auth/AuthContext'
import { AccountsPanel } from '../components/AccountsPanel'
import { JobsPanel } from '../components/JobsPanel'
import { NotesPanel } from '../components/NotesPanel'
import {
  formatAssetType,
  formatEnvironment,
  formatOS,
  formatOwner,
  formatStatus,
} from '../lib/format'

export function AssetDetailPage() {
  const { id = '' } = useParams()
  const { user } = useAuth()
  const [asset, setAsset] = useState<AssetDetail | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  const canWrite = user?.role === 'admin' || user?.role === 'operator'
  const canReveal = canWrite

  const load = useCallback(async (opts?: { soft?: boolean }) => {
    if (!id) return
    if (!opts?.soft) {
      setLoading(true)
      setError(null)
    }
    try {
      const d = await api.getAsset(id)
      setAsset(d)
      setError(null)
    } catch (err) {
      if (err instanceof ApiError) {
        setError(err.status === 404 ? '找不到此資產' : err.message)
      } else {
        setError('無法載入資產')
      }
      if (!opts?.soft) setAsset(null)
    } finally {
      if (!opts?.soft) setLoading(false)
    }
  }, [id])

  useEffect(() => {
    void load()
  }, [load])

  if (loading) {
    return (
      <div className="page">
        <p className="muted">載入中…</p>
      </div>
    )
  }

  if (error || !asset) {
    return (
      <div className="page">
        <p className="error">{error || '找不到此資產'}</p>
        <Link to="/">← 返回列表</Link>
      </div>
    )
  }

  return (
    <div className="page">
      <p>
        <Link to="/">← 資產列表</Link>
      </p>

      <div className="page-header">
        <div>
          <h1>{asset.name}</h1>
          <p className="muted">
            <code>{asset.hostname}</code>
            {asset.primary_ip ? ` · ${asset.primary_ip}` : ''}
          </p>
        </div>
        <span className={`status-chip ${asset.status === 'active' ? 'ok' : ''}`}>
          {formatStatus(asset.status)}
        </span>
      </div>

      <section className="panel">
        <h2>主資料</h2>
        <dl className="kv">
          <dt>用途</dt>
          <dd className="pre-wrap">{asset.purpose || '—'}</dd>
          <dt>負責人</dt>
          <dd title={asset.owner_id ?? undefined}>{formatOwner(asset.owner_id, user)}</dd>
          <dt>備援負責人</dt>
          <dd title={asset.backup_owner_id ?? undefined}>
            {formatOwner(asset.backup_owner_id, user)}
          </dd>
          <dt>作業系統</dt>
          <dd>{formatOS(asset.os_family, asset.os_detail)}</dd>
          <dt>類型</dt>
          <dd>{formatAssetType(asset.asset_type)}</dd>
          <dt>環境</dt>
          <dd>{formatEnvironment(asset.environment)}</dd>
          <dt>位置</dt>
          <dd>{asset.location || '—'}</dd>
          <dt>虛擬化平台</dt>
          <dd>{asset.hypervisor || '—'}</dd>
          <dt>標籤</dt>
          <dd>
            {asset.tags?.length
              ? asset.tags.map((t) => (
                  <span key={t} className="tag">
                    {t}
                  </span>
                ))
              : '—'}
          </dd>
          <dt>設定備註</dt>
          <dd className="pre-wrap">{asset.config_notes || '—'}</dd>
        </dl>
        <p className="muted hint">
          其他負責人僅顯示使用者 ID（尚無使用者列表 API）。
        </p>
      </section>

      <AccountsPanel
        key={asset.id}
        assetId={asset.id}
        accounts={asset.accounts}
        canWrite={canWrite}
        canReveal={canReveal}
        onChanged={() => void load({ soft: true })}
      />
      <JobsPanel key={`jobs-${asset.id}`} assetId={asset.id} canWrite={canWrite} />
      <NotesPanel key={`notes-${asset.id}`} assetId={asset.id} canWrite={canWrite} />
    </div>
  )
}
