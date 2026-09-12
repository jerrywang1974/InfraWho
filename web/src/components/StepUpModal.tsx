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
  const dialogRef = useRef<HTMLDivElement>(null)
  const previouslyFocused = useRef<HTMLElement | null>(null)
  const cancelledRef = useRef(false)
  const titleId = useId()

  useEffect(() => {
    if (open) {
      cancelledRef.current = false
      setPassword('')
      setError(null)
      setBusy(false)
      previouslyFocused.current = document.activeElement as HTMLElement | null
      const t = window.setTimeout(() => inputRef.current?.focus(), 0)
      return () => window.clearTimeout(t)
    }
    setPassword('')
    setError(null)
    setBusy(false)
    previouslyFocused.current?.focus?.()
    previouslyFocused.current = null
    return undefined
  }, [open])

  useEffect(() => {
    if (!open) return
    function onKeyDown(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        e.preventDefault()
        // Mark cancelled even while in-flight so a late stepUp resolve is ignored.
        if (!cancelledRef.current) {
          cancelledRef.current = true
          setPassword('')
          setError(null)
          setBusy(false)
          onCancel()
        }
        return
      }
      if (e.key !== 'Tab' || !dialogRef.current) return
      const focusable = dialogRef.current.querySelectorAll<HTMLElement>(
        'button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [href], [tabindex]:not([tabindex="-1"])',
      )
      if (focusable.length === 0) return
      const first = focusable[0]
      const last = focusable[focusable.length - 1]
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault()
        last.focus()
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault()
        first.focus()
      }
    }
    document.addEventListener('keydown', onKeyDown)
    return () => document.removeEventListener('keydown', onKeyDown)
  }, [open, onCancel])

  if (!open) return null

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    cancelledRef.current = false
    setBusy(true)
    setError(null)
    try {
      await api.stepUp(password)
      if (cancelledRef.current) return
      setPassword('')
      onSuccess()
    } catch (err) {
      if (cancelledRef.current) return
      if (err instanceof ApiError) {
        setError(err.code === 'unauthorized' ? '密碼不正確' : err.message)
      } else {
        setError('二次確認失敗')
      }
    } finally {
      if (!cancelledRef.current) setBusy(false)
    }
  }

  function handleCancel() {
    // Allow cancel during in-flight step-up; cancelledRef drops a late onSuccess.
    if (cancelledRef.current) return
    cancelledRef.current = true
    setPassword('')
    setError(null)
    setBusy(false)
    onCancel()
  }

  return (
    <div className="modal-backdrop" role="presentation" onClick={handleCancel}>
      <div
        ref={dialogRef}
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
              disabled={busy}
            />
          </label>
          {error ? <p className="error">{error}</p> : null}
          <div className="row-actions">
            <button type="button" className="btn btn-ghost" onClick={handleCancel}>
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
