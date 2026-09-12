import type { ReactNode } from 'react'
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { AuthProvider, useAuth } from './auth/AuthContext'
import { Layout } from './components/Layout'
import { AssetDetailPage } from './pages/AssetDetailPage'
import { AssetListPage } from './pages/AssetListPage'
import { LoginPage } from './pages/LoginPage'
import { SetupPage } from './pages/SetupPage'

function RequireAuth({ children }: { children: ReactNode }) {
  const { user, setup, loading } = useAuth()
  if (loading) {
    return (
      <div className="auth-page">
        <p className="muted">載入中…</p>
      </div>
    )
  }
  if (setup?.needs_bootstrap) {
    return <Navigate to="/setup" replace />
  }
  if (!user) {
    return <Navigate to="/login" replace />
  }
  return children
}

function AppRoutes() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route path="/setup" element={<SetupPage />} />
      <Route
        element={
          <RequireAuth>
            <Layout />
          </RequireAuth>
        }
      >
        <Route path="/" element={<AssetListPage />} />
        <Route path="/assets/:id" element={<AssetDetailPage />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}

export default function App() {
  return (
    <BrowserRouter>
      <AuthProvider>
        <AppRoutes />
      </AuthProvider>
    </BrowserRouter>
  )
}
