import { describe, expect, it } from 'vitest'
import { JSDOM } from 'jsdom'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

// candidate/subscribe.html: pick a plan, register / log in, then be sent on
// (free trial -> /console/billing/start-trial; every other plan -> reserve.html).
// Run in jsdom with the backend (fetch) replaced by a fake; nothing leaves the process.
// jsdom cannot navigate, so the page's `location.href = X` is rewritten IN MEMORY
// (the file on disk is untouched) to `window.__navigate = X`, which the tests read.

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

async function open(rel, { query = '', me = null, auth = { status: 200 }, trial = { status: 200 }, ga = false } = {}) {
  const calls = []
  const sent = []
  const dom = new JSDOM(load(rel), {
    runScripts: 'dangerously',
    url: 'http://localhost:5176/' + rel + query,
    beforeParse(w) {
      w.fetch = async (url, init = {}) => {
        const path = url.replace(/^https?:\/\/[^/]+/, '')
        calls.push({ path, base: url.slice(0, url.length - path.length), method: init.method, body: init.body ? JSON.parse(init.body) : undefined, credentials: init.credentials })
        const reply = (status, body) =>
          new Response(status === 204 ? null : typeof body === 'string' ? body : JSON.stringify(body), { status })
        if (path === '/auth/me') return me ? reply(200, { ID: 1, Email: me }) : reply(401, 'not authenticated')
        if (path === '/auth/register' || path === '/auth/login') {
          return auth.status === 200 ? reply(200, { ID: 2, Email: 'x' }) : reply(auth.status, auth.text ?? 'rejected')
        }
        if (path === '/auth/logout') return reply(204, '')
        if (path === '/console/billing/start-trial') {
          return trial.status === 200 ? reply(200, { tier: 'candidate_trial' }) : reply(trial.status, trial.text ?? 'x')
        }
        return reply(404, 'not found')
      }
    },
  })
  const { window: w } = dom
  if (ga) w.__gaEnabled = true // what the head gate sets in a production build with a measurement id
  w.gtag = (...args) => sent.push(args) // record instead of loading Google's script
  await tick()
  const d = w.document
  const $ = (id) => d.getElementById(id)
  return {
    w, d, $, calls, sent,
    fill: (name, value) => { d.querySelector(`[name=${name}]`).value = value },
    choose: (plan) => d.querySelector(`.choose[data-plan="${plan}"]`).click(),
    setMode(m) {
      const r = d.querySelector(`input[name=mode][value=${m}]`)
      r.checked = true
      r.dispatchEvent(new w.Event('change', { bubbles: true }))
    },
    async submit() {
      $('authform').dispatchEvent(new w.Event('submit', { cancelable: true, bubbles: true }))
      await tick()
    },
    paths: () => calls.map((c) => c.path),
    call: (path) => calls.find((c) => c.path === path),
    nav: () => w.__navigate,
  }
}

const PAGE = 'candidate/subscribe.html'

describe('candidate subscribe page', () => {
  describe('register / log in, then continue', () => {
    it('a new visitor registers and is sent to reserve.html with the chosen plan', async () => {
      const p = await open(PAGE)
      p.choose('starter')
      expect(p.$('step2').hidden).toBe(false)
      expect(p.$('step1').hidden).toBe(true)
      expect(p.$('summary').textContent).toContain('參選起步')

      p.fill('email', '  new@b.co  ')
      p.fill('password', ' password123 ')
      await p.submit()

      const reg = p.call('/auth/register')
      expect(reg.credentials).toBe('include')
      expect(reg.body).toEqual({ email: 'new@b.co', password: ' password123 ' }) // email trimmed, password untouched
      expect(p.nav()).toBe('./reserve.html?plan=starter')
      expect(p.paths()).not.toContain('/console/billing/start-trial')
      expect(p.paths()).not.toContain('/console/billing/subscribe') // payment is never taken on this page
    })

    it('passes the api / console overrides on to reserve.html', async () => {
      const p = await open(PAGE, { query: '?api=http://api.test:9/&console=http://con.test', me: 'me@b.co' })
      expect(p.call('/auth/me').base).toBe('http://api.test:9') // trailing slash stripped
      p.choose('starter')
      expect(p.nav()).toBe('./reserve.html?plan=starter&api=http%3A%2F%2Fapi.test%3A9%2F&console=http%3A%2F%2Fcon.test')
    })

    it('an already-logged-in visitor skips the account step', async () => {
      const p = await open(PAGE, { me: 'me@b.co' })
      expect(p.$('banner').hidden).toBe(false)
      expect(p.$('banner').textContent).toContain('me@b.co')
      p.choose('starter')
      expect(p.$('step2').hidden).toBe(true)
      expect(p.nav()).toBe('./reserve.html?plan=starter')
      expect(p.paths().filter((x) => x.startsWith('/auth/') && x !== '/auth/me')).toEqual([])
    })

    it('an existing user can log in instead of registering', async () => {
      const p = await open(PAGE)
      p.choose('starter')
      p.setMode('login')
      expect(p.$('pwLabel').textContent).toBe('密碼')
      p.fill('email', 'old@b.co'); p.fill('password', 'short') // login does not enforce the 8-char rule
      await p.submit()
      expect(p.paths()).toContain('/auth/login')
      expect(p.paths()).not.toContain('/auth/register')
      expect(p.nav()).toBe('./reserve.html?plan=starter')
    })

    it('a failed registration goes nowhere and can be retried', async () => {
      const p = await open(PAGE, { auth: { status: 400, text: 'an account with this email already exists' } })
      p.choose('starter')
      p.fill('email', 'a@b.co'); p.fill('password', 'password123')
      await p.submit()
      expect(p.nav()).toBeUndefined()
      expect(p.$('error').hidden).toBe(false)
      expect(p.$('error').textContent).toMatch(/already exists/)
      expect(p.$('submitBtn').disabled).toBe(false)
      expect(p.$('submitBtn').textContent).toBe('繼續')
    })

    it('"back to plans" returns to the plan list', async () => {
      const p = await open(PAGE)
      p.choose('starter')
      p.$('backToPlans').click()
      expect(p.$('step1').hidden).toBe(false)
      expect(p.$('step2').hidden).toBe(true)
    })
  })

  describe('client-side validation (same trimmed values as sent; password never trimmed)', () => {
    it('rejects bad input without calling the backend', async () => {
      const p = await open(PAGE)
      p.choose('starter')

      p.fill('email', 'not-an-email'); p.fill('password', 'password123')
      await p.submit()
      expect(p.$('error').textContent).toMatch(/Email/)

      p.fill('email', '   '); // whitespace-only email is empty after trim
      await p.submit()
      expect(p.$('error').textContent).toMatch(/Email/)

      p.fill('email', 'a@b.co'); p.fill('password', '')
      await p.submit()
      expect(p.$('error').textContent).toMatch(/請輸入密碼/)

      p.fill('password', 'short')
      await p.submit()
      expect(p.$('error').textContent).toMatch(/8/)

      expect(p.paths().filter((x) => x.startsWith('/auth/') && x !== '/auth/me')).toEqual([])
      expect(p.nav()).toBeUndefined()
    })

    it('counts the password untrimmed: 8 spaces are accepted and sent as-is', async () => {
      const p = await open(PAGE)
      p.choose('starter')
      p.fill('email', ' a@b.co '); p.fill('password', '        ')
      await p.submit()
      expect(p.call('/auth/register').body).toEqual({ email: 'a@b.co', password: '        ' })
    })

    it('clears the previous error on the next valid attempt', async () => {
      const p = await open(PAGE)
      p.choose('starter')
      p.fill('email', 'bad'); p.fill('password', 'password123')
      await p.submit()
      expect(p.$('error').hidden).toBe(false)
      p.fill('email', 'good@b.co')
      await p.submit()
      expect(p.$('error').hidden).toBe(true)
    })
  })

  describe('free trial (candidate_trial)', () => {
    it('a logged-in visitor starts the trial directly: no reserve.html, no payment', async () => {
      const p = await open(PAGE, { me: 'me@b.co' })
      p.choose('candidate_trial')
      await tick()
      expect(p.call('/console/billing/start-trial').method).toBe('POST')
      expect(p.nav()).toBeUndefined()
      expect(p.paths()).not.toContain('/console/billing/subscribe')
      expect(p.$('trialDone').hidden).toBe(false)
      expect(p.$('trialDoneText').textContent).toContain('me@b.co')
      expect(p.$('goConsoleFromTrial').href).toBe('http://localhost:5176/app/')
      expect(p.$('step1').hidden).toBe(true)
    })

    it('a new visitor registers first, then the trial starts', async () => {
      const p = await open(PAGE)
      p.choose('candidate_trial')
      expect(p.$('submitBtn').textContent).toBe('繼續開通試用')
      p.fill('email', 'new@b.co'); p.fill('password', 'password123')
      await p.submit()
      const order = p.paths()
      expect(order.indexOf('/auth/register')).toBeLessThan(order.indexOf('/console/billing/start-trial'))
      expect(p.nav()).toBeUndefined()
      expect(p.$('trialDone').hidden).toBe(false)
    })

    it('shows a friendly banner when the trial is refused (409) and stays on the plan page', async () => {
      const p = await open(PAGE, { me: 'me@b.co', trial: { status: 409, text: 'raw backend text' } })
      p.choose('candidate_trial')
      await tick()
      expect(p.$('banner').textContent).toMatch(/試用或訂閱紀錄/)
      expect(p.$('banner').textContent).not.toContain('raw backend text')
      expect(p.$('trialDone').hidden).toBe(true)
    })
  })

  describe('analytics events', () => {
    it('sends sign_up (method only) after a successful registration, and nothing with personal data', async () => {
      const p = await open(PAGE, { ga: true })
      p.choose('starter')
      p.fill('email', 'secret.person@b.co'); p.fill('password', 'password123')
      await p.submit()
      expect(p.sent).toEqual([['event', 'sign_up', { method: 'email' }]])
      const wire = JSON.stringify(p.sent)
      for (const secret of ['secret.person', 'password123']) expect(wire).not.toContain(secret)
    })

    it('registering for the free trial still sends only sign_up', async () => {
      const p = await open(PAGE, { ga: true })
      p.choose('candidate_trial')
      p.fill('email', 'a@b.co'); p.fill('password', 'password123')
      await p.submit()
      expect(p.sent).toEqual([['event', 'sign_up', { method: 'email' }]])
    })

    it('does not count a login as a sign-up', async () => {
      const p = await open(PAGE, { ga: true })
      p.choose('starter')
      p.setMode('login')
      p.fill('email', 'old@b.co'); p.fill('password', 'whatever')
      await p.submit()
      expect(p.paths()).toContain('/auth/login')
      expect(p.sent).toEqual([])
    })

    it('does not send sign_up when registration fails or validation stops it', async () => {
      const p = await open(PAGE, { ga: true, auth: { status: 400 } })
      p.choose('starter')
      p.fill('email', 'bad'); p.fill('password', 'password123')
      await p.submit()
      p.fill('email', 'a@b.co')
      await p.submit()
      expect(p.paths()).toContain('/auth/register')
      expect(p.sent).toEqual([])
    })

    it('sends nothing at all when analytics is off, yet the flow still works', async () => {
      const p = await open(PAGE) // __gaEnabled stays false (un-substituted %VITE_GA_ID%)
      expect(p.w.__gaEnabled).toBe(false)
      p.choose('starter')
      p.fill('email', 'a@b.co'); p.fill('password', 'password123')
      await p.submit()
      expect(p.nav()).toBe('./reserve.html?plan=starter')
      expect(p.sent).toEqual([])
    })
  })
})
