import { describe, expect, it } from 'vitest'
import { JSDOM } from 'jsdom'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

// The same compact GA / checkout checks run against BOTH page sets (candidate/ and business/),
// so the business pages cannot silently lose their events or their "never send the price" rule.
// Backend (fetch) and TapPay (TPDirect) are fakes; `location.href = X` is rewritten in memory
// to `window.__navigate = X` (jsdom cannot navigate). Files on disk are never modified.

const cache = new Map()
function load(rel) {
  if (!cache.has(rel)) {
    cache.set(
      rel,
      readFileSync(fileURLToPath(new URL('../' + rel, import.meta.url)), 'utf8')
        .replace(/<script src="https:\/\/js\.tappaysdk\.com[^>]*><\/script>/, '')
        .replace(/location\.href = /g, 'window.__navigate = '),
    )
  }
  return cache.get(rel)
}

const tick = (ms = 40) => new Promise((r) => setTimeout(r, ms))

async function open(rel, { query = '', me = null, auth = { status: 200 }, subscribe = { status: 200 }, ga = false } = {}) {
  const calls = []
  const sent = []
  const dom = new JSDOM(load(rel), {
    runScripts: 'dangerously',
    url: 'http://localhost:5176/' + rel + query,
    beforeParse(w) {
      w.TPDirect = {
        setupSDK() {},
        card: {
          setup() {},
          onUpdate(cb) { w.__cardUpdate = cb },
          getPrime(cb) { cb({ status: 0, card: { prime: 'PRIME_X' } }) },
        },
      }
      w.fetch = async (url, init = {}) => {
        const path = url.replace(/^https?:\/\/[^/]+/, '')
        calls.push({ path, raw: init.body, body: init.body ? JSON.parse(init.body) : undefined })
        const reply = (status, body) => new Response(typeof body === 'string' ? body : JSON.stringify(body), { status })
        if (path === '/auth/me') return me ? reply(200, { ID: 1, Email: me }) : reply(401, 'not authenticated')
        if (path === '/console/billing/config') return reply(200, { enabled: true, appId: '123', appKey: 'app_key', env: 'sandbox' })
        if (path === '/auth/register' || path === '/auth/login') {
          return auth.status === 200 ? reply(200, { ID: 2, Email: 'x' }) : reply(auth.status, 'rejected')
        }
        if (path === '/console/billing/subscribe') {
          return subscribe.status === 200 ? reply(200, { status: 'active' }) : reply(subscribe.status, 'x')
        }
        return reply(404, 'not found')
      }
    },
  })
  const { window: w } = dom
  if (ga) w.__gaEnabled = true
  w.gtag = (...args) => sent.push(args)
  await tick()
  const d = w.document
  const $ = (id) => d.getElementById(id)
  const submitForm = async (id) => {
    $(id).dispatchEvent(new w.Event('submit', { cancelable: true, bubbles: true }))
    await tick()
  }
  return {
    w, d, $, calls, sent,
    fill: (name, value) => { d.querySelector(`[name=${name}]`).value = value },
    choose: (plan) => d.querySelector(`.choose[data-plan="${plan}"]`).click(),
    setMode(m) {
      const r = d.querySelector(`input[name=mode][value=${m}]`)
      r.checked = true
      r.dispatchEvent(new w.Event('change', { bubbles: true }))
    },
    submitAuth: () => submitForm('authform'),
    submitPay: () => submitForm('payform'),
    cardValid: () => w.__cardUpdate({ canGetPrime: true, status: {} }),
    paths: () => calls.map((c) => c.path),
    call: (path) => calls.find((c) => c.path === path),
    nav: () => w.__navigate,
  }
}

const SETS = [
  { name: 'candidate', dir: 'candidate', plan: 'starter', planName: '參選起步', tier: 'starter' },
  { name: 'business', dir: 'business', plan: 'growth', planName: '成長方案', tier: 'biz_growth' },
]
const SUBSCRIBE = '/console/billing/subscribe'

describe.each(SETS)('$name pages', ({ dir, plan, planName, tier }) => {
  describe('subscribe.html', () => {
    const page = `${dir}/subscribe.html`

    it('registers and goes on to reserve.html with the chosen plan', async () => {
      const p = await open(page)
      p.choose(plan)
      expect(p.$('summary').textContent).toContain(planName)
      p.fill('email', 'a@b.co'); p.fill('password', 'password123')
      await p.submitAuth()
      expect(p.paths()).toContain('/auth/register')
      expect(p.nav()).toBe(`./reserve.html?plan=${plan}`)
      expect(p.paths()).not.toContain(SUBSCRIBE)
    })

    it('sends sign_up (method only, no personal data) after registration', async () => {
      const p = await open(page, { ga: true })
      p.choose(plan)
      p.fill('email', 'secret.person@b.co'); p.fill('password', 'password123')
      await p.submitAuth()
      expect(p.sent).toEqual([['event', 'sign_up', { method: 'email' }]])
      for (const secret of ['secret.person', 'password123']) expect(JSON.stringify(p.sent)).not.toContain(secret)
    })

    it('does not send sign_up for a login or a failed registration', async () => {
      const login = await open(page, { ga: true })
      login.choose(plan); login.setMode('login')
      login.fill('email', 'old@b.co'); login.fill('password', 'whatever')
      await login.submitAuth()
      expect(login.paths()).toContain('/auth/login')
      expect(login.sent).toEqual([])

      const failed = await open(page, { ga: true, auth: { status: 400 } })
      failed.choose(plan)
      failed.fill('email', 'a@b.co'); failed.fill('password', 'password123')
      await failed.submitAuth()
      expect(failed.nav()).toBeUndefined()
      expect(failed.sent).toEqual([])
    })

    it('sends nothing when analytics is off, yet still continues', async () => {
      const p = await open(page)
      expect(p.w.__gaEnabled).toBe(false)
      p.choose(plan)
      p.fill('email', 'a@b.co'); p.fill('password', 'password123')
      await p.submitAuth()
      expect(p.nav()).toBe(`./reserve.html?plan=${plan}`)
      expect(p.sent).toEqual([])
    })
  })

  describe('pay.html', () => {
    const page = `${dir}/pay.html`
    const secrets = ['secret.person', '王小明', '0912345678', 'PRIME_X']
    async function filled(opts) {
      const p = await open(page, { query: `?plan=${plan}`, me: 'secret.person@b.co', ...opts })
      p.fill('name', '王小明店家'); p.fill('phone', '0912345678'); p.cardValid()
      return p
    }

    it('charges with tier / prime / cardholder only - never an amount', async () => {
      const p = await filled()
      expect(p.$('summary').textContent).toContain(planName)
      await p.submitPay()
      const sub = p.call(SUBSCRIBE)
      expect(Object.keys(sub.body).sort()).toEqual(['cardholder', 'prime', 'tier'])
      expect(sub.body.tier).toBe(tier)
      expect(sub.body.prime).toBe('PRIME_X')
      expect(sub.raw).not.toMatch(/amount|price/i)
      expect(p.$('done').hidden).toBe(false)
    })

    it('blocks the page without a valid ?plan=', async () => {
      const p = await open(page, { query: '' })
      expect(p.$('blocked').hidden).toBe(false)
      expect(p.calls).toEqual([])
    })

    it('sends begin_checkout then subscribe_success, plan ids only', async () => {
      const p = await filled({ ga: true })
      await p.submitPay()
      expect(p.sent).toEqual([
        ['event', 'begin_checkout', { plan }],
        ['event', 'subscribe_success', { plan }],
      ])
      for (const s of secrets) expect(JSON.stringify(p.sent)).not.toContain(s)
    })

    it('counts begin_checkout once across declined retries and never sends subscribe_success', async () => {
      const p = await filled({ ga: true, subscribe: { status: 402 } })
      await p.submitPay(); await p.submitPay(); await p.submitPay()
      expect(p.paths().filter((x) => x === SUBSCRIBE)).toHaveLength(3)
      expect(p.sent).toEqual([['event', 'begin_checkout', { plan }]])
      expect(p.$('error').textContent).toMatch(/拒絕/)
    })

    it('sends nothing when analytics is off, yet the payment still works', async () => {
      const p = await filled()
      expect(p.w.__gaEnabled).toBe(false)
      await p.submitPay()
      expect(p.$('done').hidden).toBe(false)
      expect(p.sent).toEqual([])
    })
  })
})
