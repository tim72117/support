import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { api, ApiError } from './api.ts'
import { trackEvent, trackSignUp } from './analytics.ts'
import {
  buildContentText,
  sectionsForBackend,
  sectionsFromBackend,
  type Business,
  type BusinessPatch,
  type ContentSection,
  type Conversation,
  type ConversationDetail,
  type LayoutId,
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

export type LoadState = 'idle' | 'loading' | 'ready' | 'error'

/** Conversations are an optional backend feature; 'disabled' is a distinct,
 * non-retryable outcome from a plain 'error' (see console.go: a nil Chats
 * store answers 503). */
export type ConversationsState = 'idle' | 'loading' | 'ready' | 'error' | 'disabled'

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
  saveContent: (id: number, sections: ContentSection[]) => Promise<void>

  loadConversations: (businessId: number) => Promise<{ state: ConversationsState; list: Conversation[]; error: string }>
  loadConversation: (businessId: number, conversationId: string) => Promise<ConversationDetail>
}

interface ApiUser {
  ID: number
  Email: string
}

// backend/internal/conversation.Conversation / Message are serialised with
// their Go json tags (camelCase, unlike the capitalised business fields
// above, which predate those tags).
interface ApiConversation {
  id: string
  businessId: number
  createdAt: string
}

interface ApiMessage {
  id: number
  conversationId: string
  role: string
  content: string
  replyTo?: number
  createdAt: string
}

interface ApiConversationDetail extends ApiConversation {
  messages: ApiMessage[]
}

function toConversation(c: ApiConversation): Conversation {
  return { id: c.id, createdAt: c.createdAt }
}

function toConversationDetail(c: ApiConversationDetail): ConversationDetail {
  return {
    id: c.id,
    createdAt: c.createdAt,
    messages: c.messages.map((m) => ({
      id: m.id,
      role: m.role === 'assistant' ? 'assistant' : 'user',
      content: m.content,
      createdAt: m.createdAt,
    })),
  }
}

// The backend serialises Go structs as-is, hence the capitalised names.
interface ApiBusiness {
  ID: number
  Slug: string
  Name: string
  Tagline: string
  Mascot: string
  ThemeColor: string
  Layout: string
}

const MASCOTS: MascotId[] = ['fox', 'bear', 'cat', 'bird', 'rabbit', 'dog', 'owl', 'penguin', 'panda', 'pig']
const LAYOUTS: LayoutId[] = ['center', 'split']

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
    layout: (LAYOUTS as string[]).includes(b.Layout) ? (b.Layout as LayoutId) : 'center',
  }
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
        trackSignUp() // only reached once the account really exists
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
        trackEvent('create_business')
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
        await api<null>(`/console/businesses/${id}/content`, {
          method: 'PUT',
          body: { content: buildContentText(sections), sections: sectionsForBackend(sections) },
        })
      },

      loadConversations: async (businessId) => {
        try {
          const list = await api<ApiConversation[] | null>(`/console/businesses/${businessId}/conversations`)
          return { state: 'ready' as const, list: (list ?? []).map(toConversation), error: '' }
        } catch (err) {
          if (err instanceof ApiError && err.status === 503) {
            return { state: 'disabled' as const, list: [], error: '' }
          }
          return { state: 'error' as const, list: [], error: err instanceof Error ? err.message : '無法載入對話紀錄' }
        }
      },
      loadConversation: async (businessId, conversationId) => {
        const detail = await api<ApiConversationDetail>(
          `/console/businesses/${businessId}/conversations/${conversationId}`,
        )
        return toConversationDetail(detail)
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
