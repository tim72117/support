import { afterEach, describe, expect, it } from 'vitest'
import { build, createServer } from 'vite'
import { JSDOM } from 'jsdom'
import { mkdtempSync, readFileSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'

// The Google Analytics (gtag.js) gate in each page's <head>, exercised through Vite
// itself (the real environment-variable substitution), not a string replace:
//   - dev server        -> never loads gtag.js
//   - production build  -> loads it only with a measurement id and without
//                          VITE_DISABLE_ANALYTICS=1
// Nothing leaves the process: jsdom is told not to fetch scripts, and we
// look at the script element the gate would have added.

const root = fileURLToPath(new URL('..', import.meta.url))
const PAGES = [
  'index.html',
  'privacy.html',
  'terms.html',
  'business/index.html',
  'business/pay.html',
  'business/reserve.html',
  'business/subscribe.html',
  'candidate/index.html',
  'candidate/pay.html',
  'candidate/reserve.html',
  'candidate/subscribe.html',
]
const tmpDirs = []

// An empty env dir for every Vite run: a developer's own .env / .env.local (where local
// tracking is switched on on purpose) must not change what these tests see.
function emptyEnvDir() {
  const d = mkdtempSync(join(tmpdir(), 'empty-env-'))
  tmpDirs.push(d)
  return d
}

afterEach(() => {
  for (const d of tmpDirs.splice(0)) rmSync(d, { recursive: true, force: true })
  delete process.env.VITE_GA_ID
  delete process.env.VITE_DISABLE_ANALYTICS
  delete process.env.VITE_ANALYTICS_IN_DEV
})

/** What the page does at load: did it request gtag.js, and is tracking switched on? */
function run(html, url) {
  const dom = new JSDOM(html, { runScripts: 'dangerously', url })
  const w = dom.window
  const script = [...w.document.querySelectorAll('script[src]')].find((s) => s.src.includes('googletagmanager.com'))
  // gtag() pushes its `arguments` object; flatten to plain arrays.
  const commands = w.dataLayer.map((a) => Array.from(a).map((x) => (x instanceof w.Date ? 'DATE' : x)))
  return { enabled: w.__gaEnabled, src: script?.src ?? null, commands }
}

async function builtPages(env) {
  Object.assign(process.env, env)
  const outDir = mkdtempSync(join(tmpdir(), 'landing-build-'))
  tmpDirs.push(outDir)
  // vitest runs with NODE_ENV=test, which Vite treats as development; a real
  // production build has NODE_ENV=production.
  const previous = process.env.NODE_ENV
  process.env.NODE_ENV = 'production'
  try {
    await build({ root, envDir: emptyEnvDir(), logLevel: 'silent', build: { outDir, emptyOutDir: true } })
  } finally {
    process.env.NODE_ENV = previous
  }
  return Object.fromEntries(PAGES.map((p) => [p, readFileSync(join(outDir, p), 'utf8')]))
}

describe('GA gate on the landing pages', () => {
  it('production build with a measurement id loads gtag.js and configures the property', async () => {
    const pages = await builtPages({ VITE_GA_ID: 'G-TEST123456' })
    for (const [page, html] of Object.entries(pages)) {
      const r = run(html, 'https://example.com/' + page)
      expect(r.enabled, page).toBe(true)
      expect(r.src, page).toBe('https://www.googletagmanager.com/gtag/js?id=G-TEST123456')
      expect(r.commands[0], page).toEqual(['js', 'DATE'])
      expect(r.commands[1].slice(0, 2), page).toEqual(['config', 'G-TEST123456'])
      expect(JSON.stringify(r.commands[1]), page).not.toContain('debug_mode') // never in a production build
    }
  })

  it('production build without a measurement id loads nothing (safe by default)', async () => {
    const pages = await builtPages({})
    for (const [page, html] of Object.entries(pages)) {
      const r = run(html, 'https://example.com/' + page)
      expect(r.enabled, page).toBe(false)
      expect(r.src, page).toBeNull()
      expect(r.commands, page).toEqual([])
    }
  })

  it('VITE_DISABLE_ANALYTICS=1 switches it off even with a measurement id', async () => {
    const pages = await builtPages({ VITE_GA_ID: 'G-TEST123456', VITE_DISABLE_ANALYTICS: '1' })
    for (const [page, html] of Object.entries(pages)) {
      const r = run(html, 'https://example.com/' + page)
      expect(r.enabled, page).toBe(false)
      expect(r.src, page).toBeNull()
    }
  })

  it('the dev server never loads it, even with a measurement id (local events are off)', async () => {
    process.env.VITE_GA_ID = 'G-TEST123456'
    const server = await createServer({ root, envDir: emptyEnvDir(), logLevel: 'silent', server: { middlewareMode: true }, appType: 'custom' })
    try {
      for (const page of PAGES) {
        const html = await server.transformIndexHtml('/' + page, readFileSync(join(root, page), 'utf8'))
        const r = run(html, 'http://localhost:5176/' + page)
        expect(r.enabled, page).toBe(false)
        expect(r.src, page).toBeNull()
        expect(r.commands, page).toEqual([])
      }
    } finally {
      await server.close()
    }
  })

  it('the dev server loads it, in debug mode, ONLY with the explicit local opt-in', async () => {
    process.env.VITE_GA_ID = 'G-TEST123456'
    process.env.VITE_ANALYTICS_IN_DEV = '1'
    const server = await createServer({ root, envDir: emptyEnvDir(), logLevel: 'silent', server: { middlewareMode: true }, appType: 'custom' })
    try {
      for (const page of PAGES) {
        const html = await server.transformIndexHtml('/' + page, readFileSync(join(root, page), 'utf8'))
        const r = run(html, 'http://localhost:5176/' + page)
        expect(r.enabled, page).toBe(true)
        expect(r.src, page).toBe('https://www.googletagmanager.com/gtag/js?id=G-TEST123456')
        expect(JSON.stringify(r.commands[1]), page).toContain('debug_mode')
      }
    } finally {
      await server.close()
    }
  })

  it('the hard off switch beats the local opt-in', async () => {
    process.env.VITE_GA_ID = 'G-TEST123456'
    process.env.VITE_ANALYTICS_IN_DEV = '1'
    process.env.VITE_DISABLE_ANALYTICS = '1'
    const server = await createServer({ root, envDir: emptyEnvDir(), logLevel: 'silent', server: { middlewareMode: true }, appType: 'custom' })
    try {
      const html = await server.transformIndexHtml('/', readFileSync(join(root, 'index.html'), 'utf8'))
      expect(run(html, 'http://localhost:5176/').enabled).toBe(false)
    } finally {
      await server.close()
    }
  })

  it('never loads on an admin path, in any build', async () => {
    const pages = await builtPages({ VITE_GA_ID: 'G-TEST123456' })
    for (const [page, html] of Object.entries(pages)) {
      for (const path of ['/admin', '/admin/', '/admin/users']) {
        const r = run(html, 'https://example.com' + path)
        expect(r.enabled, page + ' ' + path).toBe(false)
        expect(r.src, page + ' ' + path).toBeNull()
      }
      expect(run(html, 'https://example.com/administration-guide').enabled, page).toBe(true)
    }
  })

  it('never loads on an admin path, whatever spelling of it the server happens to serve', async () => {
    const pages = await builtPages({ VITE_GA_ID: 'G-TEST123456' })
    const admin = ['/Admin', '/ADMIN/x', '//admin', '/admin//x', '/%61dmin', '/admin%2Fx', '/admin.html', '/admin/', '/%E0%A4%A']
    const notAdmin = ['/administration-guide', '/adminx', '/support/admin', '/', '/candidate/subscribe.html']
    for (const [page, html] of Object.entries(pages)) {
      for (const path of admin) {
        const r = run(html, 'https://example.com' + path)
        expect(r.enabled, page + ' ' + path).toBe(false)
        expect(r.src, page + ' ' + path).toBeNull()
      }
      for (const path of notAdmin) {
        expect(run(html, 'https://example.com' + path).enabled, page + ' ' + path).toBe(true)
      }
    }
  })

  it('the hard off switch: any non-empty value except 0 / false turns analytics off', async () => {
    for (const value of ['1', 'true', 'yes', ' 1 ', 'TRUE']) {
      const pages = await builtPages({ VITE_GA_ID: 'G-TEST123456', VITE_DISABLE_ANALYTICS: value })
      expect(run(pages['index.html'], 'https://example.com/').enabled, JSON.stringify(value)).toBe(false)
    }
    for (const value of ['0', 'false', 'FALSE', '']) {
      const pages = await builtPages({ VITE_GA_ID: 'G-TEST123456', VITE_DISABLE_ANALYTICS: value })
      expect(run(pages['index.html'], 'https://example.com/').enabled, JSON.stringify(value)).toBe(true)
    }
  })

  it('reports the page address without query string or hash (they can carry tokens / dev parameters)', async () => {
    const pages = await builtPages({ VITE_GA_ID: 'G-TEST123456' })
    for (const [page, html] of Object.entries(pages)) {
      const r = run(html, 'https://ai.shuttle.tools/' + page + '?api=http://internal:8082&console=x&token=abc#secret')
      const config = r.commands.find((c) => c[0] === 'config')
      expect(config[2].page_location, page).toBe('https://ai.shuttle.tools/' + page)
      expect(JSON.stringify(config), page).not.toMatch(/internal|token|secret|api=/)
    }
  })

  it('has no hard-coded measurement id and no leftover Tag Manager', () => {
    for (const page of PAGES) {
      const html = readFileSync(join(root, page), 'utf8')
      expect(html, page).not.toMatch(/\bG-[A-Z0-9]{6,}\b/)
      expect(html, page).not.toMatch(/GTM-|gtm\.js|Tag Manager/)
    }
  })

  it('the pay-test page is a dev tool and carries no analytics', () => {
    expect(readFileSync(join(root, 'candidate/pay-test.html'), 'utf8')).not.toContain('Google Analytics')
  })
})
