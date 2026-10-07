import { describe, expect, it } from 'vitest'
import { JSDOM } from 'jsdom'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

// The subscribe page's flow (register/login -> TapPay prime -> subscribe),
// run in jsdom with BOTH the backend (fetch) and TapPay's SDK (TPDirect)
// replaced by fakes. Nothing leaves the process.

const page = readFileSync(fileURLToPath(new URL('../candidate/subscribe.html', import.meta.url)), 'utf8')
  // The real SDK is a remote script; the fake TPDirect below replaces it.
  .replace(/<script src="https:\/\/js\.tappaysdk\.com[^>]*><\/script>/, '')

const tick = (ms = 40) => new Promise((r) => setTimeout(r, ms))

async function open({ me = null, subscribe = { status: 200 }, auth = { status: 200 }, billing = true, getPrime } = {}) {
  const calls = []
  const dom = new JSDOM(page, {
    runScripts: 'dangerously',
    url: 'http://localhost:5176/candidate/subscribe.html',
    beforeParse(w) {
      w.TPDirect = {
        setupSDK() {},
        card: {
          setup() {},
          onUpdate(cb) { w.__cardUpdate = cb },
          getPrime(cb) {
            cb(getPrime ?? { status: 0, card: { prime: 'PRIME_X' } })
          },
        },
      }
      // jsdom has no fetch/Response; use Node's.
      w.fetch = async (url, init = {}) => {
        const path = url.replace('http://localhost:8082', '')
        calls.push({ path, method: init.method, body: init.body ? JSON.parse(init.body) : undefined, credentials: init.credentials })
        const reply = (status, body) => new Response(typeof body === 'string' ? body : JSON.stringify(body), { status })
        if (path === '/auth/me') return me ? reply(200, { ID: 1, Email: me }) : reply(401, 'not authenticated')
        if (path === '/console/billing/config') {
          return reply(200, billing ? { enabled: true, appId: '123', appKey: 'app_key', env: 'sandbox' } : { enabled: false })
        }
        if (path === '/auth/register' || path === '/auth/login') {
          return auth.status === 200 ? reply(200, { ID: 2, Email: 'x' }) : reply(auth.status, auth.text ?? 'rejected')
        }
        if (path === '/auth/logout') return reply(204, '')
        if (path === '/console/billing/subscribe') {
          return subscribe.status === 200 ? reply(200, { tier: 'campaign', status: 'active' }) : reply(subscribe.status, subscribe.text ?? 'x')
        }
        return reply(404, 'not found')
      }
    },
  })
  await tick()
  const { window: w } = dom
  const d = w.document
  const $ = (id) => d.getElementById(id)
  return {
    w, d, $, calls,
    fill: (name, value) => { d.querySelector(`[name=${name}]`).value = value },
    cardValid: () => w.__cardUpdate({ canGetPrime: true, status: {} }),
    async submit() {
      d.getElementById('signup').dispatchEvent(new w.Event('submit', { cancelable: true, bubbles: true }))
      await tick()
    },
    paths: () => calls.map((c) => c.path),
    call: (path) => calls.find((c) => c.path === path),
  }
}

describe('subscribe page', () => {
  it('a new visitor registers, pays, and is sent to the console', async () => {
    const p = await open()
    expect(p.$('submitBtn').disabled).toBe(true) // card not valid yet
    p.cardValid()
    expect(p.$('submitBtn').disabled).toBe(false)

    p.fill('email', '  new@b.co  ')
    p.fill('password', ' password123 ')
    p.fill('name', ' 王小明競選辦公室 ')
    p.fill('phone', ' 0912345678 ')
    await p.submit()

    const order = p.paths()
    expect(order.indexOf('/auth/register')).toBeGreaterThanOrEqual(0)
    expect(order.indexOf('/auth/register')).toBeLessThan(order.indexOf('/console/billing/subscribe'))

    const reg = p.call('/auth/register')
    expect(reg.credentials).toBe('include')
    expect(reg.body).toEqual({ email: 'new@b.co', password: ' password123 ' }) // email trimmed, password untouched

    const sub = p.call('/console/billing/subscribe')
    expect(sub.body.tier).toBe('campaign') // the preselected plan
    expect(sub.body.prime).toBe('PRIME_X')
    expect(sub.body.cardholder).toEqual({ name: '王小明競選辦公室', email: 'new@b.co', phoneNumber: '0912345678' })
    expect(sub.body).not.toHaveProperty('amount') // the page never sends a price

    expect(p.$('done').hidden).toBe(false)
    expect(p.$('signup').hidden).toBe(true)
    expect(p.$('goConsole').href).toBe('http://localhost:5177/')
  })

  it('sends the plan the visitor picked', async () => {
    const p = await open({ me: 'me@b.co' })
    p.d.querySelector('input[name=plan][value=starter]').checked = true
    p.fill('name', 'T'); p.fill('phone', '09'); p.cardValid()
    await p.submit()
    expect(p.call('/console/billing/subscribe').body.tier).toBe('starter')
  })

  it('an already-logged-in visitor skips the account step and uses the session email', async () => {
    const p = await open({ me: 'me@b.co' })
    expect(p.$('who').hidden).toBe(false)
    expect(p.$('accountFields').hidden).toBe(true)
    p.fill('name', 'T'); p.fill('phone', '09'); p.cardValid()
    await p.submit()
    expect(p.paths().filter((x) => x.startsWith('/auth/') && x !== '/auth/me')).toEqual([])
    expect(p.call('/console/billing/subscribe').body.cardholder.email).toBe('me@b.co')
  })

  it('an existing user can log in instead of registering', async () => {
    const p = await open()
    p.d.querySelector('input[name=mode][value=login]').checked = true
    p.d.querySelector('input[name=mode][value=login]').dispatchEvent(new p.w.Event('change', { bubbles: true }))
    p.fill('email', 'old@b.co'); p.fill('password', 'whatever'); p.fill('name', 'T'); p.fill('phone', '09'); p.cardValid()
    await p.submit()
    expect(p.paths()).toContain('/auth/login')
    expect(p.paths()).not.toContain('/auth/register')
    expect(p.paths()).toContain('/console/billing/subscribe')
  })

  it('switching account logs out first', async () => {
    const p = await open({ me: 'me@b.co' })
    p.$('switchAccount').click()
    await tick()
    expect(p.paths()).toContain('/auth/logout')
    expect(p.$('accountFields').hidden).toBe(false)
  })

  it('a declined card shows a message, keeps the form, and allows another try', async () => {
    const p = await open({ me: 'me@b.co', subscribe: { status: 402, text: 'card declined: x' } })
    p.fill('name', 'T'); p.fill('phone', '09'); p.cardValid()
    await p.submit()
    expect(p.$('error').textContent).toMatch(/拒絕/)
    expect(p.$('error').textContent).not.toContain('card declined: x') // friendly text, not the raw body
    expect(p.$('done').hidden).toBe(true)
    expect(p.$('submitBtn').disabled).toBe(false)
  })

  it('an unknown payment outcome tells the visitor NOT to pay again', async () => {
    const p = await open({ me: 'me@b.co', subscribe: { status: 502 } })
    p.fill('name', 'T'); p.fill('phone', '09'); p.cardValid()
    await p.submit()
    expect(p.$('error').textContent).toMatch(/不要重複付款/)
    expect(p.$('done').hidden).toBe(true)
  })

  it('an existing subscription (409) is explained', async () => {
    const p = await open({ me: 'me@b.co', subscribe: { status: 409 } })
    p.fill('name', 'T'); p.fill('phone', '09'); p.cardValid()
    await p.submit()
    expect(p.$('error').textContent).toMatch(/已經有進行中的訂閱/)
  })

  it('a failed registration never reaches payment', async () => {
    const p = await open({ auth: { status: 400, text: 'an account with this email already exists' } })
    p.fill('email', 'a@b.co'); p.fill('password', 'password123'); p.fill('name', 'T'); p.fill('phone', '09'); p.cardValid()
    await p.submit()
    expect(p.paths()).not.toContain('/console/billing/subscribe')
    expect(p.$('error').textContent).toMatch(/already exists/)
  })

  it('invalid card data from TapPay stops before any charge request', async () => {
    const p = await open({ me: 'me@b.co', getPrime: { status: 1, msg: 'bad card' } })
    p.fill('name', 'T'); p.fill('phone', '09'); p.cardValid()
    await p.submit()
    expect(p.paths()).not.toContain('/console/billing/subscribe')
    expect(p.$('error').textContent).toMatch(/卡片資料有誤/)
  })

  it('validates input on the client with the same trimmed values it sends', async () => {
    const p = await open()
    p.cardValid()

    p.fill('email', 'a@b.co'); p.fill('password', 'short'); p.fill('name', 'T'); p.fill('phone', '09')
    await p.submit()
    expect(p.$('error').textContent).toMatch(/8/)

    p.fill('password', 'password123'); p.fill('name', '   ')
    await p.submit()
    expect(p.$('error').textContent).toMatch(/團隊名稱/)

    p.fill('name', 'T'); p.fill('email', 'not-an-email')
    await p.submit()
    expect(p.$('error').textContent).toMatch(/Email/)

    expect(p.paths()).not.toContain('/auth/register')
    expect(p.paths()).not.toContain('/console/billing/subscribe')
  })

  it('stays disabled with a notice when billing is not configured', async () => {
    const p = await open({ billing: false })
    expect(p.$('banner').hidden).toBe(false)
    expect(p.$('submitBtn').disabled).toBe(true)
  })

  it('shows the sandbox notice, and never asks for a real card amount', async () => {
    const p = await open()
    expect(p.$('banner').textContent).toMatch(/測試環境/)
  })

  it('has no team-plan card and no election-level field any more', async () => {
    const p = await open()
    expect(p.d.querySelectorAll('input[name=plan]')).toHaveLength(2)
    expect(p.d.body.textContent).not.toMatch(/團隊服務|參選層級/)
  })
})
