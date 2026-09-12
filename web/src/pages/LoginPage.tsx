import { useState, type FormEvent } from 'react'
import { Navigate } from 'react-router-dom'
import * as api from '../api/client'
import { ApiError } from '../api/client'
import { useAuth } from '../auth/AuthContext'

export function LoginPage() {
  const { user, setup, loading, setUser, setSetup } = useAuth()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)

  if (!loading && setup?.needs_bootstrap) {
    return <Navigate to="/setup" replace />
  }
  if (!loading && user) {
    return <Navigate to="/" replace />
  }

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setError(null)
    setSubmitting(true)
    try {
      const u = await api.login(username.trim(), password)
      setUser(u)
      // Refresh setup banner flags without risking a wipe of the new session.
      try {
        setSetup(await api.getSetupStatus())
      } catch {
        // Session cookie is set; setup flags can wait for a later refresh.
      }
    } catch (err) {
      if (err instanceof ApiError) {
        if (err.code === 'lockout' || err.status === 429) {
          setError('嘗試次數過多，帳號暫時鎖定，請稍後再試。')
        } else if (err.status === 401) {
          setError('使用者名稱或密碼不正確。')
        } else if (err.code === 'forbidden') {
          setError('來源驗證失敗。若使用自訂 Origin，請設定 INFRAWHO_TRUSTED_ORIGINS。')
        } else {
          setError(err.message || '登入失敗')
        }
      } else {
        setError('無法連線至伺服器')
      }
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="auth-page">
      <form className="card auth-card" onSubmit={onSubmit}>
        <h1>登入 InfraWho</h1>
        <p className="muted">基礎設施盤點與憑證管理</p>

        <label>
          使用者名稱
          <input
            autoComplete="username"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            required
          />
        </label>

        <label>
          密碼
          <input
            type="password"
            autoComplete="current-password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            required
          />
        </label>

        {error ? <p className="error">{error}</p> : null}

        <button className="btn btn-primary" type="submit" disabled={submitting || loading}>
          {submitting ? '登入中…' : '登入'}
        </button>
      </form>
    </div>
  )
}
