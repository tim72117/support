import { describe, expect, it } from 'vitest'
import { JSDOM } from 'jsdom'
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

const read = (rel) => readFileSync(fileURLToPath(new URL(rel, import.meta.url)), 'utf8')
const siteJs = read('../assets/site.js')

// Loads a page and runs assets/site.js the way the browser would.
function load(rel, search = '') {
  const dom = new JSDOM(read(rel).replace(/<script[^>]*src=[^>]*><\/script>/g, ''), {
    runScripts: 'outside-only',
    url: 'http://localhost:5176/' + rel.replace('../', '') + search,
  })
  dom.window.eval(siteJs)
  return dom.window.document
}

describe('login button in the page header', () => {
  for (const rel of ['../index.html', '../candidate/index.html', '../candidate/subscribe.html']) {
    it(`${rel.replace('../', '')} links to the console, not to another project's port`, () => {
      const links = load(rel).querySelectorAll('[data-console-link]')
      expect(links).toHaveLength(1)
      expect(links[0].textContent.trim()).toBe('登入')
      expect(links[0].href).toBe('http://localhost:5177/')
    })
  }

  it('can be pointed at the production console with ?console=', () => {
    const link = load('../index.html', '?console=https://console.example.com/').querySelector('[data-console-link]')
    expect(link.href).toBe('https://console.example.com/')
  })

  it('the page itself keeps a sensible fallback href before the script runs', () => {
    const doc = new JSDOM(read('../index.html'), { runScripts: 'outside-only' }).window.document
    expect(doc.querySelector('[data-console-link]').getAttribute('href')).toBe('http://localhost:5177')
  })

  it('the ghost button keeps readable text (regression: white text on a transparent button)', () => {
    const css = read('../assets/base.css')
    expect(css).toMatch(/\.nav \.links a\.btn\.ghost\s*\{[^}]*color:\s*var\(--ink\)/)
  })
})
