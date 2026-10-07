import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { api } from './api.ts'
import type { OwnerBusiness, OwnerSummary } from './model.ts'

// Everything this app shows comes from the backend: the session (/auth/*,
// shared with apps/console — same cookie, same login), whether this
// session is an admin (/admin/api/me), and, once admin, the owner/business
// listings (/admin/api/users, /admin/api/businesses). Nothing is kept as
// fake data.

interface Session {
  email: string
}

export type AdminState = 'checking' | 'admin' | 'not-admin'

interface ApiUser {
  ID: number
  Email: string
}

interface ApiMe {
  isAdmin: boolean
  email?: string
}

function toSession(u: ApiUser): Session {
  return { email: u.Email }
}

interface Backend {
  session: Session | null
  // true until the first /auth/me answer is in, so a logged-in admin
  // doesn't flash the login screen on every refresh.
  loading: boolean
  login: (email: string, password: string) => Promise<void>
  logout: () => Promise<void>

  adminState: AdminState
  owners: OwnerSummary[]
  businesses: OwnerBusiness[]
  listError: string
  reload: () => Promise<void>
}

const BackendCtx = createContext<Backend | null>(null)

export function BackendProvider({ children }: { children: ReactNode }) {
  const [session, setSession] = useState<Session | null>(null)
  const [loading, setLoading] = useState(true)
  const [adminState, setAdminState] = useState<AdminState>('checking')
  const [owners, setOwners] = useState<OwnerSummary[]>([])
  const [businesses, setBusinesses] = useState<OwnerBusiness[]>([])
  const [listError, setListError] = useState('')

  useEffect(() => {
    let cancelled = false
    api<ApiUser>('/auth/me')
      .then((u) => !cancelled && setSession(toSession(u)))
      .catch(() => {}) // 401 / unreachable backend = simply not logged in
      .finally(() => !cancelled && setLoading(false))
    return () => {
      cancelled = true
    }
  }, [])

  const reload = useCallback(async () => {
    try {
      const [ownerList, businessList] = await Promise.all([
        api<OwnerSummary[] | null>('/admin/api/users'),
        api<OwnerBusiness[] | null>('/admin/api/businesses'),
      ])
      setOwners(ownerList ?? [])
      setBusinesses(businessList ?? [])
      setListError('')
    } catch (err) {
      setListError(err instanceof Error ? err.message : '無法載入資料')
    }
  }, [])

  // Once logged in, first find out whether this account is an admin at all
  // before ever calling the list endpoints — a non-admin must see "you
  // don't have admin access", never a failed-to-load list.
  const loggedIn = session !== null
  useEffect(() => {
    if (!loggedIn) {
      setAdminState('checking')
      setOwners([])
      setBusinesses([])
      setListError('')
      return
    }
    let cancelled = false
    setAdminState('checking')
    api<ApiMe>('/admin/api/me')
      .then((me) => {
        if (cancelled) return
        setAdminState(me.isAdmin ? 'admin' : 'not-admin')
      })
      .catch(() => !cancelled && setAdminState('not-admin'))
    return () => {
      cancelled = true
    }
  }, [loggedIn])

  useEffect(() => {
    if (adminState === 'admin') {
      void reload()
    }
  }, [adminState, reload])

  const value = useMemo<Backend>(
    () => ({
      session,
      loading,
      login: async (email, password) => {
        setSession(toSession(await api<ApiUser>('/auth/login', { body: { email, password } })))
      },
      logout: async () => {
        await api('/auth/logout', { method: 'POST' }).catch(() => {})
        setSession(null)
      },
      adminState,
      owners,
      businesses,
      listError,
      reload,
    }),
    [session, loading, adminState, owners, businesses, listError, reload],
  )

  return <BackendCtx.Provider value={value}>{children}</BackendCtx.Provider>
}

export function useBackend(): Backend {
  const ctx = useContext(BackendCtx)
  if (!ctx) throw new Error('useBackend must be used within BackendProvider')
  return ctx
}
