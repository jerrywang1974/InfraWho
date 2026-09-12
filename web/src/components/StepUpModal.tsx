import { useEffect, useId, useRef, useState, type FormEvent } from 'react'
import * as api from '../api/client'
import { ApiError } from '../api/client'

type Props = {
  open: boolean
  title?: string
  hint?: string
  onSuccess: () => void
  onCancel: () => void
}

export function StepUpModal({
  open,
  title = '二次確認',
  hint = '揭示密碼、破壞性操作前需重新輸入登入密碼（有效約 5 分鐘）。',
  onSuccess,
  onCancel,
}: Props) {
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)
  const titleId = useId()

  useEffect(() => {
    if (!open) return
    const t = window.setTimeout(() => inputRef.current?.focus(), 0)
    return () => window.clearTimeout(t)
  }, [open])

  if (!open) return null

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      await api.stepUp(password)
      setPassword('')
      onSuccess()
    } catch (err) {
      if (err instanceof ApiError) {
        setError(err.code === 'unauthorized' ? '密碼不正確' : err.message)
      } else {
        setError('二次確認失敗')
      }
    } finally {
      setBusy(false)
    }
  }

  function handleCancel() {
    setPassword('')
    setError(null)
    setBusy(false)
    onCancel()
  }

  return (
    <div className="modal-backdrop" role="presentation" onClick={handleCancel}>
      <div
        className="modal card"
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        onClick={(e) => e.stopPropagation()}
      >
        <h2 id={titleId}>{title}</h2>
        <p className="muted hint">{hint}</p>
        <form className="stack" onSubmit={onSubmit}>
          <label>
            登入密碼
            <input
              ref={inputRef}
              type="password"
              autoComplete="current-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
            />
          </label>
          {error ? <p className="error">{error}</p> : null}
          <div className="row-actions">
            <button type="button" className="btn btn-ghost" onClick={handleCancel} disabled={busy}>
              取消
            </button>
            <button type="submit" className="btn btn-primary" disabled={busy || !password}>
              {busy ? '確認中…' : '確認'}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}
