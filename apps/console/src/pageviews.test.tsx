import { StrictMode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { BackendProvider } from './BackendContext.tsx'
import { App } from './App.tsx'
import { trackPageView } from './analytics.ts'
import { createFakeBackend, type FakeBackend } from './fakeBackend.ts'

// The console never changes its URL, so each screen reports itself to Google
// Analytics as a virtual page. These tests run as a production page where
// index.html enabled GA; gtag is a recorder, so nothing leaves the process.

type Sent = unknown[]
const pageViews = (sent: Sent[]) => sent.filter((c) => c[1] === 'page_view').map((c) => (c[2] as { page_path: string }).page_path)

describe('console analytics: the owner’s path through the app', () => {
  let fb: FakeBackend
  let sent: Sent[]

  beforeEach(() => {
    sent = []
    fb = createFakeBackend({ businesses: [{ Slug: 'shop', Name: '晨光烘焙坊' }] })
    vi.stubGlobal('fetch', vi.fn((url: string, init?: RequestInit) => fb.fetch(url, init)))
    vi.stubEnv('DEV', false)
    Object.assign(window, { __gaEnabled: true, gtag: (...args: unknown[]) => sent.push(args) })
    // analytics.ts remembers the last reported path across tests; park it on a neutral
    // path so each test starts from "nothing reported yet".
    trackPageView('/__test_reset__', '')
    sent.length = 0
  })
  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    vi.unstubAllEnvs()
    delete (window as unknown as Record<string, unknown>).__gaEnabled
    delete (window as unknown as Record<string, unknown>).gtag
  })

  function openApp() {
    render(
      <BackendProvider>
        <App />
      </BackendProvider>,
    )
    return userEvent.setup()
  }

  it('reports the list, then each editor tab, with the visitor’s route as plain paths', async () => {
    const user = openApp()
    await screen.findByText('晨光烘焙坊')
    expect(pageViews(sent)).toEqual(['/businesses'])

    await user.click(screen.getByText('晨光烘焙坊'))
    await screen.findByRole('heading', { name: '晨光烘焙坊' })
    await user.click(screen.getByRole('button', { name: '形象設定' }))
    await user.click(screen.getByRole('button', { name: 'AI 可以回答的內容' }))
    await user.click(screen.getByRole('button', { name: /返回列表/ }))
    await screen.findByText('你的服務')

    expect(pageViews(sent)).toEqual([
      '/businesses',
      '/businesses/edit/content',
      '/businesses/edit/branding',
      '/businesses/edit/content',
      '/businesses',
    ])
  })

  it('under StrictMode (effects run twice) page views are not duplicated', async () => {
    const user = userEvent.setup()
    render(
      <StrictMode>
        <BackendProvider>
          <App />
        </BackendProvider>
      </StrictMode>,
    )
    await screen.findByText('晨光烘焙坊')
    expect(pageViews(sent)).toEqual(['/businesses'])

    await user.click(screen.getByText('晨光烘焙坊'))
    await screen.findByRole('heading', { name: '晨光烘焙坊' })
    await user.click(screen.getByRole('button', { name: '形象設定' }))
    await user.click(screen.getByRole('button', { name: 'AI 可以回答的內容' }))

    const views = pageViews(sent)
    expect(views.length).toBeGreaterThan(1)
    views.forEach((v, i) => { if (i > 0) expect(v, `index ${i}`).not.toBe(views[i - 1]) })
  })

  it('reports the login screen when nobody is logged in', async () => {
    fb = createFakeBackend({ email: null })
    openApp()
    await screen.findByLabelText('密碼')
    expect(pageViews(sent)).toEqual(['/login'])
  })

  it('paths and titles carry no ids, names, emails or slugs', async () => {
    const user = openApp()
    await user.click(await screen.findByText('晨光烘焙坊'))
    await screen.findByRole('heading', { name: '晨光烘焙坊' })
    const wire = JSON.stringify(sent)
    for (const personal of ['晨光烘焙坊', 'shop', 'owner@example.com']) expect(wire).not.toContain(personal)
    expect(wire).not.toMatch(/businesses\/\d/)
  })

  it('counts a new registration as sign_up once, and a login as nothing', async () => {
    fb = createFakeBackend({ email: null })
    const user = openApp()
    await user.click(await screen.findByRole('button', { name: '建立帳號' }))
    await user.type(screen.getByLabelText('電子信箱'), 'new@b.co')
    await user.type(screen.getByLabelText('密碼'), 'password123')
    await user.click(screen.getByRole('button', { name: '註冊並登入' }))
    await screen.findByText('你的服務')
    expect(sent.filter((c) => c[1] === 'sign_up')).toEqual([['event', 'sign_up', { method: 'email' }]])
  })

  it('sends nothing on an admin path', async () => {
    window.history.pushState({}, '', '/admin/console')
    openApp()
    await screen.findByText('晨光烘焙坊')
    expect(sent).toEqual([])
    window.history.pushState({}, '', '/')
  })

  it('sends nothing at all in development (the default)', async () => {
    vi.stubEnv('DEV', true)
    openApp()
    await screen.findByText('晨光烘焙坊')
    expect(sent).toEqual([])
  })
})
