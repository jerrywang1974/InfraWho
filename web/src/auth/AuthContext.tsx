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
  /** Resolves only after the server clears the session cookie. */
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
      let status: SetupStatus
      try {
        status = await api.getSetupStatus()
        setSetup(status)
      } catch {
        // Keep prior setup/user on transient failures (do not wipe a valid session).
        return
      }
      if (status.needs_bootstrap) {
        setUser(null)
        return
      }
      try {
        const u = await api.me()
        setUser(u)
      } catch (err) {
        // Only treat 401 as signed-out; keep user on 5xx/network blips.
        if (err instanceof ApiError && err.status === 401) {
          setUser(null)
        }
      }
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void refresh()
  }, [refresh])

  const logout = useCallback(async () => {
    await api.logout()
    setUser(null)
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
