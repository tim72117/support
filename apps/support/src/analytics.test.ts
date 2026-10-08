// @vitest-environment node
import { afterEach, describe, expect, it, vi } from 'vitest'
import { build, createServer } from 'vite'
import { JSDOM } from 'jsdom'
import { mkdtempSync, readFileSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'

// Two things, both about "no analytics while developing":
//  1. trackEvent() only calls gtag() in a production build that has not been
//     switched off and that index.html enabled (a measurement id exists).
//  2. index.html only loads gtag.js under those conditions - checked through
//     Vite's real HTML env substitution, not a string replace.
// This same file lives in apps/console and apps/support.

const root = fileURLToPath(new URL('..', import.meta.url))
const tmpDirs: string[] = []

// An empty env dir for every Vite run below: the developer's own .env.local (which enables local
// tracking on purpose) must not change what these tests see.
function emptyEnvDir() {
  const d = mkdtempSync(join(tmpdir(), 'empty-env-'))
  tmpDirs.push(d)
  return d
}

afterEach(() => {
  vi.unstubAllEnvs()
  for (const d of tmpDirs.splice(0)) rmSync(d, { recursive: true, force: true })
  delete (globalThis as { window?: unknown }).window
})

type Call = unknown[]

function fakeWindow(gaEnabled: boolean, pathname = '/') {
  const calls: Call[] = []
  const w = {
    __gaEnabled: gaEnabled,
    location: { pathname, origin: 'https://ai.shuttle.tools' },
    gtag: (...args: unknown[]) => calls.push(args),
  }
  ;(globalThis as unknown as { window: typeof w }).window = w
  return calls
}

async function freshModule() {
  vi.resetModules()
  return import('./analytics.ts')
}

describe('trackEvent', () => {
  it('does nothing on the dev server / under test, even if GA were enabled', async () => {
    const calls = fakeWindow(true)
    const { trackEvent, analyticsEnabled } = await freshModule()
    expect(analyticsEnabled()).toBe(false)
    trackEvent('anything', { a: 1 })
    expect(calls).toEqual([])
  })

  it('calls gtag("event", ...) in a production build with GA enabled', async () => {
    vi.stubEnv('DEV', false)
    const calls = fakeWindow(true)
    const { trackEvent } = await freshModule()
    trackEvent('create_business', { n: 1 })
    trackEvent('bare')
    expect(calls).toEqual([
      ['event', 'create_business', { n: 1 }],
      ['event', 'bare', undefined],
    ])
  })

  it('does nothing in a production build where no measurement id was configured', async () => {
    vi.stubEnv('DEV', false)
    const calls = fakeWindow(false)
    const { trackEvent } = await freshModule()
    trackEvent('x')
    expect(calls).toEqual([])
  })

  it('is switched off by VITE_DISABLE_ANALYTICS=1 even in a production build', async () => {
    vi.stubEnv('DEV', false)
    vi.stubEnv('VITE_DISABLE_ANALYTICS', '1')
    const calls = fakeWindow(true)
    const { trackEvent } = await freshModule()
    trackEvent('x')
    expect(calls).toEqual([])
  })

  it('sends from the dev server only with the explicit local opt-in', async () => {
    vi.stubEnv('VITE_ANALYTICS_IN_DEV', '1')
    const calls = fakeWindow(true)
    const { trackEvent } = await freshModule()
    trackEvent('local_test')
    expect(calls).toEqual([['event', 'local_test', undefined]])
  })

  it('the hard off switch beats the local opt-in', async () => {
    vi.stubEnv('VITE_ANALYTICS_IN_DEV', '1')
    vi.stubEnv('VITE_DISABLE_ANALYTICS', '1')
    const calls = fakeWindow(true)
    const { trackEvent } = await freshModule()
    trackEvent('x')
    expect(calls).toEqual([])
  })

  const ADMIN_PATHS = [
    '/admin', '/admin/', '/admin/users', '/Admin', '/ADMIN/x', '//admin', '/admin%2Fx',
    '/%61dmin', '/admin.html', '/%E0%A4%A', // the last one is a malformed encoding
  ]
  const NON_ADMIN_PATHS = ['/administration-guide', '/adminx', '/support/admin', '/', '/businesses']

  it('never tracks admin pages, however the path is spelled', async () => {
    vi.stubEnv('DEV', false)
    for (const path of ADMIN_PATHS) {
      const calls = fakeWindow(true, path)
      const { trackEvent, trackPageView, analyticsEnabled } = await freshModule()
      trackEvent('x')
      trackPageView('/somewhere', 'x')
      expect(analyticsEnabled(), path).toBe(false)
      expect(calls, path).toEqual([])
    }
  })

  it('still tracks paths that merely resemble /admin', async () => {
    vi.stubEnv('DEV', false)
    for (const path of NON_ADMIN_PATHS) {
      const calls = fakeWindow(true, path)
      const { trackEvent, trackPageView } = await freshModule()
      trackEvent('x')
      trackPageView('/somewhere', 'x')
      expect(calls, path).toHaveLength(2)
    }
  })

  it('treats VITE_DISABLE_ANALYTICS as off for any value except empty, 0 and false', async () => {
    vi.stubEnv('DEV', false)
    for (const [value, off] of [
      ['1', true], ['true', true], ['yes', true], [' 1 ', true],
      ['', false], ['0', false], ['false', false], ['FALSE', false], [' False ', false],
    ] as const) {
      vi.stubEnv('VITE_DISABLE_ANALYTICS', value)
      const calls = fakeWindow(true)
      const { trackEvent } = await freshModule()
      trackEvent('x')
      expect(calls, JSON.stringify(value)).toHaveLength(off ? 0 : 1)
    }
  })

  it('every disabling value still beats the local opt-in', async () => {
    vi.stubEnv('VITE_ANALYTICS_IN_DEV', '1')
    for (const value of ['1', 'true', 'yes', ' 1 ']) {
      vi.stubEnv('VITE_DISABLE_ANALYTICS', value)
      const calls = fakeWindow(true)
      const { trackEvent } = await freshModule()
      trackEvent('x')
      expect(calls, value).toEqual([])
    }
  })

  it('trackPageView drops a consecutive repeat but sends A, B, A', async () => {
    vi.stubEnv('DEV', false)
    const calls = fakeWindow(true)
    const { trackPageView } = await freshModule()
    trackPageView('/a', 'A')
    trackPageView('/a', 'A')
    expect(calls).toHaveLength(1)
    trackPageView('/b', 'B')
    trackPageView('/a', 'A')
    expect(calls.map((c) => (c[2] as { page_path: string }).page_path)).toEqual(['/a', '/b', '/a'])
  })

  it('a blocked trackPageView does not poison the repeat check (admin path)', async () => {
    vi.stubEnv('DEV', false)
    const calls = fakeWindow(true, '/admin')
    const { trackPageView } = await freshModule()
    trackPageView('/a', 'A')
    expect(calls).toEqual([])
    fakeWindow(true, '/') // same module, now on a tracked path
    ;(globalThis as unknown as { window: { gtag: (...a: unknown[]) => void } }).window.gtag = (...args) => calls.push(args)
    trackPageView('/a', 'A')
    expect(calls).toHaveLength(1)
  })

  it('a blocked trackPageView does not poison the repeat check (hard off switch)', async () => {
    vi.stubEnv('DEV', false)
    vi.stubEnv('VITE_DISABLE_ANALYTICS', '1')
    const calls = fakeWindow(true)
    const { trackPageView } = await freshModule()
    trackPageView('/a', 'A')
    expect(calls).toEqual([])
    vi.stubEnv('VITE_DISABLE_ANALYTICS', '')
    trackPageView('/a', 'A')
    expect(calls).toHaveLength(1)
  })

  it('trackPageView reports a virtual page with its path, title and full location', async () => {
    vi.stubEnv('DEV', false)
    const calls = fakeWindow(true)
    const { trackPageView } = await freshModule()
    trackPageView('/businesses/edit/content', '編輯內容')
    expect(calls).toEqual([
      ['event', 'page_view', {
        page_path: '/businesses/edit/content',
        page_title: '編輯內容',
        page_location: 'https://ai.shuttle.tools/businesses/edit/content',
      }],
    ])
  })

  it('does not throw if gtag is missing', async () => {
    vi.stubEnv('DEV', false)
    const w = { __gaEnabled: true, location: { pathname: '/', origin: 'https://x' } }
    ;(globalThis as unknown as { window: typeof w }).window = w
    const { trackEvent } = await freshModule()
    expect(() => trackEvent('x')).not.toThrow()
  })
})

function gate(html: string, url: string) {
  const w = new JSDOM(html, { runScripts: 'dangerously', url }).window as unknown as {
    document: Document
    __gaEnabled: boolean
    dataLayer: ArrayLike<unknown>[]
  }
  const script = [...w.document.querySelectorAll('script[src]')].find((s) => (s as HTMLScriptElement).src.includes('googletagmanager.com'))
  return {
    enabled: w.__gaEnabled,
    src: script ? (script as HTMLScriptElement).src : null,
    // gtag() pushes its `arguments` object; flatten to plain arrays of the command names + ids.
    commands: w.dataLayer.map((a) => Array.from(a).map((x) => (Object.prototype.toString.call(x) === '[object Date]' ? 'DATE' : x))),
  }
}

async function builtIndex(env: Record<string, string>) {
  Object.assign(process.env, env)
  const outDir = mkdtempSync(join(tmpdir(), 'app-build-'))
  tmpDirs.push(outDir)
  const previous = process.env.NODE_ENV
  process.env.NODE_ENV = 'production' // vitest sets 'test', which Vite treats as development
  try {
    await build({ root, envDir: emptyEnvDir(), logLevel: 'silent', build: { outDir, emptyOutDir: true } })
  } finally {
    process.env.NODE_ENV = previous
    for (const k of Object.keys(env)) delete process.env[k]
  }
  return readFileSync(join(outDir, 'index.html'), 'utf8')
}

describe('Google Analytics gate in index.html', () => {
  it('loads gtag.js and configures the property in a production build with a measurement id', async () => {
    const r = gate(await builtIndex({ VITE_GA_ID: 'G-TEST123456' }), 'https://example.com/')
    expect(r.enabled).toBe(true)
    expect(r.src).toBe('https://www.googletagmanager.com/gtag/js?id=G-TEST123456')
    expect(r.commands[0]).toEqual(['js', 'DATE'])
    expect(r.commands[1].slice(0, 2)).toEqual(['config', 'G-TEST123456']) // 3rd item = per-app options
    expect(JSON.stringify(r.commands[1])).not.toContain('debug_mode') // never in a production build
  })

  it('loads nothing without a measurement id', async () => {
    const r = gate(await builtIndex({}), 'https://example.com/')
    expect(r).toEqual({ enabled: false, src: null, commands: [] })
  })

  it('loads nothing when VITE_DISABLE_ANALYTICS=1', async () => {
    const r = gate(await builtIndex({ VITE_GA_ID: 'G-TEST123456', VITE_DISABLE_ANALYTICS: '1' }), 'https://example.com/')
    expect(r).toEqual({ enabled: false, src: null, commands: [] })
  })

  it('loads nothing on the dev server, even with a measurement id', async () => {
    process.env.VITE_GA_ID = 'G-TEST123456'
    const server = await createServer({ root, envDir: emptyEnvDir(), logLevel: 'silent', server: { middlewareMode: true }, appType: 'custom' })
    try {
      const html = await server.transformIndexHtml('/', readFileSync(join(root, 'index.html'), 'utf8'))
      expect(gate(html, 'http://localhost/')).toEqual({ enabled: false, src: null, commands: [] })
    } finally {
      await server.close()
      delete process.env.VITE_GA_ID
    }
  })

  it('the dev server loads it, in debug mode, only with the explicit local opt-in', async () => {
    process.env.VITE_GA_ID = 'G-TEST123456'
    process.env.VITE_ANALYTICS_IN_DEV = '1'
    const server = await createServer({ root, envDir: emptyEnvDir(), logLevel: 'silent', server: { middlewareMode: true }, appType: 'custom' })
    try {
      const html = await server.transformIndexHtml('/', readFileSync(join(root, 'index.html'), 'utf8'))
      const r = gate(html, 'http://localhost/')
      expect(r.enabled).toBe(true)
      expect(r.src).toBe('https://www.googletagmanager.com/gtag/js?id=G-TEST123456')
      expect(JSON.stringify(r.commands[1])).toContain('debug_mode')
    } finally {
      await server.close()
      delete process.env.VITE_GA_ID
      delete process.env.VITE_ANALYTICS_IN_DEV
    }
  })

  it('never loads on an admin path, whatever spelling of it the server happens to serve', async () => {
    const html = await builtIndex({ VITE_GA_ID: 'G-TEST123456' })
    const admin = ['/admin', '/admin/', '/admin/users', '/Admin', '/ADMIN/x', '//admin', '/admin//x', '/%61dmin', '/admin%2Fx', '/admin.html', '/%E0%A4%A']
    for (const path of admin) {
      expect(gate(html, 'https://example.com' + path), path).toEqual({ enabled: false, src: null, commands: [] })
    }
    for (const path of ['/administration-guide', '/adminx', '/support/admin', '/']) {
      expect(gate(html, 'https://example.com' + path).enabled, path).toBe(true)
    }
  })

  it('the hard off switch: any non-empty value except 0 / false turns analytics off', async () => {
    for (const value of ['1', 'true', 'yes', ' 1 ', 'TRUE']) {
      const html = await builtIndex({ VITE_GA_ID: 'G-TEST123456', VITE_DISABLE_ANALYTICS: value })
      expect(gate(html, 'https://example.com/').enabled, JSON.stringify(value)).toBe(false)
    }
    for (const value of ['0', 'false', 'FALSE', '']) {
      const html = await builtIndex({ VITE_GA_ID: 'G-TEST123456', VITE_DISABLE_ANALYTICS: value })
      expect(gate(html, 'https://example.com/').enabled, JSON.stringify(value)).toBe(true)
    }
  })

  it('reports the page address without query string or hash', async () => {
    const html = await builtIndex({ VITE_GA_ID: 'G-TEST123456' })
    const r = gate(html, 'https://ai.shuttle.tools/support/shop?token=abc&email=a@b.co#secret')
    const config = r.commands.find((c) => c[0] === 'config') as unknown[]
    expect((config[2] as { page_location: string }).page_location).toBe('https://ai.shuttle.tools/support/shop')
    expect(JSON.stringify(config)).not.toMatch(/token|email|secret/)
  })

  it('has no hard-coded measurement id and no leftover Tag Manager', () => {
    const html = readFileSync(join(root, 'index.html'), 'utf8')
    expect(html).not.toMatch(/\bG-[A-Z0-9]{6,}\b/)
    expect(html).not.toMatch(/GTM-|gtm\.js|Tag Manager/)
  })
})
