// Thin fetch wrapper for the ai-support backend (backend/internal/console).
// The session lives in an HttpOnly cookie set by the backend, so every call
// needs `credentials: 'include'` — and the backend's ALLOWED_ORIGIN must
// list this app's origin.

export const API_BASE: string =
  (import.meta.env.VITE_API_BASE as string | undefined)?.replace(/\/+$/, '') ?? 'http://localhost:8082'

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

export interface ApiResult<T> {
  data: T
  headers: Headers
}

export async function apiWithHeaders<T>(
  path: string,
  init: { method?: string; body?: unknown } = {},
): Promise<ApiResult<T>> {
  let res: Response
  try {
    res = await fetch(API_BASE + path, {
      method: init.method ?? (init.body === undefined ? 'GET' : 'POST'),
      credentials: 'include',
      headers: init.body === undefined ? undefined : { 'Content-Type': 'application/json' },
      body: init.body === undefined ? undefined : JSON.stringify(init.body),
    })
  } catch {
    throw new ApiError(0, '連不上伺服器，請稍後再試')
  }
  const text = await res.text()
  if (!res.ok) throw new ApiError(res.status, text.trim() || `HTTP ${res.status}`)
  return { data: (text ? JSON.parse(text) : null) as T, headers: res.headers }
}

export async function api<T>(path: string, init: { method?: string; body?: unknown } = {}): Promise<T> {
  return (await apiWithHeaders<T>(path, init)).data
}
