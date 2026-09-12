import { useCallback, useEffect, useState, type FormEvent } from 'react'
import * as api from '../api/client'
import { ApiError } from '../api/client'
import type { AssetNote } from '../api/types'

type Props = {
  assetId: string
  canWrite: boolean
}

export function NotesPanel({ assetId, canWrite }: Props) {
  const [items, setItems] = useState<AssetNote[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [showCreate, setShowCreate] = useState(false)
  const [busyId, setBusyId] = useState<string | null>(null)
  const [editingId, setEditingId] = useState<string | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const res = await api.listNotes(assetId, { limit: 200 })
      setItems(res.items)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '無法載入備註')
    } finally {
      setLoading(false)
    }
  }, [assetId])

  useEffect(() => {
    void load()
  }, [load])

  async function onDelete(id: string) {
    if (!window.confirm('確定刪除此備註？')) return
    setBusyId(id)
    setError(null)
    try {
      await api.deleteNote(id)
      await load()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '刪除失敗')
    } finally {
      setBusyId(null)
    }
  }

  return (
    <section className="panel panel-wide">
      <div className="panel-header">
        <h2>備註</h2>
        {canWrite ? (
          <button
            type="button"
            className="btn btn-primary"
            onClick={() => setShowCreate((v) => !v)}
          >
            {showCreate ? '關閉表單' : '新增備註'}
          </button>
        ) : null}
      </div>
      <p className="muted hint">Markdown 純文字儲存（Phase 1 以純文字顯示）。</p>

      {showCreate && canWrite ? (
        <NoteForm
          submitLabel="建立"
          onSubmit={async (title, body) => {
            await api.createNote(assetId, { title, body })
            setShowCreate(false)
            await load()
          }}
          onCancel={() => setShowCreate(false)}
        />
      ) : null}

      {error ? <p className="error">{error}</p> : null}
      {loading ? <p className="muted">載入中…</p> : null}
      {!loading && items.length === 0 ? <p className="muted">尚無備註。</p> : null}

      <ul className="item-list">
        {items.map((note) => (
          <li key={note.id} className="item-card">
            {editingId === note.id ? (
              <NoteForm
                initialTitle={note.title}
                initialBody={note.body}
                submitLabel="儲存"
                onSubmit={async (title, body) => {
                  await api.patchNote(note.id, { title, body })
                  setEditingId(null)
                  await load()
                }}
                onCancel={() => setEditingId(null)}
              />
            ) : (
              <>
                <div className="item-main">
                  <div className="item-title">
                    <strong>{note.title || '（無標題）'}</strong>
                    <span className="muted hint">{note.updated_at}</span>
                  </div>
                  <pre className="note-body pre-wrap">{note.body || '—'}</pre>
                </div>
                {canWrite ? (
                  <div className="item-actions">
                    <button type="button" className="btn" onClick={() => setEditingId(note.id)}>
                      編輯
                    </button>
                    <button
                      type="button"
                      className="btn btn-danger"
                      disabled={busyId === note.id}
                      onClick={() => void onDelete(note.id)}
                    >
                      刪除
                    </button>
                  </div>
                ) : null}
              </>
            )}
          </li>
        ))}
      </ul>
    </section>
  )
}

function NoteForm({
  initialTitle = '',
  initialBody = '',
  submitLabel,
  onSubmit,
  onCancel,
}: {
  initialTitle?: string
  initialBody?: string
  submitLabel: string
  onSubmit: (title: string, body: string) => Promise<void>
  onCancel: () => void
}) {
  const [title, setTitle] = useState(initialTitle)
  const [body, setBody] = useState(initialBody)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      await onSubmit(title.trim(), body)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '儲存失敗')
    } finally {
      setBusy(false)
    }
  }

  return (
    <form className="form-card nested-form" onSubmit={handleSubmit}>
      <div className="form-grid">
        <label className="span-2">
          標題
          <input value={title} onChange={(e) => setTitle(e.target.value)} />
        </label>
        <label className="span-2">
          內容
          <textarea rows={5} value={body} onChange={(e) => setBody(e.target.value)} />
        </label>
      </div>
      {error ? <p className="error">{error}</p> : null}
      <div className="row-actions">
        <button type="button" className="btn btn-ghost" onClick={onCancel} disabled={busy}>
          取消
        </button>
        <button type="submit" className="btn btn-primary" disabled={busy}>
          {busy ? '儲存中…' : submitLabel}
        </button>
      </div>
    </form>
  )
}
