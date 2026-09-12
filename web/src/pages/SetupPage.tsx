import { useState, type FormEvent } from 'react'
import { Navigate } from 'react-router-dom'
import * as api from '../api/client'
import { ApiError } from '../api/client'
import { useAuth } from '../auth/AuthContext'

export function SetupPage() {
  const { user, setup, loading, setUser, refresh } = useAuth()
  const [username, setUsername] = useState('admin')
  const [displayName, setDisplayName] = useState('')
  const [password, setPassword] = useState('')
  const [password2, setPassword2] = useState('')
  const [ackOffline, setAckOffline] = useState(false)
  const [ackIrrecoverable, setAckIrrecoverable] = useState(false)
  const [ackBackup, setAckBackup] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)

  const needsBootstrap = !!setup?.needs_bootstrap
  const needsAckOnly =
    !needsBootstrap && !!user && user.role === 'admin' && setup && !setup.checklist_complete

  if (!loading && !needsBootstrap && !needsAckOnly) {
    if (user) return <Navigate to="/" replace />
    return <Navigate to="/login" replace />
  }

  async function onBootstrap(e: FormEvent) {
    e.preventDefault()
    setError(null)
    if (password.length < 8) {
      setError('密碼至少 8 個字元')
      return
    }
    if (password !== password2) {
      setError('兩次輸入的密碼不一致')
      return
    }
    if (!ackOffline || !ackIrrecoverable || !ackBackup) {
      setError('請勾選全部安裝檢查項目')
      return
    }
    if (!setup?.master_key_ready) {
      setError('主金鑰尚未就緒。請先設定 INFRAWHO_MASTER_KEY_FILE 後再繼續。')
      return
    }
    setSubmitting(true)
    try {
      const u = await api.bootstrap({
        username: username.trim(),
        password,
        display_name: displayName.trim() || undefined,
        acknowledge_kek_offline: ackOffline,
        acknowledge_kek_irrecoverable: ackIrrecoverable,
        acknowledge_backup_planned: ackBackup,
      })
      setUser(u)
      await refresh()
    } catch (err) {
      if (err instanceof ApiError) {
        if (err.code === 'not_ready') {
          setError('主金鑰尚未就緒，無法完成安裝。')
        } else if (err.code === 'conflict') {
          setError('系統已完成初始化，請改為登入。')
        } else {
          setError(err.message || '安裝失敗')
        }
      } else {
        setError('無法連線至伺服器')
      }
    } finally {
      setSubmitting(false)
    }
  }

  async function onAcknowledge(e: FormEvent) {
    e.preventDefault()
    setError(null)
    if (!ackOffline || !ackIrrecoverable || !ackBackup) {
      setError('請勾選全部安裝檢查項目')
      return
    }
    setSubmitting(true)
    try {
      await api.acknowledgeChecklist({
        acknowledge_kek_offline: ackOffline,
        acknowledge_kek_irrecoverable: ackIrrecoverable,
        acknowledge_backup_planned: ackBackup,
      })
      await refresh()
    } catch (err) {
      if (err instanceof ApiError) {
        setError(err.message || '無法儲存檢查清單')
      } else {
        setError('無法連線至伺服器')
      }
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="auth-page">
      <form
        className="card auth-card wide"
        onSubmit={needsBootstrap ? onBootstrap : onAcknowledge}
      >
        <h1>{needsBootstrap ? '首次安裝精靈' : '完成安裝檢查清單'}</h1>
        <p className="muted">
          主金鑰遺失將導致密文永久無法解密。請先離線保存 KEK，並規劃資料庫備份路徑。
        </p>

        <div className={`status-chip ${setup?.master_key_ready ? 'ok' : 'bad'}`}>
          {setup?.master_key_ready ? '主金鑰已就緒' : '主金鑰尚未就緒'}
        </div>

        {needsBootstrap ? (
          <>
            <label>
              管理員使用者名稱
              <input
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                autoComplete="username"
                required
              />
            </label>
            <label>
              顯示名稱（選填）
              <input value={displayName} onChange={(e) => setDisplayName(e.target.value)} />
            </label>
            <label>
              密碼
              <input
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                autoComplete="new-password"
                required
                minLength={8}
              />
            </label>
            <label>
              確認密碼
              <input
                type="password"
                value={password2}
                onChange={(e) => setPassword2(e.target.value)}
                autoComplete="new-password"
                required
                minLength={8}
              />
            </label>
          </>
        ) : null}

        <fieldset className="checklist">
          <legend>必要確認</legend>
          <label className="check">
            <input
              type="checkbox"
              checked={ackOffline}
              onChange={(e) => setAckOffline(e.target.checked)}
            />
            我已將主金鑰（KEK）離線保存於安全處
          </label>
          <label className="check">
            <input
              type="checkbox"
              checked={ackIrrecoverable}
              onChange={(e) => setAckIrrecoverable(e.target.checked)}
            />
            我了解遺失主金鑰後密文無法恢復
          </label>
          <label className="check">
            <input
              type="checkbox"
              checked={ackBackup}
              onChange={(e) => setAckBackup(e.target.checked)}
            />
            我已規劃資料庫備份路徑（備份不含主金鑰）
          </label>
        </fieldset>

        {error ? <p className="error">{error}</p> : null}

        <button
          className="btn btn-primary"
          type="submit"
          disabled={submitting || loading || !setup?.master_key_ready}
        >
          {submitting ? '處理中…' : needsBootstrap ? '建立管理員並完成安裝' : '儲存檢查清單'}
        </button>
      </form>
    </div>
  )
}
