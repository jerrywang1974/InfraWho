import { useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import * as api from '../api/client'
import { ApiError } from '../api/client'
import type { AssetDetail } from '../api/types'
import { useAuth } from '../auth/AuthContext'
import { PlaceholderPanel } from '../components/PlaceholderPanel'
import { formatOwner, formatOS } from '../lib/format'

export function AssetDetailPage() {
  const { id = '' } = useParams()
  const { user } = useAuth()
  const [asset, setAsset] = useState<AssetDetail | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let cancelled = false
    async function load() {
      setLoading(true)
      setError(null)
      try {
        const d = await api.getAsset(id)
        if (!cancelled) setAsset(d)
      } catch (err) {
        if (!cancelled) {
          if (err instanceof ApiError) {
            setError(err.status === 404 ? '找不到此資產' : err.message)
          } else {
            setError('無法載入資產')
          }
        }
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    if (id) void load()
    return () => {
      cancelled = true
    }
  }, [id])

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
        <span className={`status-chip ${asset.status === 'active' ? 'ok' : ''}`}>{asset.status}</span>
      </div>

      <section className="panel">
        <h2>主資料</h2>
        <dl className="kv">
          <dt>用途</dt>
          <dd className="pre-wrap">{asset.purpose || '—'}</dd>
          <dt>負責人</dt>
          <dd>{formatOwner(asset.owner_id, user)}</dd>
          <dt>備援負責人</dt>
          <dd>{formatOwner(asset.backup_owner_id, user)}</dd>
          <dt>作業系統</dt>
          <dd>{formatOS(asset.os_family, asset.os_detail)}</dd>
          <dt>類型</dt>
          <dd>{asset.asset_type}</dd>
          <dt>環境</dt>
          <dd>{asset.environment}</dd>
          <dt>位置</dt>
          <dd>{asset.location || '—'}</dd>
          <dt>Hypervisor</dt>
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
      </section>

      <div className="panel-grid">
        <PlaceholderPanel title="帳號／憑證" />
        <PlaceholderPanel title="排程工作" />
        <PlaceholderPanel title="備註" />
      </div>
    </div>
  )
}
