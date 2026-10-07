import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { BackendProvider } from './BackendContext.tsx'
import { App } from './App.tsx'

function jsonResponse(status: number, body: unknown) {
  return new Response(typeof body === 'string' ? body : JSON.stringify(body), { status })
}

describe('console login against the backend', () => {
  const fetchMock = vi.fn()

  beforeEach(() => {
    fetchMock.mockReset()
    vi.stubGlobal('fetch', fetchMock)
  })
  afterEach(() => {
    cleanup() // vitest runs without globals, so RTL does not auto-clean
    vi.unstubAllGlobals()
  })

  function renderApp() {
    return render(
      <BackendProvider>
        <App />
      </BackendProvider>,
    )
  }

  it('restores an existing session from /auth/me without showing the login form', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(200, { ID: 1, Email: 'owner@example.com' }))
    renderApp()
    // The email lives inside the account menu's dropdown now (see
    // AccountMenu), not displayed flat in the top bar — open it first.
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: '帳號選單' }))
    expect(await screen.findByText('owner@example.com')).toBeInTheDocument()
    expect(screen.queryByLabelText('密碼')).not.toBeInTheDocument()
  })

  it('logs in with the trimmed email and the untrimmed password', async () => {
    fetchMock
      .mockResolvedValueOnce(jsonResponse(401, 'not authenticated')) // /auth/me
      .mockResolvedValueOnce(jsonResponse(200, { ID: 1, Email: 'a@b.co' })) // /auth/login
    renderApp()
    const user = userEvent.setup()
    await user.type(await screen.findByLabelText('電子信箱'), '  a@b.co  ')
    await user.type(screen.getByLabelText('密碼'), ' pw with spaces ')
    await user.click(screen.getByRole('button', { name: '登入' }))

    // After a successful login the console also loads the owner's businesses.
    await waitFor(() => expect(fetchMock.mock.calls.length).toBeGreaterThanOrEqual(2))
    const [url, init] = fetchMock.mock.calls[1]
    expect(String(url)).toMatch(/\/auth\/login$/)
    expect(init.credentials).toBe('include')
    expect(JSON.parse(init.body)).toEqual({ email: 'a@b.co', password: ' pw with spaces ' })
    await user.click(await screen.findByRole('button', { name: '帳號選單' }))
    expect(await screen.findByText('a@b.co')).toBeInTheDocument()
  })

  it('shows the backend error and stays on the form when login fails', async () => {
    fetchMock
      .mockResolvedValueOnce(jsonResponse(401, 'not authenticated'))
      .mockResolvedValueOnce(jsonResponse(401, 'invalid email or password'))
    renderApp()
    const user = userEvent.setup()
    await user.type(await screen.findByLabelText('電子信箱'), 'a@b.co')
    await user.type(screen.getByLabelText('密碼'), 'wrong')
    await user.click(screen.getByRole('button', { name: '登入' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('invalid email or password')
    expect(screen.getByLabelText('密碼')).toBeInTheDocument()
  })

  it('registers through /auth/register when switched to register mode', async () => {
    fetchMock
      .mockResolvedValueOnce(jsonResponse(401, 'not authenticated'))
      .mockResolvedValueOnce(jsonResponse(200, { ID: 2, Email: 'new@b.co' }))
    renderApp()
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: '建立帳號' }))
    await user.type(screen.getByLabelText('電子信箱'), 'new@b.co')
    await user.type(screen.getByLabelText('密碼'), 'password123')
    await user.click(screen.getByRole('button', { name: '註冊並登入' }))
    await waitFor(() => expect(String(fetchMock.mock.calls[1][0])).toMatch(/\/auth\/register$/))
  })
})
