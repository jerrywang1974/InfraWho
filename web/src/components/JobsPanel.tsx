import { useCallback, useEffect, useState, type FormEvent } from 'react'
import * as api from '../api/client'
import { ApiError } from '../api/client'
import type { ScheduledJob } from '../api/types'
import { formatSchedulerType } from '../lib/format'

const SCHEDULER_TYPES = [
  { value: 'cron', label: 'cron' },
  { value: 'systemd_timer', label: 'systemd timer' },
  { value: 'windows_task', label: 'Windows 工作排程' },
  { value: 'other', label: '其他' },
] as const

type Props = {
  assetId: string
  canWrite: boolean
}

export function JobsPanel({ assetId, canWrite }: Props) {
  const [items, setItems] = useState<ScheduledJob[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [showCreate, setShowCreate] = useState(false)
  const [busyId, setBusyId] = useState<string | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const res = await api.listJobs(assetId, { limit: 200 })
      setItems(res.items)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '無法載入排程工作')
    } finally {
      setLoading(false)
    }
  }, [assetId])

  useEffect(() => {
    void load()
  }, [load])

  async function onDelete(id: string) {
    if (!window.confirm('確定刪除此排程文件？')) return
    setBusyId(id)
    setError(null)
    try {
      await api.deleteJob(id)
      await load()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '刪除失敗')
    } finally {
      setBusyId(null)
    }
  }

  async function toggleEnabled(job: ScheduledJob) {
    setBusyId(job.id)
    setError(null)
    try {
      await api.patchJob(job.id, { enabled_doc: !job.enabled_doc })
      await load()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '更新失敗')
    } finally {
      setBusyId(null)
    }
  }

  return (
    <section className="panel panel-wide">
      <div className="panel-header">
        <h2>排程工作</h2>
        {canWrite ? (
          <button
            type="button"
            className="btn btn-primary"
            onClick={() => setShowCreate((v) => !v)}
          >
            {showCreate ? '關閉表單' : '新增排程'}
          </button>
        ) : null}
      </div>
      <p className="muted hint">文件化紀錄（documented, not verified）— 非自動發現。</p>

      {showCreate && canWrite ? (
        <CreateJobForm
          assetId={assetId}
          onCreated={() => {
            setShowCreate(false)
            void load()
          }}
          onCancel={() => setShowCreate(false)}
        />
      ) : null}

      {error ? <p className="error">{error}</p> : null}
      {loading ? <p className="muted">載入中…</p> : null}

      {!loading && items.length === 0 ? <p className="muted">尚無排程文件。</p> : null}

      <ul className="item-list">
        {items.map((job) => (
          <li key={job.id} className="item-card">
            <div className="item-main">
              <div className="item-title">
                <strong>{job.name}</strong>
                <span className="tag">{formatSchedulerType(job.scheduler_type)}</span>
                <span className={`status-chip ${job.enabled_doc ? 'ok' : ''}`}>
                  {job.enabled_doc ? '文件：啟用' : '文件：停用'}
                </span>
              </div>
              {job.schedule_expr ? (
                <p>
                  <code>{job.schedule_expr}</code>
                </p>
              ) : null}
              {job.command_or_path ? (
                <p className="pre-wrap mono-sm">{job.command_or_path}</p>
              ) : null}
              {job.description ? <p className="muted pre-wrap">{job.description}</p> : null}
            </div>
            {canWrite ? (
              <div className="item-actions">
                <button
                  type="button"
                  className="btn"
                  disabled={busyId === job.id}
                  onClick={() => void toggleEnabled(job)}
                >
                  {job.enabled_doc ? '標為停用' : '標為啟用'}
                </button>
                <button
                  type="button"
                  className="btn btn-danger"
                  disabled={busyId === job.id}
                  onClick={() => void onDelete(job.id)}
                >
                  刪除
                </button>
              </div>
            ) : null}
          </li>
        ))}
      </ul>
    </section>
  )
}

function CreateJobForm({
  assetId,
  onCreated,
  onCancel,
}: {
  assetId: string
  onCreated: () => void
  onCancel: () => void
}) {
  const [name, setName] = useState('')
  const [schedulerType, setSchedulerType] = useState('cron')
  const [scheduleExpr, setScheduleExpr] = useState('')
  const [commandOrPath, setCommandOrPath] = useState('')
  const [description, setDescription] = useState('')
  const [enabledDoc, setEnabledDoc] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      await api.createJob(assetId, {
        name: name.trim(),
        scheduler_type: schedulerType,
        schedule_expr: scheduleExpr,
        command_or_path: commandOrPath,
        description,
        enabled_doc: enabledDoc,
      })
      onCreated()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '建立失敗')
    } finally {
      setBusy(false)
    }
  }

  return (
    <form className="form-card nested-form" onSubmit={onSubmit}>
      <h3>新增排程文件</h3>
      <div className="form-grid">
        <label>
          名稱
          <input value={name} onChange={(e) => setName(e.target.value)} required />
        </label>
        <label>
          排程類型
          <select value={schedulerType} onChange={(e) => setSchedulerType(e.target.value)}>
            {SCHEDULER_TYPES.map((t) => (
              <option key={t.value} value={t.value}>
                {t.label}
              </option>
            ))}
          </select>
        </label>
        <label className="span-2">
          排程表達式
          <input
            value={scheduleExpr}
            onChange={(e) => setScheduleExpr(e.target.value)}
            placeholder="例如 0 2 * * *"
          />
        </label>
        <label className="span-2">
          命令／路徑
          <textarea
            rows={2}
            value={commandOrPath}
            onChange={(e) => setCommandOrPath(e.target.value)}
          />
        </label>
        <label className="span-2">
          說明
          <textarea rows={2} value={description} onChange={(e) => setDescription(e.target.value)} />
        </label>
        <label className="check span-2">
          <input
            type="checkbox"
            checked={enabledDoc}
            onChange={(e) => setEnabledDoc(e.target.checked)}
          />
          <span>文件狀態：啟用（非實際驗證）</span>
        </label>
      </div>
      {error ? <p className="error">{error}</p> : null}
      <div className="row-actions">
        <button type="button" className="btn btn-ghost" onClick={onCancel} disabled={busy}>
          取消
        </button>
        <button type="submit" className="btn btn-primary" disabled={busy}>
          {busy ? '建立中…' : '建立'}
        </button>
      </div>
    </form>
  )
}
