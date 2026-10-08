import { describe, expect, it } from 'vitest'
import { JSDOM } from 'jsdom'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

// candidate/pay.html: TapPay prime -> /console/billing/subscribe.
// Run in jsdom with the backend (fetch) AND TapPay's SDK (TPDirect) replaced by fakes.
// jsdom cannot navigate, so `location.href = X` is rewritten IN MEMORY (the file on
// disk is untouched) to `window.__navigate = X`; the 401 redirect timer (1500 ms) is
// shortened to 0 so the test does not wait.

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

async function open(rel, { query = '?plan=starter', me = null, subscribe = { status: 200 }, billing = true, env = 'sandbox', getPrime, ga = false } = {}) {
  const calls = []
  const sent = []
  const order = [] // backend paths, 'getPrime' and 'ga:<event>' in the order they happened
  const dom = new JSDOM(load(rel), {
    runScripts: 'dangerously',
    url: 'http://localhost:5176/' + rel + query,
    beforeParse(w) {
      const realSetTimeout = w.setTimeout.bind(w)
      w.setTimeout = (fn, ms, ...a) => realSetTimeout(fn, ms === 1500 ? 0 : ms, ...a)
      w.TPDirect = {
        setupSDK() {},
        card: {
          setup() {},
          onUpdate(cb) { w.__cardUpdate = cb },
          getPrime(cb) {
            order.push('getPrime')
            cb(getPrime ?? { status: 0, card: { prime: 'PRIME_X' } })
          },
        },
      }
      w.fetch = async (url, init = {}) => {
        const path = url.replace(/^https?:\/\/[^/]+/, '')
        order.push(path)
        calls.push({ path, method: init.method, raw: init.body, body: init.body ? JSON.parse(init.body) : undefined, credentials: init.credentials })
        const reply = (status, body) => new Response(typeof body === 'string' ? body : JSON.stringify(body), { status })
        if (path === '/auth/me') return me ? reply(200, { ID: 1, Email: me }) : reply(401, 'not authenticated')
        if (path === '/console/billing/config') {
          return reply(200, billing ? { enabled: true, appId: '123', appKey: 'app_key', env } : { enabled: false })
        }
        if (path === '/console/billing/subscribe') {
          return subscribe.status === 200 ? reply(200, { status: 'active' }) : reply(subscribe.status, subscribe.text ?? 'x')
        }
        return reply(404, 'not found')
      }
    },
  })
  const { window: w } = dom
  if (ga) w.__gaEnabled = true // what the head gate sets in a production build with a measurement id
  w.gtag = (...args) => { sent.push(args); order.push('ga:' + args[1]) }
  await tick()
  const d = w.document
  const $ = (id) => d.getElementById(id)
  return {
    w, d, $, calls, sent, order,
    fill: (name, value) => { d.querySelector(`[name=${name}]`).value = value },
    cardValid: () => w.__cardUpdate({ canGetPrime: true, status: {} }),
    async submit() {
      $('payform').dispatchEvent(new w.Event('submit', { cancelable: true, bubbles: true }))
      await tick()
    },
    paths: () => calls.map((c) => c.path),
    call: (path) => calls.find((c) => c.path === path),
    nav: () => w.__navigate,
    // a ready-to-pay visitor (logged in, valid card, details filled)
    ready() { this.fill('name', 'T'); this.fill('phone', '09'); this.cardValid() },
  }
}

const PAGE = 'candidate/pay.html'
const SUBSCRIBE = '/console/billing/subscribe'

describe('candidate pay page', () => {
  describe('plan selection from the URL', () => {
    it('shows the plan named by ?plan= in the summary', async () => {
      const p = await open(PAGE, { query: '?plan=starter' })
      expect(p.$('summary').textContent).toContain('參選起步')
      expect(p.$('blocked').hidden).toBe(true)
      expect(p.$('payform').hidden).toBe(false)
    })

    it('blocks the page, without touching the backend, when no plan is given', async () => {
      const p = await open(PAGE, { query: '' })
      expect(p.$('blocked').hidden).toBe(false)
      expect(p.$('blockedText').textContent).toMatch(/請先從方案頁選擇/)
      expect(p.$('payform').hidden).toBe(true)
      expect(p.calls).toEqual([])
    })

    it('blocks the page for an unknown plan (including the trial, which never pays)', async () => {
      for (const plan of ['nope', 'candidate_trial']) {
        const p = await open(PAGE, { query: '?plan=' + plan })
        expect(p.$('blocked').hidden).toBe(false)
        expect(p.$('blockedText').textContent).toContain(plan)
        expect(p.calls).toEqual([])
      }
    })
  })

  describe('paying', () => {
    it('gets a prime, then subscribes with only tier, prime and cardholder - never an amount', async () => {
      const p = await open(PAGE, { me: 'me@b.co' })
      expect(p.$('submitBtn').disabled).toBe(true) // card not valid yet
      p.cardValid()
      expect(p.$('submitBtn').disabled).toBe(false)
      expect(p.d.querySelector('[name=email]').value).toBe('me@b.co') // prefilled from the session

      p.fill('name', ' 王小明競選辦公室 '); p.fill('phone', ' 0912345678 ')
      await p.submit()

      expect(p.order.indexOf('getPrime')).toBeGreaterThanOrEqual(0)
      expect(p.order.indexOf('getPrime')).toBeLessThan(p.order.indexOf(SUBSCRIBE))
      const sub = p.call(SUBSCRIBE)
      expect(sub.method).toBe('POST')
      expect(sub.credentials).toBe('include')
      expect(Object.keys(sub.body).sort()).toEqual(['cardholder', 'prime', 'tier'])
      expect(sub.body).toEqual({
        tier: 'starter',
        prime: 'PRIME_X',
        cardholder: { name: '王小明競選辦公室', email: 'me@b.co', phoneNumber: '0912345678' }, // trimmed
      })
      expect(sub.raw).not.toMatch(/amount|price/i)

      expect(p.$('done').hidden).toBe(false)
      expect(p.$('payform').hidden).toBe(true)
      expect(p.$('doneText').textContent).toContain('參選起步')
      expect(p.$('goConsole').href).toBe('http://localhost:5176/app/')
    })

    it('sends the plan from the URL as the tier (campaign)', async () => {
      const p = await open(PAGE, { query: '?plan=campaign', me: 'me@b.co' })
      p.ready()
      await p.submit()
      expect(p.call(SUBSCRIBE).body.tier).toBe('campaign')
    })

    it('validates the details on the client, with the trimmed values, before any prime is requested', async () => {
      const p = await open(PAGE, { me: 'me@b.co' })
      p.cardValid()
      p.fill('name', '   '); p.fill('phone', '09')
      await p.submit()
      expect(p.$('error').textContent).toMatch(/團隊名稱/)

      p.fill('name', 'T'); p.fill('phone', '  ')
      await p.submit()
      expect(p.$('error').textContent).toMatch(/手機號碼/)

      p.fill('phone', '09'); p.fill('email', 'not-an-email')
      await p.submit()
      expect(p.$('error').textContent).toMatch(/Email/)

      expect(p.order).not.toContain('getPrime')
      expect(p.paths()).not.toContain(SUBSCRIBE)
    })

    it('invalid card data from TapPay stops before any charge request', async () => {
      const p = await open(PAGE, { me: 'me@b.co', getPrime: { status: 1, msg: 'bad card' } })
      p.ready()
      await p.submit()
      expect(p.paths()).not.toContain(SUBSCRIBE)
      expect(p.$('error').textContent).toMatch(/卡片資料有誤/)
      expect(p.$('done').hidden).toBe(true)
    })
  })

  describe('backend refusals', () => {
    it('a declined card (402) shows a friendly message, keeps the form, and allows another try', async () => {
      const p = await open(PAGE, { me: 'me@b.co', subscribe: { status: 402, text: 'card declined: x' } })
      p.ready()
      await p.submit()
      expect(p.$('error').textContent).toMatch(/拒絕/)
      expect(p.$('error').textContent).not.toContain('card declined: x')
      expect(p.$('done').hidden).toBe(true)
      expect(p.$('payform').hidden).toBe(false)
      expect(p.$('submitBtn').disabled).toBe(false)
      expect(p.$('submitBtn').textContent).toBe('確認並付款')
      await p.submit() // retry is possible
      expect(p.paths().filter((x) => x === SUBSCRIBE)).toHaveLength(2)
    })

    it('an unknown payment outcome (502) tells the visitor NOT to pay again', async () => {
      const p = await open(PAGE, { me: 'me@b.co', subscribe: { status: 502 } })
      p.ready()
      await p.submit()
      expect(p.$('error').textContent).toMatch(/不要重複付款/)
      expect(p.$('done').hidden).toBe(true)
    })

    it('an existing subscription (409) is explained', async () => {
      const p = await open(PAGE, { me: 'me@b.co', subscribe: { status: 409 } })
      p.ready()
      await p.submit()
      expect(p.$('error').textContent).toMatch(/已經有進行中的訂閱/)
    })

    it('not logged in (401) sends the visitor back to subscribe.html with the plan', async () => {
      const p = await open(PAGE, { me: null, env: 'production', subscribe: { status: 401 } }) // (in sandbox the test-env banner overwrites the not-logged-in one)
      expect(p.$('banner').textContent).toMatch(/尚未登入/)
      p.fill('email', 'a@b.co'); p.ready()
      await p.submit()
      expect(p.$('error').textContent).toMatch(/請先登入/)
      await tick()
      expect(p.nav()).toBe('./subscribe.html?plan=starter')
    })

    it('falls back to the server text for statuses without a friendly message', async () => {
      const p = await open(PAGE, { me: 'me@b.co', subscribe: { status: 500, text: 'boom' } })
      p.ready()
      await p.submit()
      expect(p.$('error').textContent).toBe('boom')
    })
  })

  describe('billing configuration', () => {
    it('stays disabled with a notice when billing is not enabled, and never charges', async () => {
      const p = await open(PAGE, { me: 'me@b.co', billing: false })
      expect(p.$('banner').hidden).toBe(false)
      expect(p.$('banner').textContent).toMatch(/尚未開放/)
      expect(p.$('submitBtn').disabled).toBe(true)
      p.fill('name', 'T'); p.fill('phone', '09')
      await p.submit() // even a forced submit does nothing
      expect(p.order).not.toContain('getPrime')
      expect(p.paths()).not.toContain(SUBSCRIBE)
    })

    it('shows the sandbox notice in the test environment only', async () => {
      const sandbox = await open(PAGE, { me: 'me@b.co', env: 'sandbox' })
      expect(sandbox.$('banner').textContent).toMatch(/測試環境/)
      const prod = await open(PAGE, { me: 'me@b.co', env: 'production' })
      expect(prod.$('banner').hidden).toBe(true)
    })
  })

  describe('analytics events', () => {
    const secrets = ['secret.person', '王小明', '0912345678', 'PRIME_X']
    async function filled(opts) {
      const p = await open(PAGE, { me: 'secret.person@b.co', ga: true, ...opts })
      p.fill('name', '王小明競選辦公室'); p.fill('phone', '0912345678'); p.cardValid()
      return p
    }

    it('sends begin_checkout after the prime and before the charge, then subscribe_success - plan ids only', async () => {
      const p = await filled()
      await p.submit()
      expect(p.sent).toEqual([
        ['event', 'begin_checkout', { plan: 'starter' }],
        ['event', 'subscribe_success', { plan: 'starter' }],
      ])
      expect(p.order.filter((x) => x === 'getPrime' || x === SUBSCRIBE || x.startsWith('ga:'))).toEqual([
        'getPrime', 'ga:begin_checkout', SUBSCRIBE, 'ga:subscribe_success',
      ])
      const wire = JSON.stringify(p.sent)
      for (const s of secrets) expect(wire).not.toContain(s)
    })

    it('counts begin_checkout once per visit even when a declined card is retried, and never a success', async () => {
      const p = await filled({ subscribe: { status: 402 } })
      await p.submit()
      await p.submit()
      await p.submit()
      expect(p.paths().filter((x) => x === SUBSCRIBE)).toHaveLength(3)
      expect(p.sent).toEqual([['event', 'begin_checkout', { plan: 'starter' }]])
      expect(p.sent.some((a) => a[1] === 'subscribe_success')).toBe(false)
      for (const s of secrets) expect(JSON.stringify(p.sent)).not.toContain(s)
    })

    it('no begin_checkout when TapPay refuses the card (no prime was obtained)', async () => {
      const p = await filled({ getPrime: { status: 1, msg: 'bad card' } })
      await p.submit()
      expect(p.sent).toEqual([])
    })

    it('a decline followed by a successful retry sends one begin_checkout and one subscribe_success', async () => {
      const p = await filled({ subscribe: { status: 402 } })
      await p.submit()
      // the backend now accepts the (new) card: swap the fake's behaviour
      const origFetch = p.w.fetch
      p.w.fetch = async (url, init) => (url.endsWith(SUBSCRIBE) ? new Response('{}', { status: 200 }) : origFetch(url, init))
      await p.submit()
      expect(p.sent.map((a) => a[1])).toEqual(['begin_checkout', 'subscribe_success'])
    })

    it('sends nothing at all when analytics is off, yet the payment still works', async () => {
      const p = await filled({ ga: false })
      expect(p.w.__gaEnabled).toBe(false)
      await p.submit()
      expect(p.$('done').hidden).toBe(false)
      expect(p.sent).toEqual([])
    })
  })
})
