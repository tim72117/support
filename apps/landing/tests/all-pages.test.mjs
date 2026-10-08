import { describe, expect, it } from 'vitest'
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'
import { fileURLToPath } from 'node:url'

// A static audit across ALL three apps (console, support, landing), so a page
// or script added later cannot quietly bypass the "no tracking locally, never
// on /admin" rules:
//   - every HTML page either has no analytics at all, or has the full gate;
//   - the only code that may call gtag() is the gated head snippet, the
//     analytics.ts modules (which check the same rules), and the one-line
//     track() helper that checks window.__gaEnabled.

const appsDir = fileURLToPath(new URL('../..', import.meta.url))
const SKIP = new Set(['node_modules', 'dist', '.vite'])

function walk(dir, exts) {
  const out = []
  for (const name of readdirSync(dir)) {
    if (SKIP.has(name)) continue
    const p = join(dir, name)
    if (statSync(p).isDirectory()) out.push(...walk(p, exts))
    else if (exts.some((e) => name.endsWith(e))) out.push(p)
  }
  return out
}

const rel = (p) => relative(appsDir, p).replaceAll('\\', '/')
const pages = walk(appsDir, ['.html'])
const sources = walk(appsDir, ['.ts', '.tsx', '.js', '.jsx', '.mjs']).filter(
  (p) => !/\.(test|d)\.[a-z]+$/.test(p) && !/[\\/]tests[\\/]/.test(p) && !/vite\.config\./.test(p),
)

const HEAD = /<!-- Google Analytics 4 \(gtag\.js\)[\s\S]*?<!-- End Google Analytics -->/

describe('every page in every app', () => {
  it('finds the pages this audit is about', () => {
    expect(pages.map(rel).sort()).toEqual([
      'admin/index.html',
      'console/index.html',
      'landing/business/index.html',
      'landing/business/pay.html',
      'landing/business/reserve.html',
      'landing/business/subscribe.html',
      'landing/candidate/index.html',
      'landing/candidate/pay-test.html',
      'landing/candidate/pay.html',
      'landing/candidate/reserve.html',
      'landing/candidate/subscribe.html',
      'landing/index.html',
      'landing/privacy.html',
      'landing/terms.html',
      'support/index.html',
    ])
  })

  // Pages that must NEVER carry analytics, whatever else changes: the platform admin back office
  // (served under /admin/) and the developer-only payment test page.
  const NEVER_TRACKED = ['admin/index.html', 'landing/candidate/pay-test.html']
  // Every other page is a visitor-facing page and must be tracked (behind the gate).
  it('every visitor-facing page is tracked and every never-tracked page is not', () => {
    for (const page of pages) {
      const name = rel(page)
      const tracked = readFileSync(page, 'utf8').includes('googletagmanager.com')
      expect(tracked, name).toBe(!NEVER_TRACKED.includes(name))
    }
  })

  it('no source file of the admin app talks to analytics at all', () => {
    const adminFiles = walk(join(appsDir, 'admin'), ['.ts', '.tsx', '.js', '.html'])
    expect(adminFiles.length).toBeGreaterThan(0)
    for (const f of adminFiles) {
      expect(readFileSync(f, 'utf8'), rel(f)).not.toMatch(/gtag|dataLayer|googletagmanager|__gaEnabled|analytics/i)
    }
  })

  for (const page of pages) {
    const name = rel(page)
    const html = readFileSync(page, 'utf8')

    if (html.includes('googletagmanager.com')) {
      it(`${name}: tracking is fully gated (hard off switch, dev, /admin, measurement id)`, () => {
        const head = html.match(HEAD)?.[0] ?? ''
        expect(head, 'the gate snippet').not.toBe('')
        expect(head).toContain("'%VITE_DISABLE_ANALYTICS%'")
        expect(head).toContain("var dev = '%DEV%' === 'true'")
        expect(head).toContain("if (dev && '%VITE_ANALYTICS_IN_DEV%' !== '1') return")
        expect(head).toContain('decodeURIComponent(location.pathname)') // the admin guard judges the decoded path
        expect(head).toMatch(/admin/)
        expect(head).toContain('cfg.page_location = location.origin + location.pathname')
        expect(head).toContain("var id = '%VITE_GA_ID%'")
        // the loader only runs after every return above
        expect(head.indexOf('createElement')).toBeGreaterThan(head.lastIndexOf(') return'))
        // exactly one loader on the page
        expect(html.split('googletagmanager.com').length - 1).toBe(1)
      })
    } else {
      it(`${name}: has no analytics at all`, () => {
        expect(html).not.toMatch(/gtag|dataLayer|google-analytics|googletagmanager|__gaEnabled/)
      })
    }

    it(`${name}: page scripts only send events through the gated helper`, () => {
      const rest = html.replace(HEAD, '')
      for (const line of rest.split('\n').filter((l) => /\bgtag\s*\(/.test(l))) {
        expect(line, 'a gtag() call outside the gated head').toContain('__gaEnabled')
      }
      expect(rest).not.toMatch(/dataLayer/)
    })
  }
})

describe('every script in every app', () => {
  it('finds the analytics modules', () => {
    expect(sources.map(rel).filter((p) => p.endsWith('analytics.ts')).sort()).toEqual([
      'console/src/analytics.ts',
      'support/src/analytics.ts',
    ])
  })

  it('nothing but analytics.ts talks to gtag or the dataLayer', () => {
    const offenders = sources
      .filter((p) => !p.endsWith('analytics.ts'))
      .filter((p) => /\bgtag\b|dataLayer|__gaEnabled/.test(readFileSync(p, 'utf8')))
      .map(rel)
    expect(offenders).toEqual([])
  })

  it('analytics.ts checks every rule before sending', () => {
    for (const p of sources.filter((x) => x.endsWith('analytics.ts'))) {
      const src = readFileSync(p, 'utf8')
      const enabled = src.slice(src.indexOf('export function analyticsEnabled'), src.indexOf('export function trackEvent'))
      expect(enabled, rel(p)).toContain('hardDisabled()') // the off switch is consulted first
      expect(src, rel(p)).toContain('VITE_DISABLE_ANALYTICS') // ...and is read from the env (behaviour: analytics.test.ts)
      expect(enabled, rel(p)).toContain('import.meta.env.DEV')
      expect(enabled, rel(p)).toContain("VITE_ANALYTICS_IN_DEV !== '1'")
      expect(enabled, rel(p)).toContain('isAdminPath()')
      expect(enabled, rel(p)).toContain('__gaEnabled')
      // every send goes through trackEvent, which asks analyticsEnabled() first
      const track = src.slice(src.indexOf('export function trackEvent'))
      expect(track.indexOf('if (!analyticsEnabled()) return'), rel(p)).toBeLessThan(track.indexOf('gtag?.('))
    }
  })

  it('no measurement id is hard-coded anywhere', () => {
    for (const p of [...pages, ...sources]) {
      expect(readFileSync(p, 'utf8'), rel(p)).not.toMatch(/\bG-[A-Z0-9]{8,}\b/)
    }
  })
})
