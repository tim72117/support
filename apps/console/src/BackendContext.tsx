import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { api, apiWithHeaders, ApiError } from './api.ts'
import {
  buildContentText,
  sectionsForBackend,
  sectionsFromBackend,
  type Business,
  type BusinessPatch,
  type ContentSection,
  type MascotId,
  type NewBusinessInput,
} from './model.ts'

// Everything the console shows comes from the backend: the session
// (/auth/*), the owner's businesses and their content (/console/businesses).
// Nothing is kept as fake data; a page refresh re-reads it all.

interface Session {
  email: string
  ownerName: string
}

/** Result of pushing a business's content to the AI service (onagent). */
export type SyncStatus = 'ok' | 'failed' | 'disabled' | 'unknown'

export type LoadState = 'idle' | 'loading' | 'ready' | 'error'

interface Backend {
  session: Session | null
  // true until the first /auth/me answer is in, so a logged-in user doesn't
  // flash the login screen on every refresh.
  loading: boolean
  login: (email: string, password: string) => Promise<void>
  register: (email: string, password: string) => Promise<void>
  logout: () => Promise<void>

  businesses: Business[]
  businessesState: LoadState
  businessesError: string
  reloadBusinesses: () => Promise<void>
  createBusiness: (input: NewBusinessInput) => Promise<Business>
  updateBusiness: (id: number, patch: BusinessPatch) => Promise<Business>
  deleteBusiness: (id: number) => Promise<void>
  loadContent: (id: number) => Promise<ContentSection[]>
  saveContent: (id: number, sections: ContentSection[]) => Promise<SyncStatus>
  syncOnagent: (id: number) => Promise<SyncStatus>
}

interface ApiUser {
  ID: number
  Email: string
}

// The backend serialises Go structs as-is, hence the capitalised names.
interface ApiBusiness {
  ID: number
  Slug: string
  Name: string
  Tagline: string
  Mascot: string
  ThemeColor: string
  Connected: boolean
  onagentSync?: string
}

const MASCOTS: MascotId[] = ['fox', 'bear', 'cat', 'bird']

function toSession(u: ApiUser): Session {
  return { email: u.Email, ownerName: u.Email.split('@')[0] ?? '老闆' }
}

function toBusiness(b: ApiBusiness): Business {
  return {
    id: b.ID,
    slug: b.Slug,
    name: b.Name,
    tagline: b.Tagline ?? '',
    mascot: (MASCOTS as string[]).includes(b.Mascot) ? (b.Mascot as MascotId) : 'fox',
    themeColor: b.ThemeColor || '#FF8A5B',
    connected: !!b.Connected,
  }
}

function toSyncStatus(v: string | null | undefined): SyncStatus {
  return v === 'ok' || v === 'failed' || v === 'disabled' ? v : 'unknown'
}

const BackendCtx = createContext<Backend | null>(null)

export function BackendProvider({ children }: { children: ReactNode }) {
  const [session, setSession] = useState<Session | null>(null)
  const [loading, setLoading] = useState(true)
  const [businesses, setBusinesses] = useState<Business[]>([])
  const [businessesState, setBusinessesState] = useState<LoadState>('idle')
  const [businessesError, setBusinessesError] = useState('')

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

  const reloadBusinesses = useCallback(async () => {
    setBusinessesState('loading')
    try {
      const list = await api<ApiBusiness[] | null>('/console/businesses')
      setBusinesses((list ?? []).map(toBusiness))
      setBusinessesError('')
      setBusinessesState('ready')
    } catch (err) {
      setBusinessesError(err instanceof Error ? err.message : '無法載入服務列表')
      setBusinessesState('error')
    }
  }, [])

  // Load the owner's businesses whenever someone is logged in; forget them
  // the moment nobody is, so one owner never sees another's list.
  const loggedIn = session !== null
  useEffect(() => {
    if (loggedIn) {
      void reloadBusinesses()
    } else {
      setBusinesses([])
      setBusinessesState('idle')
      setBusinessesError('')
    }
  }, [loggedIn, reloadBusinesses])

  const value = useMemo<Backend>(
    () => ({
      session,
      loading,
      login: async (email, password) => {
        setSession(toSession(await api<ApiUser>('/auth/login', { body: { email, password } })))
      },
      register: async (email, password) => {
        setSession(toSession(await api<ApiUser>('/auth/register', { body: { email, password } })))
      },
      logout: async () => {
        // Clear locally even if the request fails: the user asked to leave.
        await api('/auth/logout', { method: 'POST' }).catch(() => {})
        setSession(null)
      },

      businesses,
      businessesState,
      businessesError,
      reloadBusinesses,

      createBusiness: async (input) => {
        const created = toBusiness(await api<ApiBusiness>('/console/businesses', { body: input }))
        setBusinesses((prev) => [...prev, created])
        return created
      },
      updateBusiness: async (id, patch) => {
        const updated = toBusiness(await api<ApiBusiness>(`/console/businesses/${id}`, { method: 'PATCH', body: patch }))
        setBusinesses((prev) => prev.map((b) => (b.id === id ? updated : b)))
        return updated
      },
      deleteBusiness: async (id) => {
        await api(`/console/businesses/${id}`, { method: 'DELETE' })
        setBusinesses((prev) => prev.filter((b) => b.id !== id))
      },
      loadContent: async (id) => {
        const res = await api<{ Content: string; Sections: unknown }>(`/console/businesses/${id}/content`)
        return sectionsFromBackend(res.Sections, res.Content ?? '')
      },
      saveContent: async (id, sections) => {
        const { headers } = await apiWithHeaders<null>(`/console/businesses/${id}/content`, {
          method: 'PUT',
          body: { content: buildContentText(sections), sections: sectionsForBackend(sections) },
        })
        // Saving can create the AI app for the first time; reflect that.
        const status = toSyncStatus(headers.get('X-Onagent-Sync'))
        if (status === 'ok') setBusinesses((prev) => prev.map((b) => (b.id === id ? { ...b, connected: true } : b)))
        return status
      },
      syncOnagent: async (id) => {
        // 200 = synced, 503 = the AI service is not configured on this
        // server, anything else (502, network) = failed, try again later.
        try {
          await api(`/console/businesses/${id}/onagent-sync`, { method: 'POST' })
        } catch (err) {
          return err instanceof ApiError && err.status === 503 ? 'disabled' : 'failed'
        }
        setBusinesses((prev) => prev.map((b) => (b.id === id ? { ...b, connected: true } : b)))
        return 'ok'
      },
    }),
    [session, loading, businesses, businessesState, businessesError, reloadBusinesses],
  )

  return <BackendCtx.Provider value={value}>{children}</BackendCtx.Provider>
}

export function useBackend(): Backend {
  const ctx = useContext(BackendCtx)
  if (!ctx) throw new Error('useBackend must be used within BackendProvider')
  return ctx
}
