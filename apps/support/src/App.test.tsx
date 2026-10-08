import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'

// A stand-in for @onagent/bridge that records construction and prompts, so the
// tests can assert *when* a connection would be opened and with what text.
interface BridgeOpts {
  url: string
  appId: string
  apiKey?: string
  lazyConnect?: boolean
  tools?: Array<{ name: string; handle: (args: unknown) => unknown }>
  onAssistantMessage?: (text: string) => void
  onError?: (err: { message: string; code?: string }) => void
  onQuotaExceeded?: (err: { message: string }) => void
}
const { bridges, FakeBridge } = vi.hoisted(() => {
  const bridges: InstanceType<typeof FakeBridge>[] = []
  class FakeBridge {
    prompts: string[] = []
    closed = false
    opts: BridgeOpts
    constructor(opts: BridgeOpts) {
      this.opts = opts
      bridges.push(this)
    }
    prompt(text: string) {
      this.prompts.push(text)
    }
    close() {
      this.closed = true
    }
  }
  return { bridges, FakeBridge }
})
// defineTool itself is real production logic worth exercising for real
// (it parses/validates tool args), so re-implement just enough of it here
// rather than mocking it away — only AgentBridge's networking is faked.
vi.mock('@onagent/bridge', () => ({
  AgentBridge: FakeBridge,
  defineTool: (name: string, parseArgs: (raw: unknown) => unknown, handle: (args: unknown) => unknown) => ({
    name,
    handle: (raw: unknown) => handle(parseArgs(raw)),
  }),
}))

import { App } from './App.tsx'

const BUSINESS = {
  slug: 'shop',
  name: '晨光烘焙坊',
  maxMessageLength: 500,
  chat: { available: true, wsUrl: 'wss://onagent.test/ws', appId: 'app-1', apiKey: 'browser-key' },
}

type Handler = (url: string, init?: RequestInit) => Response | Promise<Response>
let handler: Handler
const fetchMock = vi.fn((url: string, init?: RequestInit) => Promise.resolve(handler(url, init)))

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } })
}
function apiError(status: number, code: string): Response {
  return json({ error: { code, message: code } }, status)
}
function calls(method: string, suffix: string) {
  return fetchMock.mock.calls.filter(
    ([url, init]) => String(url).endsWith(suffix) && ((init as RequestInit | undefined)?.method ?? 'GET') === method,
  )
}

beforeEach(() => {
  bridges.length = 0
  fetchMock.mockClear()
  vi.stubGlobal('fetch', fetchMock)
  window.history.pushState({}, '', '/support/shop')
  handler = (url) => {
    if (url.endsWith('/public/businesses/shop')) return json(BUSINESS)
    return apiError(404, 'not_found')
  }
})
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

async function openChat() {
  const user = userEvent.setup()
  render(<App />)
  await user.click(await screen.findByRole('button', { name: '開始對話' }))
  return user
}

describe('consumer chat page', () => {
  it('shows a not-found message for an unknown slug', async () => {
    window.history.pushState({}, '', '/support/nope')
    handler = () => apiError(404, 'not_found')
    render(<App />)
    expect(await screen.findByText(/找不到這個服務頁面/)).toBeInTheDocument()
    expect(bridges).toHaveLength(0)
  })

  it('shows not-found when the URL has no slug, without calling the backend', () => {
    window.history.pushState({}, '', '/')
    render(<App />)
    expect(screen.getByText(/找不到這個服務頁面/)).toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('shows a load error (not "not found") when the backend is unreachable', async () => {
    handler = () => {
      throw new TypeError('network')
    }
    render(<App />)
    expect(await screen.findByText(/目前無法載入/)).toBeInTheDocument()
  })

  it('opens no onagent connection and sends no message before the visitor sends something', async () => {
    const user = await openChat()
    expect(screen.getByText(/晨光烘焙坊 的 AI 小幫手/)).toBeInTheDocument() // greeting shown
    expect(bridges).toHaveLength(0)
    expect(calls('POST', '/chat')).toHaveLength(0)
    // only the page-data GET went out, and without credentials
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect((fetchMock.mock.calls[0][1] as RequestInit).credentials).toBe('omit')
    // typing alone does nothing either
    await user.type(screen.getByPlaceholderText('輸入訊息……'), '你好')
    expect(bridges).toHaveLength(0)
  })

  it('sends to the backend first, forwards the backend content to onagent, then reports the reply', async () => {
    let releaseChat!: (r: Response) => void
    const chatGate = new Promise<Response>((resolve) => (releaseChat = resolve))
    handler = (url) => {
      if (url.endsWith('/public/businesses/shop')) return json(BUSINESS)
      if (url.endsWith('/chat')) return chatGate
      if (url.endsWith('/chat/reply')) return json({ messageId: 9 })
      return apiError(404, 'not_found')
    }

    const user = await openChat()
    await user.type(screen.getByPlaceholderText('輸入訊息……'), '  幾點開門？  ')
    await user.click(screen.getByRole('button', { name: '送出' }))

    // The backend call is in flight; onagent has not been touched yet.
    await waitFor(() => expect(calls('POST', '/chat')).toHaveLength(1))
    expect(JSON.parse(calls('POST', '/chat')[0][1]!.body as string)).toEqual({ content: '幾點開門？' })
    expect(bridges).toHaveLength(0)

    // The backend answers; only now is the bridge built and prompted — with the
    // backend's returned content, not the raw input.
    releaseChat(json({ conversationId: 'c1', messageId: 7, content: '幾點開門？(from backend)' }))
    await waitFor(() => expect(bridges).toHaveLength(1))
    expect(bridges[0].prompts).toEqual(['幾點開門？(from backend)'])
    expect(bridges[0].opts).toMatchObject({ url: 'wss://onagent.test/ws', appId: 'app-1', apiKey: 'browser-key', lazyConnect: true })

    // onagent replies: shown, and reported to the backend with the ids.
    bridges[0].opts.onAssistantMessage!('早上九點開門。')
    expect(await screen.findByText('早上九點開門。')).toBeInTheDocument()
    await waitFor(() => expect(calls('POST', '/chat/reply')).toHaveLength(1))
    expect(JSON.parse(calls('POST', '/chat/reply')[0][1]!.body as string)).toEqual({
      conversationId: 'c1',
      messageId: 7,
      content: '早上九點開門。',
    })

    // Second message continues the same conversation and reuses the bridge.
    handler = (url) => {
      if (url.endsWith('/chat')) return json({ conversationId: 'c1', messageId: 8, content: '謝謝' })
      if (url.endsWith('/chat/reply')) return json({ messageId: 10 })
      return apiError(404, 'not_found')
    }
    await user.type(screen.getByPlaceholderText('輸入訊息……'), '謝謝')
    await user.click(screen.getByRole('button', { name: '送出' }))
    await waitFor(() => expect(bridges[0].prompts).toHaveLength(2))
    expect(bridges).toHaveLength(1)
    expect(JSON.parse(calls('POST', '/chat')[1][1]!.body as string)).toEqual({ conversationId: 'c1', content: '謝謝' })
  })

  it('shows "unable to serve" and never reaches onagent when the owner is over quota', async () => {
    handler = (url) => {
      if (url.endsWith('/public/businesses/shop')) return json(BUSINESS)
      return apiError(429, 'quota_exceeded')
    }
    const user = await openChat()
    await user.type(screen.getByPlaceholderText('輸入訊息……'), '你好')
    await user.click(screen.getByRole('button', { name: '送出' }))

    expect(await screen.findByRole('alert')).toHaveTextContent('目前無法服務')
    expect(bridges).toHaveLength(0)
    expect(screen.getByPlaceholderText('輸入訊息……')).toBeDisabled()
  })

  it('shows an error and lets the visitor retry when the backend call fails', async () => {
    handler = (url) => {
      if (url.endsWith('/public/businesses/shop')) return json(BUSINESS)
      return apiError(500, 'internal_error')
    }
    const user = await openChat()
    await user.type(screen.getByPlaceholderText('輸入訊息……'), '你好')
    await user.click(screen.getByRole('button', { name: '送出' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('訊息沒有送出')
    expect(bridges).toHaveLength(0)
    expect(screen.getByPlaceholderText('輸入訊息……')).toBeEnabled()
  })

  it('shows an error when onagent reports one, and does not report a reply', async () => {
    handler = (url) => {
      if (url.endsWith('/public/businesses/shop')) return json(BUSINESS)
      return json({ conversationId: 'c1', messageId: 1, content: '你好' })
    }
    const user = await openChat()
    await user.type(screen.getByPlaceholderText('輸入訊息……'), '你好')
    await user.click(screen.getByRole('button', { name: '送出' }))
    await waitFor(() => expect(bridges).toHaveLength(1))

    bridges[0].opts.onError!({ message: 'inference error: boom' })
    expect(await screen.findByRole('alert')).toHaveTextContent('AI 暫時沒辦法回覆')
    expect(screen.queryByText(/boom/)).not.toBeInTheDocument() // internal detail not shown
    expect(calls('POST', '/chat/reply')).toHaveLength(0)
  })

  it('treats onagent-side quota exhaustion as unavailable', async () => {
    handler = (url) => {
      if (url.endsWith('/public/businesses/shop')) return json(BUSINESS)
      return json({ conversationId: 'c1', messageId: 1, content: '你好' })
    }
    const user = await openChat()
    await user.type(screen.getByPlaceholderText('輸入訊息……'), '你好')
    await user.click(screen.getByRole('button', { name: '送出' }))
    await waitFor(() => expect(bridges).toHaveLength(1))
    bridges[0].opts.onQuotaExceeded!({ message: 'quota' })
    expect(await screen.findByRole('alert')).toHaveTextContent('目前無法服務')
  })

  it('does not offer to start when the backend says chat is unavailable', async () => {
    handler = () => json({ ...BUSINESS, chat: { available: false } })
    render(<App />)
    expect(await screen.findByText(/目前無法服務/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '開始對話' })).not.toBeInTheDocument()
    expect(bridges).toHaveLength(0)
  })

  it('renders the split layout with a placeholder side panel when the owner chose it', async () => {
    handler = () => json({ ...BUSINESS, layout: 'split' })
    render(<App />)
    expect(await screen.findByText(/之後可以在這裡放上你的圖片/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '開始對話' })).toBeInTheDocument()
  })

  it('registers list_sections/read_section tools that call the backend sections API', async () => {
    handler = (url) => {
      if (url.endsWith('/public/businesses/shop')) return json(BUSINESS)
      if (url.endsWith('/sections')) return json({ sections: [{ id: 'hours', title: '營業時間' }] })
      if (url.endsWith('/sections/hours')) return json({ id: 'hours', title: '營業時間', body: '9-18 點' })
      return json({ conversationId: 'c1', messageId: 1, content: '你好' })
    }
    const user = await openChat()
    await user.type(screen.getByPlaceholderText('輸入訊息……'), '你好')
    await user.click(screen.getByRole('button', { name: '送出' }))
    await waitFor(() => expect(bridges).toHaveLength(1))

    const tools = bridges[0].opts.tools as Array<{ name: string; handle: (args: unknown) => unknown }>
    const listTool = tools.find((t) => t.name === 'list_sections')!
    const readTool = tools.find((t) => t.name === 'read_section')!
    expect(listTool).toBeTruthy()
    expect(readTool).toBeTruthy()

    // Every call must carry businessSlug (the tool definitions in
    // backend/onagent-tools/*.yaml require it — see useChat.ts's
    // requireOwnSlug), because every business's page now shares the same
    // onagent app instead of each having its own.
    await expect(listTool.handle({ businessSlug: 'shop' })).resolves.toEqual({
      sections: [{ id: 'hours', title: '營業時間' }],
    })
    await expect(readTool.handle({ businessSlug: 'shop', sectionId: 'hours' })).resolves.toEqual({
      id: 'hours',
      title: '營業時間',
      body: '9-18 點',
    })
    expect(calls('GET', '/sections')).toHaveLength(1)
    expect(calls('GET', '/sections/hours')).toHaveLength(1)
    expect(() => readTool.handle({ businessSlug: 'shop' })).toThrow()
    expect(() => listTool.handle({})).toThrow()
    expect(() => readTool.handle({ businessSlug: 'some-other-shop', sectionId: 'hours' })).toThrow()
  })

  it('closes the onagent connection when the page unmounts', async () => {
    handler = (url) => {
      if (url.endsWith('/public/businesses/shop')) return json(BUSINESS)
      return json({ conversationId: 'c1', messageId: 1, content: '你好' })
    }
    const user = userEvent.setup()
    const { unmount } = render(<App />)
    await user.click(await screen.findByRole('button', { name: '開始對話' }))
    await user.type(screen.getByPlaceholderText('輸入訊息……'), '你好')
    await user.click(screen.getByRole('button', { name: '送出' }))
    await waitFor(() => expect(bridges).toHaveLength(1))
    unmount()
    expect(bridges[0].closed).toBe(true)
  })
})
