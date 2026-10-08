// analytics.ts: the one place that sends custom events and virtual page views
// to Google Analytics 4 (gtag.js, which index.html loads).
//
// Nothing is sent when:
//   - VITE_DISABLE_ANALYTICS is set to anything but empty/0/false (hard off
//     switch, case-insensitive), or
//   - running the Vite dev server, unless VITE_ANALYTICS_IN_DEV=1 (set in the
//     git-ignored .env.local to test tracking locally; index.html then turns
//     on debug_mode so the events land in GA's DebugView), or
//   - the page is under /admin (admin pages are never tracked; the path is
//     decoded, lower-cased and slash-collapsed first, and an undecodable path
//     counts as admin), or
//   - index.html did not enable GA, i.e. no measurement id is configured
//     (window.__gaEnabled is false).
// index.html applies the same rules to loading gtag.js. Tests run in dev mode
// without the opt-in, so they never send anything.
//
// This app changes screens without changing the URL, so each screen reports
// itself with trackPageView(); that is what makes the owner's path through
// the console visible in GA. Never put personal data (email, names, message
// text) in event parameters or in the reported paths.

type Gtag = (command: 'event', name: string, params?: Record<string, unknown>) => void

function isAdminPath(): boolean {
  let p = window.location?.pathname ?? ''
  try {
    p = decodeURIComponent(p)
  } catch {
    return true // a malformed path cannot be judged, so do not track it
  }
  p = p.toLowerCase().replace(/\/+/g, '/')
  return /^\/admin(\/|\.|$)/.test(p)
}

function hardDisabled(): boolean {
  const v = String(import.meta.env.VITE_DISABLE_ANALYTICS ?? '').trim().toLowerCase()
  return v !== '' && v !== '0' && v !== 'false'
}

export function analyticsEnabled(): boolean {
  if (hardDisabled()) return false
  if (import.meta.env.DEV && import.meta.env.VITE_ANALYTICS_IN_DEV !== '1') return false
  if (isAdminPath()) return false
  return (window as unknown as { __gaEnabled?: boolean }).__gaEnabled === true
}

export function trackEvent(name: string, data?: Record<string, unknown>) {
  if (!analyticsEnabled()) return
  ;(window as unknown as { gtag?: Gtag }).gtag?.('event', name, data)
}

let lastPagePath: string | null = null

/** A virtual page view for a screen that has no URL of its own. */
export function trackPageView(path: string, title: string) {
  // Skip consecutive repeats (React StrictMode runs effects twice in dev). Calls
  // blocked by analyticsEnabled() must not update the memory.
  if (!analyticsEnabled() || path === lastPagePath) return
  lastPagePath = path
  trackEvent('page_view', {
    page_path: path,
    page_title: title,
    page_location: (window.location?.origin ?? '') + path,
  })
}

// 'sign_up' is the exact name GA4 recognises as a registration. Only call it
// once a NEW account was really created (the register call succeeded), never
// merely because the button was pressed.
export function trackSignUp() {
  trackEvent('sign_up', { method: 'email' })
}
