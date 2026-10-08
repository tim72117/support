/// <reference types="vitest/config" />
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Business-owner console. In production this is embedded into the Go
// backend binary (see backend/Dockerfile, backend/cmd/server/static_console.go)
// and served under "/app/", not the origin root — "/console/" was already
// taken by the console API (see main.go's mountCredentialedRoutes), so
// base must match whatever prefix static_console.go mounts this at, or the
// build's own asset URLs (script src, link href, …) resolve to the wrong
// path. The dev server is unaffected: Vite ignores `base` for the dev
// middleware itself, it only changes built asset URLs and import.meta.env.BASE_URL.
export default defineConfig({
  base: '/app/',
  plugins: [react()],
  server: {
    port: 5177,
    strictPort: true, // 5173-5175 belong to other projects on this machine; fail loudly instead of drifting
  },
  test: {
    // Tests must never depend on (or react to) a developer's own .env / .env.local: that is where
    // local analytics is switched on, and it would leak into import.meta.env here.
    env: { VITE_GA_ID: '', VITE_ANALYTICS_IN_DEV: '', VITE_DISABLE_ANALYTICS: '' },
    // Some analytics tests run real Vite builds; give them room when the machine is busy.
    testTimeout: 30_000,
    environment: 'jsdom',
    setupFiles: ['./src/test-setup.ts'],
  },
})
