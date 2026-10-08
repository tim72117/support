// A stand-in for the ai-support backend, for tests only. It sits at the
// network boundary (a replacement for `fetch`) and speaks the real API
// contract of backend/internal/console — same paths, same status codes, same
// capitalised JSON field names — so the console code under test is
// exercised exactly as it will run against the real thing.
//
// There is no per-business "connected to onagent" status or sync endpoint
// to fake anymore: every business's consumer page shares one fixed onagent
// app configured once on the real backend (ONAGENT_APP_ID/ONAGENT_APP_KEY),
// so the console never provisions or syncs anything per business.

interface Biz {
  ID: number
  OwnerID: number
  Slug: string
  Name: string
  Tagline: string
  Mascot: string
  ThemeColor: string
  Layout: string
}

interface FakeMessage {
  id: number
  conversationId: string
  role: string
  content: string
  replyTo?: number
  createdAt: string
}

interface FakeConversation {
  id: string
  businessId: number
  createdAt: string
}

export interface Call {
  method: string
  path: string
  body: unknown
}

export interface FakeBackend {
  fetch: (url: string, init?: RequestInit) => Promise<Response>
  calls: Call[]
  businesses: Biz[]
  contents: Map<number, { Content: string; Sections: unknown }>
  /** Make requests whose "METHOD path" matches fail with this status. */
  failNext: (key: string, status: number, message?: string) => void
  /** Fail every request as if the network were down. */
  offline: boolean
  callsTo: (method: string, pathPattern: RegExp) => Call[]
  conversations: FakeConversation[]
  messages: FakeMessage[]
  /** null = conversations feature not configured on this server (503), the
   * same shape as a nil Chats store in backend/internal/console. */
  conversationsEnabled: boolean
}

const MASCOTS = ['fox', 'bear', 'cat', 'bird', 'rabbit', 'dog', 'owl', 'penguin', 'panda', 'pig']
const LAYOUTS = ['center', 'split']

function res(status: number, body: unknown, headers: Record<string, string> = {}) {
  const text = body === null || body === undefined ? '' : typeof body === 'string' ? body : JSON.stringify(body)
  return new Response(status === 204 ? null : text, { status, headers })
}

export function createFakeBackend(opts: { email?: string | null; businesses?: Partial<Biz>[] } = {}): FakeBackend {
  const email = opts.email === undefined ? 'owner@example.com' : opts.email
  let loggedInAs: string | null = email
  let nextId = 100
  const failures = new Map<string, { status: number; message: string }>()

  const fb: FakeBackend = {
    calls: [],
    businesses: [],
    contents: new Map(),
    offline: false,
    conversations: [],
    messages: [],
    conversationsEnabled: true,
    failNext(key, status, message = 'forced failure') {
      failures.set(key, { status, message })
    },
    callsTo(method, pattern) {
      return fb.calls.filter((c) => c.method === method && pattern.test(c.path))
    },
    async fetch(url, init) {
      const method = (init?.method ?? 'GET').toUpperCase()
      const path = new URL(url, 'http://backend.test').pathname
      const body = init?.body ? JSON.parse(String(init.body)) : undefined
      fb.calls.push({ method, path, body })
      if (fb.offline) throw new TypeError('network down')

      const key = `${method} ${path}`
      const forced = failures.get(key)
      if (forced) {
        failures.delete(key)
        return res(forced.status, forced.message)
      }

      // --- auth ---
      if (path === '/auth/me') return loggedInAs ? res(200, { ID: 1, Email: loggedInAs }) : res(401, 'not authenticated')
      if (path === '/auth/login' && method === 'POST') {
        loggedInAs = body.email
        return res(200, { ID: 1, Email: body.email })
      }
      if (path === '/auth/register' && method === 'POST') {
        loggedInAs = body.email
        return res(200, { ID: 2, Email: body.email })
      }
      if (path === '/auth/logout' && method === 'POST') {
        loggedInAs = null
        return res(204, null)
      }
      if (!loggedInAs) return res(401, 'not authenticated')

      // --- businesses ---
      if (path === '/console/businesses' && method === 'GET') return res(200, fb.businesses.length ? fb.businesses : null)
      if (path === '/console/businesses' && method === 'POST') {
        const slug: string = body.slug ?? ''
        if (!/^[a-z0-9][a-z0-9-]*$/.test(slug) || slug.length > 40) return res(400, 'invalid URL')
        const name = String(body.name ?? '').trim()
        if (!name) return res(400, 'name is required')
        if (fb.businesses.some((b) => b.Slug === slug)) return res(409, 'this URL is already taken')
        const biz: Biz = {
          ID: nextId++,
          OwnerID: 1,
          Slug: slug,
          Name: name,
          Tagline: String(body.tagline ?? '').trim(),
          Mascot: body.mascot || 'fox',
          ThemeColor: body.themeColor || '#FF8A5B',
          Layout: body.layout || 'center',
        }
        fb.businesses.push(biz)
        fb.contents.set(biz.ID, { Content: '', Sections: null })
        return res(200, biz)
      }

      // --- conversations ---
      const convMatch = path.match(/^\/console\/businesses\/(\d+)\/conversations(?:\/([^/]+))?$/)
      if (convMatch) {
        const bizId = Number(convMatch[1])
        const convBiz = fb.businesses.find((b) => b.ID === bizId)
        if (!convBiz) return res(404, '404 page not found')
        if (!fb.conversationsEnabled) return res(503, 'conversations are unavailable')
        const cid = convMatch[2]
        if (!cid && method === 'GET') {
          const list = fb.conversations.filter((c) => c.businessId === bizId)
          return res(200, list.length ? list : null)
        }
        if (cid && method === 'GET') {
          const conv = fb.conversations.find((c) => c.id === cid && c.businessId === bizId)
          if (!conv) return res(404, '404 page not found')
          const msgs = fb.messages.filter((m2) => m2.conversationId === cid)
          return res(200, { ...conv, messages: msgs })
        }
        return res(404, '404 page not found')
      }

      const m = path.match(/^\/console\/businesses\/(\d+)(\/content)?$/)
      if (!m) return res(404, '404 page not found')
      const id = Number(m[1])
      const biz = fb.businesses.find((b) => b.ID === id)
      if (!biz) return res(404, '404 page not found')
      const sub = m[2]

      if (!sub && method === 'GET') return res(200, biz)
      if (!sub && method === 'PATCH') {
        const next = { ...biz }
        if (body.name !== undefined) next.Name = String(body.name).trim()
        if (body.tagline !== undefined) next.Tagline = String(body.tagline).trim()
        if (body.mascot !== undefined) next.Mascot = body.mascot
        if (body.themeColor !== undefined) next.ThemeColor = body.themeColor
        if (body.layout !== undefined) next.Layout = body.layout
        if (!next.Name) return res(400, 'name is required')
        if (!MASCOTS.includes(next.Mascot)) return res(400, 'invalid branding: unknown mascot')
        if (!/^#[0-9a-fA-F]{6}$/.test(next.ThemeColor)) return res(400, 'invalid branding: theme color must look like #RRGGBB')
        if (!LAYOUTS.includes(next.Layout)) return res(400, 'invalid branding: unknown layout')
        Object.assign(biz, next)
        return res(200, biz)
      }
      if (!sub && method === 'DELETE') {
        fb.businesses = fb.businesses.filter((b) => b.ID !== id)
        fb.contents.delete(id)
        return res(204, null)
      }
      if (sub === '/content' && method === 'GET') return res(200, fb.contents.get(id) ?? { Content: '', Sections: null })
      if (sub === '/content' && method === 'PUT') {
        fb.contents.set(id, { Content: body.content, Sections: body.sections ?? null })
        return res(204, null)
      }
      return res(404, '404 page not found')
    },
  }

  for (const b of opts.businesses ?? []) {
    fb.businesses.push({
      ID: nextId++,
      OwnerID: 1,
      Slug: `shop-${nextId}`,
      Name: '店',
      Tagline: '',
      Mascot: 'fox',
      ThemeColor: '#FF8A5B',
      Layout: 'center',
      ...b,
    })
    fb.contents.set(fb.businesses[fb.businesses.length - 1].ID, { Content: '', Sections: null })
  }
  return fb
}
