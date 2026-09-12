import { useState } from 'react'
import { Link, Outlet } from 'react-router-dom'
import { useAuth } from '../auth/AuthContext'
import { formatRole } from '../lib/format'

export function Layout() {
  const { user, setup, logout } = useAuth()
  const [logoutError, setLogoutError] = useState<string | null>(null)
  const [loggingOut, setLoggingOut] = useState(false)

  async function onLogout() {
    setLogoutError(null)
    setLoggingOut(true)
    try {
      await logout()
    } catch {
      setLogoutError('登出失敗，工作階段可能仍有效。請重試。')
    } finally {
      setLoggingOut(false)
    }
  }

  return (
    <div className="app-shell">
      <header className="topbar">
        <div className="topbar-brand">
          <Link to="/">InfraWho</Link>
        </div>
        <nav className="topbar-nav">
          <Link to="/">資產</Link>
        </nav>
        <div className="topbar-user">
          {user ? (
            <>
              <span className="muted">
                {user.display_name || user.username}
                <span className="role-pill">{formatRole(user.role)}</span>
              </span>
              <button
                type="button"
                className="btn btn-ghost"
                disabled={loggingOut}
                onClick={() => void onLogout()}
              >
                {loggingOut ? '登出中…' : '登出'}
              </button>
            </>
          ) : null}
        </div>
      </header>

      {logoutError ? (
        <div className="banner warn" role="alert">
          {logoutError}
        </div>
      ) : null}

      {setup?.show_banner ? (
        <div className="banner warn" role="status">
          安裝檢查清單尚未完成。請確認主金鑰離線備份與還原計畫後，於設定精靈完成勾選。
          {!setup.checklist_complete ? (
            <Link className="banner-link" to="/setup">
              前往完成
            </Link>
          ) : null}
        </div>
      ) : null}

      <main className="main">
        <Outlet />
      </main>
    </div>
  )
}
