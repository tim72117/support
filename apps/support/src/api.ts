// Client for the ai-support backend's anonymous /public/* API (no cookies, no
// credentials). Error bodies look like {"error":{"code","message"}}.

export const API_BASE: string = (import.meta.env.VITE_API_BASE ?? 'http://localhost:8082').replace(/\/+$/, '')

export interface PublicBusiness {
  slug: string
  name: string
  /** The owner's look settings; optional so an older backend still works. */
  tagline?: string
  mascot?: string
  themeColor?: string
  maxMessageLength: number
  chat: {
    available: boolean
    /** onagent WebSocket endpoint for @onagent/bridge. */
    wsUrl?: string
    appId?: string
    /** onagent's browser-side API key for this business's app (public by design). */
    apiKey?: string
  }
}

export interface ChatResult {
  conversationId: string
  messageId: number
  content: string
}

export class ApiError extends Error {
  readonly status: number
  readonly code: string
  constructor(status: number, code: string, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  let res: Response
  try {
    res = await fetch(`${API_BASE}${path}`, {
      ...init,
      credentials: 'omit',
      headers: init?.body ? { 'Content-Type': 'application/json' } : undefined,
    })
  } catch {
    throw new ApiError(0, 'network_error', '無法連線到伺服器')
  }
  if (!res.ok) {
    let code = 'unknown'
    let message = `HTTP ${res.status}`
    try {
      const body = await res.json()
      code = body?.error?.code ?? code
      message = body?.error?.message ?? message
    } catch {
      // non-JSON error body; keep the defaults
    }
    throw new ApiError(res.status, code, message)
  }
  return (await res.json()) as T
}

export function fetchBusiness(slug: string): Promise<PublicBusiness> {
  return request(`/public/businesses/${encodeURIComponent(slug)}`)
}

/** Step 1 of a turn: the backend records (and quota-checks) the message. */
export function postChat(slug: string, body: { conversationId?: string; content: string }): Promise<ChatResult> {
  return request(`/public/businesses/${encodeURIComponent(slug)}/chat`, {
    method: 'POST',
    body: JSON.stringify(body),
  })
}

/** Step 4 of a turn: report onagent's reply so the backend can store it. */
export function postReply(
  slug: string,
  body: { conversationId: string; messageId: number; content: string },
): Promise<{ messageId: number }> {
  return request(`/public/businesses/${encodeURIComponent(slug)}/chat/reply`, {
    method: 'POST',
    body: JSON.stringify(body),
  })
}
