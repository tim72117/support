import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { vi } from 'vitest'
import { BackendProvider } from './BackendContext.tsx'
import { App } from './App.tsx'
import { createFakeBackend, type FakeBackend } from './fakeBackend.ts'

// The "對話紀錄" tab in BusinessEditor, run against a fake of the backend's
// HTTP API (fakeBackend.ts) for GET .../conversations and
// GET .../conversations/{cid} — same paths, status codes and field names
// (camelCase, per backend/internal/conversation's json tags) as the real one.

const STUDIO = { Slug: 'furry-studio', Name: '毛孩美容工作室', Tagline: '', Mascot: 'cat', ThemeColor: '#4ECDC4' }

describe('console: conversation history tab', () => {
  let fb: FakeBackend

  beforeEach(() => {
    fb = createFakeBackend({ businesses: [STUDIO] })
    vi.stubGlobal('fetch', vi.fn((url: string, init?: RequestInit) => fb.fetch(url, init)))
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText: () => Promise.resolve() } })
  })
  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  async function openTab() {
    render(
      <BackendProvider>
        <App />
      </BackendProvider>,
    )
    const user = userEvent.setup()
    await user.click(await screen.findByText('毛孩美容工作室'))
    await screen.findByRole('heading', { name: '毛孩美容工作室' })
    await user.click(screen.getByRole('button', { name: '對話紀錄' }))
    return user
  }

  it('shows an empty state when the business has no conversations yet', async () => {
    await openTab()
    expect(await screen.findByText(/還沒有任何對話紀錄/)).toBeInTheDocument()
  })

  it('says so when conversations are not enabled on this server', async () => {
    fb.conversationsEnabled = false
    await openTab()
    expect(await screen.findByText(/還沒有啟用對話紀錄功能/)).toBeInTheDocument()
  })

  it('lists conversations and shows the full message thread when opened', async () => {
    const bizId = fb.businesses[0].ID
    fb.conversations.push({ id: 'abc12345ff', businessId: bizId, createdAt: '2026-01-02T03:04:00Z' })
    fb.messages.push(
      { id: 1, conversationId: 'abc12345ff', role: 'user', content: '營業時間是幾點？', createdAt: '2026-01-02T03:04:00Z' },
      { id: 2, conversationId: 'abc12345ff', role: 'assistant', content: '我們每天 10:00–20:00 營業。', replyTo: 1, createdAt: '2026-01-02T03:05:00Z' },
    )

    const user = await openTab()
    expect(await screen.findByText(/對話 #abc12345/)).toBeInTheDocument()

    await user.click(screen.getByText(/對話 #abc12345/))
    expect(await screen.findByText('營業時間是幾點？')).toBeInTheDocument()
    expect(screen.getByText('我們每天 10:00–20:00 營業。')).toBeInTheDocument()
    expect(screen.getByText('顧客')).toBeInTheDocument()
    expect(screen.getByText('AI 小幫手')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /返回對話列表/ }))
    expect(await screen.findByText(/對話 #abc12345/)).toBeInTheDocument()
  })

  it('shows an error and can retry when the list fails to load', async () => {
    const bizId = fb.businesses[0].ID
    fb.failNext(`GET /console/businesses/${bizId}/conversations`, 500, 'database is down')
    await openTab()
    expect(await screen.findByRole('alert')).toHaveTextContent('database is down')

    fb.conversations.push({ id: 'ok-conv', businessId: bizId, createdAt: '2026-01-02T03:04:00Z' })
    await userEvent.setup().click(screen.getByRole('button', { name: '重試' }))
    expect(await screen.findByText(/對話 #ok-conv/)).toBeInTheDocument()
  })
})
