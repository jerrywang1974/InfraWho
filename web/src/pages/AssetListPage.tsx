import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { Link } from 'react-router-dom'
import * as api from '../api/client'
import { ApiError } from '../api/client'
import type { Asset } from '../api/types'
import { useAuth } from '../auth/AuthContext'
import { formatOwner, formatOS } from '../lib/format'

const PAGE_SIZE = 50

export function AssetListPage() {
  const { user } = useAuth()
  const [items, setItems] = useState<Asset[]>([])
  const [total, setTotal] = useState(0)
  const [offset, setOffset] = useState(0)
  const [q, setQ] = useState('')
  const [qDraft, setQDraft] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [showCreate, setShowCreate] = useState(false)

  const canWrite = user?.role === 'admin' || user?.role === 'operator'

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const res = await api.listAssets({ q: q || undefined, limit: PAGE_SIZE, offset })
      setItems(res.items)
      setTotal(res.total)
    } catch (err) {
      if (err instanceof ApiError) {
        setError(err.message)
      } else {
        setError('無法載入資產列表')
      }
    } finally {
      setLoading(false)
    }
  }, [q, offset])

  useEffect(() => {
    void load()
  }, [load])

  function onSearch(e: FormEvent) {
    e.preventDefault()
    setOffset(0)
    setQ(qDraft.trim())
  }

  return (
    <div className="page">
      <div className="page-header">
        <div>
          <h1>資產</h1>
          <p className="muted">用途、負責人與作業系統一覽</p>
        </div>
        {canWrite ? (
          <button type="button" className="btn btn-primary" onClick={() => setShowCreate((v) => !v)}>
            {showCreate ? '關閉表單' : '新增資產'}
          </button>
        ) : null}
      </div>

      {showCreate && canWrite ? (
        <CreateAssetForm
          defaultOwnerId={user?.id ?? null}
          onCreated={() => {
            setShowCreate(false)
            setOffset(0)
            void load()
          }}
        />
      ) : null}

      <form className="toolbar" onSubmit={onSearch}>
        <input
          className="grow"
          placeholder="搜尋名稱、hostname、用途…"
          value={qDraft}
          onChange={(e) => setQDraft(e.target.value)}
        />
        <button className="btn" type="submit">
          搜尋
        </button>
      </form>

      {error ? <p className="error">{error}</p> : null}
      {loading ? <p className="muted">載入中…</p> : null}

      {!loading && items.length === 0 ? (
        <p className="muted">尚無資產。{canWrite ? '可點「新增資產」建立第一筆。' : ''}</p>
      ) : null}

      {items.length > 0 ? (
        <div className="table-wrap">
          <table className="data-table">
            <thead>
              <tr>
                <th>名稱</th>
                <th>Hostname</th>
                <th>用途</th>
                <th>負責人</th>
                <th>作業系統</th>
                <th>環境</th>
                <th>狀態</th>
              </tr>
            </thead>
            <tbody>
              {items.map((a) => (
                <tr key={a.id}>
                  <td>
                    <Link to={`/assets/${a.id}`}>{a.name}</Link>
                  </td>
                  <td>
                    <code>{a.hostname}</code>
                  </td>
                  <td className="clamp">{a.purpose || '—'}</td>
                  <td>{formatOwner(a.owner_id, user)}</td>
                  <td>{formatOS(a.os_family, a.os_detail)}</td>
                  <td>{a.environment}</td>
                  <td>{a.status}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}

      {total > PAGE_SIZE ? (
        <div className="pager">
          <button
            type="button"
            className="btn"
            disabled={offset <= 0}
            onClick={() => setOffset((o) => Math.max(0, o - PAGE_SIZE))}
          >
            上一頁
          </button>
          <span className="muted">
            {offset + 1}–{Math.min(offset + PAGE_SIZE, total)} / {total}
          </span>
          <button
            type="button"
            className="btn"
            disabled={offset + PAGE_SIZE >= total}
            onClick={() => setOffset((o) => o + PAGE_SIZE)}
          >
            下一頁
          </button>
        </div>
      ) : null}
    </div>
  )
}

function CreateAssetForm({
  defaultOwnerId,
  onCreated,
}: {
  defaultOwnerId: string | null
  onCreated: () => void
}) {
  const [name, setName] = useState('')
  const [hostname, setHostname] = useState('')
  const [purpose, setPurpose] = useState('')
  const [osFamily, setOsFamily] = useState('linux')
  const [osDetail, setOsDetail] = useState('')
  const [environment, setEnvironment] = useState('lab')
  const [assetType, setAssetType] = useState('vm')
  const [assignSelf, setAssignSelf] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setError(null)
    setSubmitting(true)
    try {
      await api.createAsset({
        name: name.trim(),
        hostname: hostname.trim(),
        purpose: purpose.trim() || undefined,
        os_family: osFamily,
        os_detail: osDetail.trim() || undefined,
        environment,
        asset_type: assetType,
        owner_id: assignSelf && defaultOwnerId ? defaultOwnerId : null,
        status: 'active',
      })
      onCreated()
    } catch (err) {
      if (err instanceof ApiError) {
        setError(err.message)
      } else {
        setError('建立失敗')
      }
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <form className="card form-card" onSubmit={onSubmit}>
      <h2>新增資產</h2>
      <div className="form-grid">
        <label>
          名稱
          <input value={name} onChange={(e) => setName(e.target.value)} required />
        </label>
        <label>
          Hostname
          <input value={hostname} onChange={(e) => setHostname(e.target.value)} required />
        </label>
        <label className="span-2">
          用途
          <textarea value={purpose} onChange={(e) => setPurpose(e.target.value)} rows={2} />
        </label>
        <label>
          類型
          <select value={assetType} onChange={(e) => setAssetType(e.target.value)}>
            <option value="physical">實體機</option>
            <option value="vm">虛擬機</option>
            <option value="other">其他</option>
          </select>
        </label>
        <label>
          環境
          <select value={environment} onChange={(e) => setEnvironment(e.target.value)}>
            <option value="prod">prod</option>
            <option value="staging">staging</option>
            <option value="dev">dev</option>
            <option value="lab">lab</option>
            <option value="other">other</option>
          </select>
        </label>
        <label>
          作業系統
          <select value={osFamily} onChange={(e) => setOsFamily(e.target.value)}>
            <option value="linux">Linux</option>
            <option value="windows">Windows</option>
            <option value="other">其他</option>
          </select>
        </label>
        <label>
          OS 細節
          <input
            value={osDetail}
            onChange={(e) => setOsDetail(e.target.value)}
            placeholder="例如 Ubuntu 22.04"
          />
        </label>
        <label className="check span-2">
          <input
            type="checkbox"
            checked={assignSelf}
            onChange={(e) => setAssignSelf(e.target.checked)}
            disabled={!defaultOwnerId}
          />
          將目前使用者設為負責人
        </label>
      </div>
      {error ? <p className="error">{error}</p> : null}
      <button className="btn btn-primary" type="submit" disabled={submitting}>
        {submitting ? '建立中…' : '建立'}
      </button>
    </form>
  )
}
