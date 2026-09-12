import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import * as api from '../api/client'
import { ApiError } from '../api/client'
import type { SetupStatus, User } from '../api/types'

type AuthState = {
  loading: boolean
  user: User | null
  setup: SetupStatus | null
  refresh: () => Promise<void>
  setUser: (user: User | null) => void
  setSetup: (setup: SetupStatus | null) => void
  logout: () => Promise<void>
}

const AuthContext = createContext<AuthState | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [loading, setLoading] = useState(true)
  const [user, setUser] = useState<User | null>(null)
  const [setup, setSetup] = useState<SetupStatus | null>(null)

  const refresh = useCallback(async () => {
    setLoading(true)
    try {
      const status = await api.getSetupStatus()
      setSetup(status)
      if (status.needs_bootstrap) {
        setUser(null)
        return
      }
      try {
        const u = await api.me()
        setUser(u)
      } catch (err) {
        if (err instanceof ApiError && err.status === 401) {
          setUser(null)
        } else {
          setUser(null)
        }
      }
    } catch {
      setSetup(null)
      setUser(null)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void refresh()
  }, [refresh])

  const logout = useCallback(async () => {
    try {
      await api.logout()
    } finally {
      setUser(null)
    }
  }, [])

  const value = useMemo(
    () => ({ loading, user, setup, refresh, setUser, setSetup, logout }),
    [loading, user, setup, refresh, logout],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext)
  if (!ctx) {
    throw new Error('useAuth must be used within AuthProvider')
  }
  return ctx
}
