import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { BackendProvider } from './BackendContext.tsx'
import { App } from './App.tsx'
import { createFakeBackend, type FakeBackend } from './fakeBackend.ts'

// The owner-settings flows in the console UI (list / create / edit content /
// edit look / delete / sync), run against a fake of the backend's HTTP API
// (fakeBackend.ts) — the same paths, status codes and field names as the real
// one. Nothing here is fake data inside the app itself.

const BAKERY = { Slug: 'chenguang-bakery', Name: '晨光烘焙坊', Tagline: '天然酵母麵包', Mascot: 'bear', ThemeColor: '#FF8A5B', Connected: true }
const STUDIO = { Slug: 'furry-studio', Name: '毛孩美容工作室', Tagline: '', Mascot: 'cat', ThemeColor: '#4ECDC4', Connected: false }

describe('console: owner settings against the backend API', () => {
  let fb: FakeBackend

  beforeEach(() => {
    fb = createFakeBackend({ businesses: [BAKERY, STUDIO] })
    vi.stubGlobal('fetch', vi.fn((url: string, init?: RequestInit) => fb.fetch(url, init)))
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText: () => Promise.resolve() } })
  })
  afterEach(() => {
    cleanup() // vitest runs without globals, so RTL does not auto-clean
    vi.unstubAllGlobals()
  })

  async function openApp() {
    render(
      <BackendProvider>
        <App />
      </BackendProvider>,
    )
    await screen.findByText('你的服務')
    return userEvent.setup()
  }

  async function openBusiness(user: ReturnType<typeof userEvent.setup>, name: string) {
    await user.click(await screen.findByText(name))
    await screen.findByRole('heading', { name })
  }

  // ---- list ---------------------------------------------------------------

  describe('list', () => {
    it('shows what the backend returns, with public links and AI status', async () => {
      await openApp()
      expect(await screen.findByText('晨光烘焙坊')).toBeInTheDocument()
      expect(screen.getByText('/support/chenguang-bakery')).toBeInTheDocument()
      expect(screen.getByText('毛孩美容工作室')).toBeInTheDocument()
      expect(screen.getByText('AI 小幫手已上線')).toBeInTheDocument()
      expect(screen.getByText('尚未啟用')).toBeInTheDocument()
      expect(screen.getByText('尚未填寫服務簡介')).toBeInTheDocument()
      expect(fb.callsTo('GET', /^\/console\/businesses$/)).toHaveLength(1)
    })

    it('shows an empty state when the owner has no business yet (backend returns null)', async () => {
      fb = createFakeBackend({ businesses: [] })
      await openApp()
      expect(await screen.findByText(/還沒有任何服務/)).toBeInTheDocument()
    })

    it('shows the error and can retry when the list cannot be loaded', async () => {
      fb.failNext('GET /console/businesses', 500, 'database is down')
      const user = await openApp()
      expect(await screen.findByRole('alert')).toHaveTextContent('database is down')
      expect(screen.getByRole('button', { name: /新增服務/ })).toBeDisabled() // nothing to add to yet
      await user.click(screen.getByRole('button', { name: '重新載入' }))
      expect(await screen.findByText('晨光烘焙坊')).toBeInTheDocument()
    })

    it('shows nothing owned by a previous login after logging out and in as someone else', async () => {
      const user = await openApp()
      await screen.findByText('晨光烘焙坊')
      await user.click(screen.getByRole('button', { name: '帳號選單' }))
      await user.click(await screen.findByRole('menuitem', { name: '登出' }))
      await screen.findByLabelText('密碼')
      fb.businesses = []
      await user.type(screen.getByLabelText('電子信箱'), 'other@example.com')
      await user.type(screen.getByLabelText('密碼'), 'password123')
      await user.click(screen.getByRole('button', { name: '登入' }))
      expect(await screen.findByText(/還沒有任何服務/)).toBeInTheDocument()
      expect(screen.queryByText('晨光烘焙坊')).not.toBeInTheDocument()
    })
  })

  // ---- create -------------------------------------------------------------

  describe('create', () => {
    async function openModal() {
      const user = await openApp()
      await user.click(await screen.findByRole('button', { name: /新增服務/ }))
      return user
    }

    it('suggests an ASCII URL from the name and only enables create for a valid name + URL', async () => {
      const user = await openModal()
      const create = screen.getByRole('button', { name: '建立' })
      expect(create).toBeDisabled()

      await user.type(screen.getByLabelText('服務名稱'), '  Demo Shop  ')
      expect(screen.getByLabelText('對外網址')).toHaveValue('demo-shop')
      expect(create).toBeEnabled()
    })

    it('a Chinese-only name gives no URL suggestion, so the owner must type one', async () => {
      const user = await openModal()
      await user.type(screen.getByLabelText('服務名稱'), '選民服務')
      expect(screen.getByLabelText('對外網址')).toHaveValue('')
      expect(screen.getByRole('button', { name: '建立' })).toBeDisabled()
      await user.type(screen.getByLabelText('對外網址'), 'voter-service')
      expect(screen.getByRole('button', { name: '建立' })).toBeEnabled()
    })

    it('rejects a whitespace-only name and an invalid URL before any request is made', async () => {
      const user = await openModal()
      await user.type(screen.getByLabelText('服務名稱'), '    ')
      await user.type(screen.getByLabelText('對外網址'), 'ok-url')
      expect(screen.getByRole('button', { name: '建立' })).toBeDisabled()

      await user.clear(screen.getByLabelText('服務名稱'))
      await user.type(screen.getByLabelText('服務名稱'), '名稱')
      await user.clear(screen.getByLabelText('對外網址'))
      await user.type(screen.getByLabelText('對外網址'), '-bad_url')
      expect(screen.getByText(/只能使用小寫英文字母/)).toBeInTheDocument()
      expect(screen.getByRole('button', { name: '建立' })).toBeDisabled()
      expect(fb.callsTo('POST', /businesses$/)).toHaveLength(0)
    })

    it('lowercases the URL as it is typed', async () => {
      const user = await openModal()
      await user.type(screen.getByLabelText('對外網址'), 'My-Shop')
      expect(screen.getByLabelText('對外網址')).toHaveValue('my-shop')
    })

    it('sends the trimmed values to the backend and opens the new business', async () => {
      const user = await openModal()
      await user.type(screen.getByLabelText('服務名稱'), '  選民服務  ')
      await user.type(screen.getByLabelText('對外網址'), 'voter-service')
      await user.click(screen.getByRole('button', { name: '小貓' }))
      await user.click(screen.getByRole('button', { name: '#4C9EFF' }))
      await user.click(screen.getByRole('button', { name: '建立' }))

      expect(await screen.findByRole('heading', { name: '選民服務' })).toBeInTheDocument()
      expect(screen.getByText('/support/voter-service')).toBeInTheDocument()

      const [post] = fb.callsTo('POST', /^\/console\/businesses$/)
      expect(post.body).toEqual({ slug: 'voter-service', name: '選民服務', mascot: 'cat', themeColor: '#4C9EFF' })
      expect(fb.businesses.map((b) => b.Slug)).toContain('voter-service')

      await user.click(screen.getByRole('button', { name: /返回列表/ }))
      expect(await screen.findByText('選民服務')).toBeInTheDocument()
      expect(screen.getAllByText(/^\/support\//)).toHaveLength(3)
    })

    it('says so when the URL is taken and keeps the dialog open with the input intact', async () => {
      const user = await openModal()
      await user.type(screen.getByLabelText('服務名稱'), '另一家')
      await user.type(screen.getByLabelText('對外網址'), 'chenguang-bakery')
      await user.click(screen.getByRole('button', { name: '建立' }))
      expect(await screen.findByRole('alert')).toHaveTextContent('這個網址已經被使用')
      expect(screen.getByLabelText('服務名稱')).toHaveValue('另一家')
      expect(screen.getByRole('button', { name: '建立' })).toBeEnabled() // can change the URL and retry
      expect(fb.businesses).toHaveLength(2)
    })

    it('shows other server errors and does not add a phantom entry', async () => {
      fb.failNext('POST /console/businesses', 500, 'failed to create business')
      const user = await openModal()
      await user.type(screen.getByLabelText('服務名稱'), '店')
      await user.type(screen.getByLabelText('對外網址'), 'new-shop')
      await user.click(screen.getByRole('button', { name: '建立' }))
      expect(await screen.findByRole('alert')).toHaveTextContent('failed to create business')
      await user.click(screen.getByRole('button', { name: '取消' }))
      expect(screen.getAllByText(/^\/support\//)).toHaveLength(2)
    })

    it('cancelling adds nothing and sends nothing', async () => {
      const user = await openModal()
      await user.type(screen.getByLabelText('服務名稱'), '不要建立')
      await user.click(screen.getByRole('button', { name: '取消' }))
      expect(screen.queryByText('不要建立')).not.toBeInTheDocument()
      expect(fb.callsTo('POST', /businesses$/)).toHaveLength(0)
    })
  })

  // ---- edit content -------------------------------------------------------

  describe('edit content', () => {
    it('loads the saved sections from the backend', async () => {
      const id = fb.businesses[0].ID
      fb.contents.set(id, {
        Content: '【營業時間】\n週二到週日',
        Sections: [{ id: 'hours', title: '營業時間', body: '週二到週日 8:00–19:00' }],
      })
      const user = await openApp()
      await openBusiness(user, '晨光烘焙坊')
      expect((await screen.findAllByPlaceholderText('在這裡輸入內容……'))[0]).toHaveValue('週二到週日 8:00–19:00')
    })

    it('keeps text that was saved without editor state instead of dropping it', async () => {
      const id = fb.businesses[1].ID
      fb.contents.set(id, { Content: '直接用 API 寫入的內容', Sections: null })
      const user = await openApp()
      await openBusiness(user, '毛孩美容工作室')
      await screen.findAllByPlaceholderText('在這裡輸入內容……')
      const preview = screen.getByText('內容預覽（AI 會依需要查詢各章節，不會一次整段提供）').parentElement as HTMLElement
      expect(within(preview).getByText(/直接用 API 寫入的內容/)).toBeInTheDocument()
    })

    it('tracks unsaved changes, saves flat text + sections, and persists across reopening', async () => {
      const user = await openApp()
      await openBusiness(user, '毛孩美容工作室')
      const hours = (await screen.findAllByPlaceholderText('在這裡輸入內容……'))[0]

      expect(screen.getByText('所有變更都已儲存')).toBeInTheDocument()
      const save = screen.getByRole('button', { name: '儲存變更' })
      expect(save).toBeDisabled()

      await user.type(hours, '每天 10:00–20:00')
      expect(screen.getByText('有尚未儲存的變更')).toBeInTheDocument()
      const preview = screen.getByText('內容預覽（AI 會依需要查詢各章節，不會一次整段提供）').parentElement as HTMLElement
      expect(within(preview).getByText(/【營業時間】/)).toBeInTheDocument()

      await user.click(save)
      expect(await screen.findByText('所有變更都已儲存')).toBeInTheDocument()

      const [put] = fb.callsTo('PUT', /\/content$/)
      const body = put.body as { content: string; sections: { id: string; body: string }[] }
      expect(body.content).toBe('【營業時間】\n每天 10:00–20:00')
      expect(body.sections.find((s) => s.id === 'hours')?.body).toBe('每天 10:00–20:00')
      expect(body.sections).toHaveLength(6)

      await user.click(screen.getByRole('button', { name: /返回列表/ }))
      await openBusiness(user, '毛孩美容工作室')
      expect((await screen.findAllByPlaceholderText('在這裡輸入內容……'))[0]).toHaveValue('每天 10:00–20:00')
    })

    it('does not mix up drafts between two businesses', async () => {
      const user = await openApp()
      await openBusiness(user, '晨光烘焙坊')
      await user.type((await screen.findAllByPlaceholderText('在這裡輸入內容……'))[0], '（草稿，未儲存）')
      await user.click(screen.getByRole('button', { name: /返回列表/ }))
      await openBusiness(user, '毛孩美容工作室')
      expect((await screen.findAllByPlaceholderText('在這裡輸入內容……'))[0]).toHaveValue('')
    })

    it('shows a load error and can retry', async () => {
      const id = fb.businesses[0].ID
      fb.failNext(`GET /console/businesses/${id}/content`, 500, 'oops')
      const user = await openApp()
      await user.click(await screen.findByText('晨光烘焙坊'))
      expect(await screen.findByRole('alert')).toHaveTextContent('無法載入內容')
      await user.click(screen.getByRole('button', { name: '重試' }))
      expect(await screen.findAllByPlaceholderText('在這裡輸入內容……')).not.toHaveLength(0)
    })

    it('keeps the draft and shows the error when saving fails', async () => {
      const id = fb.businesses[1].ID
      fb.failNext(`PUT /console/businesses/${id}/content`, 500, 'failed to save content')
      const user = await openApp()
      await openBusiness(user, '毛孩美容工作室')
      await user.type((await screen.findAllByPlaceholderText('在這裡輸入內容……'))[0], '重要內容')
      await user.click(screen.getByRole('button', { name: '儲存變更' }))
      expect(await screen.findByText('failed to save content')).toBeInTheDocument()
      expect(screen.getAllByPlaceholderText('在這裡輸入內容……')[0]).toHaveValue('重要內容')
      expect(screen.getByRole('button', { name: '儲存變更' })).toBeEnabled() // can try again
    })
  })

  // ---- AI sync ------------------------------------------------------------

  describe('AI sync status', () => {
    async function saveSomething() {
      const user = await openApp()
      await openBusiness(user, '毛孩美容工作室')
      await user.type((await screen.findAllByPlaceholderText('在這裡輸入內容……'))[0], '內容')
      await user.click(screen.getByRole('button', { name: '儲存變更' }))
      return user
    }

    it('confirms when the AI has the latest content', async () => {
      fb.syncResult = 'ok'
      await saveSomething()
      expect(await screen.findByText(/AI 小幫手已經用到最新內容/)).toBeInTheDocument()
    })

    it('warns when the sync failed but the content was saved, and can re-sync', async () => {
      fb.syncResult = 'failed'
      const user = await saveSomething()
      expect(await screen.findByText(/同步給 AI 小幫手失敗/)).toBeInTheDocument()
      expect(fb.contents.get(fb.businesses[1].ID)?.Content).toContain('內容') // saved regardless

      fb.syncResult = 'ok'
      await user.click(screen.getByRole('button', { name: '重新同步' }))
      await waitFor(() => expect(fb.callsTo('POST', /onagent-sync$/)).toHaveLength(1))
      await waitFor(() => expect(screen.queryByText(/同步給 AI 小幫手失敗/)).not.toBeInTheDocument())
      expect(await screen.findByText(/AI 小幫手已經用到最新內容/)).toBeInTheDocument()
    })

    it('says the AI service is not enabled yet when the server has no onagent configured', async () => {
      fb.syncResult = 'disabled'
      await saveSomething()
      expect(await screen.findByText(/AI 對話服務目前還沒啟用/)).toBeInTheDocument()
    })
  })

  // ---- edit look ----------------------------------------------------------

  describe('edit look', () => {
    async function openBranding() {
      const user = await openApp()
      await openBusiness(user, '晨光烘焙坊')
      await user.click(screen.getByRole('button', { name: '形象設定' }))
      return user
    }

    it('sends only the changed fields, trimmed, and updates the list', async () => {
      const user = await openBranding()
      const name = screen.getByDisplayValue('晨光烘焙坊')
      await user.clear(name)
      await user.type(name, '  晨光麵包  ')
      await user.click(screen.getByRole('button', { name: '儲存變更' }))

      await waitFor(() => expect(fb.callsTo('PATCH', /businesses\/\d+$/)).toHaveLength(1))
      expect(fb.callsTo('PATCH', /businesses\/\d+$/)[0].body).toEqual({ name: '晨光麵包' })
      expect(await screen.findByText('所有變更都已儲存')).toBeInTheDocument()

      await user.click(screen.getByRole('button', { name: /返回列表/ }))
      expect(await screen.findByText('晨光麵包')).toBeInTheDocument()
      expect(screen.queryByText('晨光烘焙坊')).not.toBeInTheDocument()
    })

    it('changes tagline, mascot and colour together', async () => {
      const user = await openBranding()
      await user.type(screen.getByPlaceholderText('一句話介紹你的服務'), '，每日現烤')
      await user.click(screen.getByTitle('小鳥'))
      await user.click(screen.getByRole('button', { name: '#8E7DFF' }))
      await user.click(screen.getByRole('button', { name: '儲存變更' }))
      await waitFor(() => expect(fb.callsTo('PATCH', /businesses\/\d+$/)).toHaveLength(1))
      expect(fb.callsTo('PATCH', /businesses\/\d+$/)[0].body).toEqual({
        tagline: '天然酵母麵包，每日現烤',
        mascot: 'bird',
        themeColor: '#8E7DFF',
      })
    })

    it('cannot save an empty name', async () => {
      const user = await openBranding()
      const name = screen.getByDisplayValue('晨光烘焙坊')
      await user.clear(name)
      await user.type(name, '   ')
      expect(screen.getByRole('button', { name: '儲存變更' })).toBeDisabled()
      expect(fb.callsTo('PATCH', /businesses\/\d+$/)).toHaveLength(0)
    })

    it('shows the backend validation error and keeps the edit', async () => {
      const id = fb.businesses[0].ID
      fb.failNext(`PATCH /console/businesses/${id}`, 400, 'invalid branding: unknown mascot')
      const user = await openBranding()
      await user.click(screen.getByTitle('小貓'))
      await user.click(screen.getByRole('button', { name: '儲存變更' }))
      expect(await screen.findByText(/invalid branding/)).toBeInTheDocument()
      expect(screen.getByRole('button', { name: '儲存變更' })).toBeEnabled() // still dirty: the edit is kept
    })

    it('the public URL cannot be edited here', async () => {
      await openBranding()
      expect(screen.queryByDisplayValue('chenguang-bakery')).not.toBeInTheDocument()
    })
  })

  // ---- delete -------------------------------------------------------------

  describe('delete', () => {
    async function openDelete() {
      const user = await openApp()
      await openBusiness(user, '毛孩美容工作室')
      await user.click(screen.getByRole('button', { name: '形象設定' }))
      await user.click(screen.getByRole('button', { name: '刪除這個服務' }))
      return user
    }

    it('asks for confirmation first and sends nothing until it is given', async () => {
      await openDelete()
      expect(screen.getByRole('alertdialog')).toHaveTextContent('確定要刪除「毛孩美容工作室」')
      expect(fb.callsTo('DELETE', /businesses\/\d+$/)).toHaveLength(0)
    })

    it('cancelling keeps the business and stays on the screen (no silent navigation)', async () => {
      const user = await openDelete()
      await user.click(screen.getByRole('button', { name: '取消' }))
      expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
      expect(screen.getByRole('heading', { name: '毛孩美容工作室' })).toBeInTheDocument()
      expect(fb.callsTo('DELETE', /businesses\/\d+$/)).toHaveLength(0)
      expect(fb.businesses).toHaveLength(2)
    })

    it('confirming deletes it on the backend and returns to a list without it', async () => {
      const user = await openDelete()
      await user.click(screen.getByRole('button', { name: '確定刪除' }))
      await screen.findByText('你的服務')
      await waitFor(() => expect(screen.queryByText('毛孩美容工作室')).not.toBeInTheDocument())
      expect(screen.getByText('晨光烘焙坊')).toBeInTheDocument()
      expect(fb.businesses.map((b) => b.Slug)).toEqual(['chenguang-bakery'])
    })

    it('stays and shows the error when the backend refuses', async () => {
      const id = fb.businesses[1].ID
      fb.failNext(`DELETE /console/businesses/${id}`, 500, 'failed to delete business')
      const user = await openDelete()
      await user.click(screen.getByRole('button', { name: '確定刪除' }))
      expect(await screen.findByText('failed to delete business')).toBeInTheDocument()
      expect(screen.getByRole('heading', { name: '毛孩美容工作室' })).toBeInTheDocument()
      expect(fb.businesses).toHaveLength(2)
    })
  })

  // ---- session ------------------------------------------------------------

  describe('session', () => {
    it('logs out through the backend and returns to the login screen', async () => {
      const user = await openApp()
      await user.click(screen.getByRole('button', { name: '帳號選單' }))
      await user.click(await screen.findByRole('menuitem', { name: '登出' }))
      expect(await screen.findByLabelText('密碼')).toBeInTheDocument()
      expect(fb.callsTo('POST', /\/auth\/logout$/)).toHaveLength(1)
    })

    it('logs out locally even if the backend call fails', async () => {
      const user = await openApp()
      fb.failNext('POST /auth/logout', 500)
      await user.click(screen.getByRole('button', { name: '帳號選單' }))
      await user.click(await screen.findByRole('menuitem', { name: '登出' }))
      expect(await screen.findByLabelText('密碼')).toBeInTheDocument()
    })

    it('a backend that is down at startup shows the login screen, not a crash', async () => {
      fb.offline = true
      render(
        <BackendProvider>
          <App />
        </BackendProvider>,
      )
      expect(await screen.findByLabelText('密碼')).toBeInTheDocument()
    })
  })
})
