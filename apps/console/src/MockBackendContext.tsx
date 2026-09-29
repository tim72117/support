import { createContext, useContext, useMemo, useState, type ReactNode } from 'react'
import { seedBusinesses, type Business } from './mockData.ts'

// Stand-in for the real /auth + /console API (see docs/refactor-initial-
// scaffold-plan-2026-09-27.md — this build intentionally does the UI first
// against fake data, then swaps this context's guts for real fetch calls).
// Every "session" here just lives in memory; refreshing the page logs out.

interface Session {
  email: string
  ownerName: string
}

interface MockBackend {
  session: Session | null
  login: (email: string) => void
  logout: () => void
  businesses: Business[]
  getBusiness: (id: string) => Business | undefined
  updateBusiness: (id: string, patch: Partial<Business>) => void
  createBusiness: (business: Business) => void
  deleteBusiness: (id: string) => void
}

const MockBackendCtx = createContext<MockBackend | null>(null)

export function MockBackendProvider({ children }: { children: ReactNode }) {
  const [session, setSession] = useState<Session | null>(null)
  const [businesses, setBusinesses] = useState<Business[]>(() => seedBusinesses())

  const value = useMemo<MockBackend>(
    () => ({
      session,
      login: (email: string) => {
        const namePart = email.split('@')[0] ?? '老闆'
        setSession({ email, ownerName: namePart })
      },
      logout: () => setSession(null),
      businesses,
      getBusiness: (id: string) => businesses.find((b) => b.id === id),
      updateBusiness: (id: string, patch: Partial<Business>) => {
        setBusinesses((prev) => prev.map((b) => (b.id === id ? { ...b, ...patch } : b)))
      },
      createBusiness: (business: Business) => {
        setBusinesses((prev) => [...prev, business])
      },
      deleteBusiness: (id: string) => {
        setBusinesses((prev) => prev.filter((b) => b.id !== id))
      },
    }),
    [session, businesses],
  )

  return <MockBackendCtx.Provider value={value}>{children}</MockBackendCtx.Provider>
}

export function useMockBackend(): MockBackend {
  const ctx = useContext(MockBackendCtx)
  if (!ctx) throw new Error('useMockBackend must be used within MockBackendProvider')
  return ctx
}
